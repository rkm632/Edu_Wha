package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"

	_ "modernc.org/sqlite"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var client *whatsmeow.Client
var currentQR string

func main() {
	// Database setup
	dbLog := waLog.Stdout("Database", "ERROR", true)
	container, err := sqlstore.New(context.Background(), "sqlite", "file:store.db?_pragma=foreign_keys(1)", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "ERROR", true)
	client = whatsmeow.NewClient(deviceStore, clientLog)

	// Connection Logic
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

	// 1. Landing Page UI
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// 2. Status API
	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := "Disconnected"
		if client.IsConnected() && client.IsLoggedIn() {
			status = "Connected"
		} else if currentQR != "" {
			status = "QR_Ready"
		}
		
		endpoint := "https://" + r.Host + "/api/send"
		json.NewEncoder(w).Encode(map[string]string{
			"status":   status,
			"qr":       currentQR,
			"endpoint": endpoint,
		})
	})

	// 3. Code-Based Login API
	http.HandleFunc("/api/pair", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct { Phone string `json:"phone"` }
		json.NewDecoder(r.Body).Decode(&req)

		if client.IsLoggedIn() {
			json.NewEncoder(w).Encode(map[string]string{"error": "Already logged in"})
			return
		}

		// FIXED: Yahan context.Background() pehle parameter me add kar diya gaya hai
		code, err := client.PairPhone(context.Background(), req.Phone, true, whatsmeow.PairClientChrome, "Chrome (Windows)")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"code": code})
	})

	// 4. Message Send API
	http.HandleFunc("/api/send", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		var req struct {
			Phone   string `json:"phone"`
			Message string `json:"message"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if !client.IsLoggedIn() {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "WhatsApp not connected"})
			return
		}

		targetJID := types.NewJID(req.Phone, types.DefaultUserServer)
		msg := &waE2E.Message{Conversation: proto.String(req.Message)}
		_, err = client.SendMessage(context.Background(), targetJID, msg)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Message sent!"})
	})

	// Server Start
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server running on port %s", port)
	http.ListenAndServe(":"+port, nil)
}
