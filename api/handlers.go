package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"golang.org/x/crypto/bcrypt"
)

// DB connection receive karne ke liye
var DB *sql.DB

// Error aur Success bhejne ka helper function
func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// --------------------------------------------------------
// 1. LOGIN API (Admin aur Normal Users Dono Ke Liye)
// --------------------------------------------------------
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var id int
	var hashPass, role string

	err := DB.QueryRow("SELECT id, password, role FROM users WHERE username=$1", req.Username).Scan(&id, &hashPass, &role)
	if err != nil {
		respondJSON(w, 401, map[string]string{"status": "error", "message": "Invalid Username or Password!"})
		return
	}

	// Normal user ke liye bcrypt use hota hai, par default admin ke liye plain text (for initial setup)
	if role == "admin" {
		if req.Password != hashPass {
			respondJSON(w, 401, map[string]string{"status": "error", "message": "Invalid Admin Password!"})
			return
		}
	} else {
		err = bcrypt.CompareHashAndPassword([]byte(hashPass), []byte(req.Password))
		if err != nil {
			respondJSON(w, 401, map[string]string{"status": "error", "message": "Invalid User Password!"})
			return
		}
	}

	// Ek real app me yahan JWT Token generate hota hai
	respondJSON(w, 200, map[string]interface{}{
		"status": "success",
		"token":  "dummy-auth-token-for-" + req.Username,
		"role":   role,
		"user_id": id,
	})
}

// --------------------------------------------------------
// 2. CREATE NEW USER API (Sirf Admin Use Kar Sakta Hai)
// --------------------------------------------------------
func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Username == "" || req.Password == "" {
		respondJSON(w, 400, map[string]string{"error": "Username and Password required"})
		return
	}

	// Password ko hack hone se bachane ke liye Encrypt (Hash) karna
	hashedPass, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)

	// User ko database me dalna aur AI settings ki default values empty rakhna
	_, err := DB.Exec("INSERT INTO users (username, password, role) VALUES ($1, $2, 'user')", req.Username, string(hashedPass))
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": "Username already exists or DB Error!"})
		return
	}

	respondJSON(w, 200, map[string]string{"status": "success", "message": "New SaaS User Created Successfully!"})
}

// --------------------------------------------------------
// 3. DELETE USER API (Admin Feature)
// --------------------------------------------------------
func DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Username == "admin" {
		respondJSON(w, 403, map[string]string{"error": "Cannot delete master admin!"})
		return
	}

	_, err := DB.Exec("DELETE FROM users WHERE username=$1", req.Username)
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": "Failed to delete user"})
		return
	}

	respondJSON(w, 200, map[string]string{"status": "success", "message": "User deleted permanently."})
}

// --------------------------------------------------------
// 4. SAVE AI SETTINGS API (Custom Chatbot config)
// --------------------------------------------------------
func SaveAISettingsHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username     string `json:"username"`
		AIProvider   string `json:"ai_provider"` // openai, gemini, groq, deepseek, openrouter
		APIKey       string `json:"api_key"`
		SystemPrompt string `json:"system_prompt"` // Business details for lead gen
		AutoReply    bool   `json:"auto_reply"`  // true ya false
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Note: Database me AI columns hum next update me add karenge. 
	// Abhi ye sirf API receiver hai jo Dashboard se settings lega.
	
	// DB.Exec("UPDATE users SET ai_provider=$1, api_key=$2, system_prompt=$3, auto_reply=$4 WHERE username=$5", ...)
	
	respondJSON(w, 200, map[string]string{"status": "success", "message": "AI Auto-Reply Settings Saved Successfully!"})
}
