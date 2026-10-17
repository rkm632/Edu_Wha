package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	_ "github.com/lib/pq"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

// Global State
var client *whatsmeow.Client
var currentQR string

// ==========================================
// 1. HELPER FUNCTIONS (API-FIRST DESIGN)
// ==========================================

// JSON response bhejne ka global function (Ye HTML/CSS ki errors se bachata hai)
func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// CORS Middleware: Ye kisi bhi browser ya UI se aane wali request ko allow karega
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization")

		// Preflight check (Browser ki security ko pass karne ke liye)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

// ==========================================
// 2. MAIN SERVER STARTUP
// ==========================================

func main() {
	// Database Connection (Aiven PostgreSQL)
	dbLog := waLog.Stdout("Database", "ERROR", true)
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL set nahi hai!")
	}

	container, err := sqlstore.New(context.Background(), "postgres", dbURL, dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "ERROR", true)
	client = whatsmeow.NewClient(deviceStore, clientLog)

	// WhatsApp Connection Engine
	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					currentQR = evt.Code
				} else {
					currentQR = ""
				}
			}
		}()
	} else {
		err = client.Connect()
		if err != nil {
			panic(err)
		}
	}

	// ==========================================
	// 3. SECURE API ENDPOINTS
	// ==========================================

	// Frontend Page Render
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// Admin Login API
	http.HandleFunc("/api/login", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		validUser := os.Getenv("ADMIN_USER")
		validPass := os.Getenv("ADMIN_PASS")
		if validUser == "" { validUser = "admin" }
		if validPass == "" { validPass = "admin123" }

		if req.Username == validUser && req.Password == validPass {
			respondJSON(w, 200, map[string]string{"status": "success", "token": "eduta-ai-premium-token"})
		} else {
			respondJSON(w, 401, map[string]string{"status": "error", "message": "Invalid Credentials"})
		}
	}))

	// Status API
	http.HandleFunc("/api/status", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		status := "Disconnected"
		if client.IsConnected() && client.IsLoggedIn() {
			status = "Connected"
		} else if currentQR != "" {
			status = "QR_Ready"
		}
		
		endpoint := "https://" + r.Host + "/api/send"
		respondJSON(w, 200, map[string]string{
			"status":   status,
			"qr":       currentQR,
			"endpoint": endpoint,
		})
	}))

	// Pairing Code API
	http.HandleFunc("/api/pair", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		var req struct { Phone string `json:"phone"` }
		json.NewDecoder(r.Body).Decode(&req)

		if client.IsLoggedIn() {
			respondJSON(w, 400, map[string]string{"error": "Already logged in"})
			return
		}

		code, err := client.PairPhone(context.Background(), req.Phone, true, whatsmeow.PairClientChrome, "Chrome (Windows)")
		if err != nil {
			respondJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, 200, map[string]string{"code": code})
	}))

	// Send Message API
	http.HandleFunc("/api/send", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Phone   string `json:"phone"`
			Message string `json:"message"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if !client.IsLoggedIn() {
			respondJSON(w, 403, map[string]string{"status": "error", "message": "WhatsApp not connected"})
			return
		}

		targetJID := types.NewJID(req.Phone, types.DefaultUserServer)
		msg := &waE2E.Message{Conversation: proto.String(req.Message)}
		
		_, err = client.SendMessage(context.Background(), targetJID, msg)
		if err != nil {
			respondJSON(w, 500, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		respondJSON(w, 200, map[string]string{"status": "success", "message": "Message sent!"})
	}))

	// Start Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server running gracefully on port %s", port)
	http.ListenAndServe(":"+port, nil)
}
