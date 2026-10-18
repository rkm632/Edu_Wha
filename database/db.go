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
		log.Fatal("DATABASE_URL set nahi hai!")
	}

	var err error
	DB, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("Database connection error:", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatal("Database ping error:", err)
	}

	log.Println("✅ Neon PostgreSQL Connected Successfully!")

	// 1. Users & AI Config Table
	createUsersTable := `
	CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY,
		username VARCHAR(50) UNIQUE NOT NULL,
		password VARCHAR(255) NOT NULL,
		role VARCHAR(20) DEFAULT 'user',
		wa_jid VARCHAR(100) DEFAULT '',
		ai_provider VARCHAR(30) DEFAULT 'groq',
		api_key TEXT DEFAULT '',
		system_prompt TEXT DEFAULT '',
		auto_reply BOOLEAN DEFAULT false,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);`
	_, err = DB.Exec(createUsersTable)
	if err != nil {
		log.Fatal("Users table error:", err)
	}

	// Ensure naye columns purane table me bhi add ho jayein
	DB.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS wa_jid VARCHAR(100) DEFAULT '';")
	DB.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS ai_provider VARCHAR(30) DEFAULT 'groq';")
	DB.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS api_key TEXT DEFAULT '';")
	DB.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS system_prompt TEXT DEFAULT '';")
	DB.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS auto_reply BOOLEAN DEFAULT false;")

	// 2. Products / Media Table (Neon Object Storage ke Image URLs save karne ke liye)
	createProductsTable := `
	CREATE TABLE IF NOT EXISTS user_media (
		id SERIAL PRIMARY KEY,
		username VARCHAR(50) NOT NULL,
		title VARCHAR(100) NOT NULL,
		image_url TEXT NOT NULL,
		caption TEXT DEFAULT ''
	);`
	_, _ = DB.Exec(createProductsTable)

	createDefaultAdmin()
}

func createDefaultAdmin() {
	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_PASS")
	if adminUser == "" { adminUser = "admin" }
	if adminPass == "" { adminPass = "admin123" }

	var exists bool
	_ = DB.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE username=$1)", adminUser).Scan(&exists)
	if !exists {
		_, _ = DB.Exec("INSERT INTO users (username, password, role) VALUES ($1, $2, 'admin')", adminUser, adminPass)
		log.Println("👑 Default Master Admin Ready!")
	}
}
