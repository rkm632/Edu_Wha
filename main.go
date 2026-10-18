package main

import (
	"log"
	"net/http"
	"os"

	// Ye aapke naye folders ko import kar raha hai
	// Dhyan rahe, aapke go.mod me module ka naam "wa-server" hona chahiye
	"wa-server/database"
	"wa-server/whatsapp"
	"wa-server/api"
)

// CORS Middleware (Browser blocks se bachne ke liye)
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func main() {
	log.Println("🚀 Starting EdutaAI Enterprise Gateway...")

	// 1. Database Start Karein aur API ko DB connection dein
	database.InitDB()
	api.DB = database.DB

	// 2. WhatsApp Engine Start Karein
	dbURL := os.Getenv("DATABASE_URL")
	whatsapp.InitStore(dbURL)

	// 3. API Endpoints Routes
	
	// Frontend Dashboard (Ab ye public folder se aayega)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "public/index.html")
	})

	// User & Admin APIs (handlers.go se link)
	http.HandleFunc("/api/login", corsMiddleware(api.LoginHandler))
	http.HandleFunc("/api/admin/create-user", corsMiddleware(api.CreateUserHandler))
	http.HandleFunc("/api/admin/delete-user", corsMiddleware(api.DeleteUserHandler))
	http.HandleFunc("/api/ai/settings", corsMiddleware(api.SaveAISettingsHandler))

	// 4. Server Port Setup
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	
	log.Printf("✅ Server is running gracefully on port %s", port)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Fatal("Server Crash Error: ", err)
	}
}
