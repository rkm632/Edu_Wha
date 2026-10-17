package database

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func InitDB() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is not set")
	}

	var err error
	DB, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Ping karke check karna ki DB chal raha hai ya nahi
	if err = DB.Ping(); err != nil {
		log.Fatal("Database ping failed:", err)
	}

	log.Println("✅ PostgreSQL Database Connected Successfully!")

	// Users table create karna (Agar pehle se nahi hai to)
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username VARCHAR(50) UNIQUE NOT NULL,
		password VARCHAR(255) NOT NULL,
		role VARCHAR(20) DEFAULT 'user', -- 'admin' ya 'user'
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`

	_, err = DB.Exec(createTableQuery)
	if err != nil {
		log.Fatal("Failed to create users table:", err)
	}

	// Check karna ki default Admin exist karta hai ya nahi, agar nahi to create karna
	createDefaultAdmin()
}

func createDefaultAdmin() {
	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_PASS")

	if adminUser == "" { adminUser = "admin" }
	if adminPass == "" { adminPass = "admin123" }

	var exists bool
	err := DB.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE username=$1)", adminUser).Scan(&exists)
	if err != nil {
		log.Println("Error checking admin user:", err)
		return
	}

	if !exists {
		_, err = DB.Exec("INSERT INTO users (username, password, role) VALUES ($1, $2, 'admin')", adminUser, adminPass)
		if err != nil {
			log.Println("Error creating default admin:", err)
		} else {
			log.Println("👑 Default Admin Created!")
		}
	}
}
