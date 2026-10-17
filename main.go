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
	waLog "go.mau.fi/whatsmeow/util/log"
)

var client *whatsmeow.Client
var currentQR string

func main() {
	// 1. Database setup
	dbLog := waLog.Stdout("Database", "ERROR", true)
	
	// FIXED: Naye update ke hisaab se 'context.Background()' ko pehle argument par set kiya aur '1' hata diya
	container, err := sqlstore.New(context.Background(), "sqlite", "file:store.db?_pragma=foreign_keys(1)", dbLog)
	if err != nil {
		panic(err)
	}

	// FIXED: Yahan bhi context pass kar diya gaya hai
	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(err)
	}

	clientLog := waLog.Stdout("Client", "ERROR", true)
	client = whatsmeow.NewClient(deviceStore, clientLog)

	// 2. WhatsApp connection aur QR Code generator
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

	// 3. Landing Page (HTML) dikhane ka rasta
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "index.html")
	})

	// 4. Status aur QR Code check karne ki API
	http.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		status := "Disconnected"
		if client.IsConnected() && client.IsLoggedIn() {
			status = "Connected"
		} else if currentQR != "" {
			status = "QR_Ready"
		}
		json.NewEncoder(w).Encode(map[string]string{
			"status": status,
			"qr":     currentQR,
		})
	})

	// 5. Server Start
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Server running on port %s", port)
	http.ListenAndServe(":"+port, nil)
}
