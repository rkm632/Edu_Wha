package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"

	"go.mau.fi/whatsmeow"
	"golang.org/x/crypto/bcrypt"
	"wa-server/whatsapp"
)

var DB *sql.DB

func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}

// 1. LOGIN API (Admin & SaaS Users)
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var id int
	var hashPass, role, aiProvider, apiKey, sysPrompt string
	var autoReply bool

	err := DB.QueryRow(`SELECT id, password, role, ai_provider, api_key, system_prompt, auto_reply 
		FROM users WHERE username=$1`, req.Username).
		Scan(&id, &hashPass, &role, &aiProvider, &apiKey, &sysPrompt, &autoReply)

	if err != nil {
		respondJSON(w, 401, map[string]string{"status": "error", "message": "Invalid Username or Password!"})
		return
	}

	if role == "admin" {
		if req.Password != hashPass {
			respondJSON(w, 401, map[string]string{"status": "error", "message": "Invalid Admin Password!"})
			return
		}
	} else {
		if err := bcrypt.CompareHashAndPassword([]byte(hashPass), []byte(req.Password)); err != nil {
			respondJSON(w, 401, map[string]string{"status": "error", "message": "Invalid Password!"})
			return
		}
	}

	respondJSON(w, 200, map[string]interface{}{
		"status":        "success",
		"token":         "eduta-token-" + req.Username,
		"username":      req.Username,
		"role":          role,
		"user_id":       id,
		"ai_provider":   aiProvider,
		"api_key":       apiKey,
		"system_prompt": sysPrompt,
		"auto_reply":    autoReply,
	})
}

// 2. ADMIN: CREATE USER / LIBRARY ACCOUNT
func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Username == "" || req.Password == "" {
		respondJSON(w, 400, map[string]string{"error": "Username aur Password dono zaroori hain"})
		return
	}

	hashedPass, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	_, err := DB.Exec("INSERT INTO users (username, password, role) VALUES ($1, $2, 'user')", req.Username, string(hashedPass))
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": "Username pehle se maujood hai!"})
		return
	}

	respondJSON(w, 200, map[string]string{"status": "success", "message": "Account Created Successfully!"})
}

// 3. ADMIN: LIST ALL ACCOUNTS
func ListUsersHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := DB.Query("SELECT id, username, role, wa_jid, auto_reply FROM users ORDER BY id ASC")
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var users []map[string]interface{}
	for rows.Next() {
		var id int
		var u, role, jid string
		var auto bool
		rows.Scan(&id, &u, &role, &jid, &auto)

		connStatus := "Offline"
		if c, ok := whatsapp.ActiveClients[u]; ok && c.IsConnected() && c.IsLoggedIn() {
			connStatus = "Connected"
		}

		users = append(users, map[string]interface{}{
			"id":         id,
			"username":   u,
			"role":       role,
			"wa_jid":     jid,
			"auto_reply": auto,
			"status":     connStatus,
		})
	}
	respondJSON(w, 200, map[string]interface{}{"users": users})
}

// 4. ADMIN: DELETE USER
func DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Username == "admin" {
		respondJSON(w, 403, map[string]string{"error": "Master Admin delete nahi ho sakta!"})
		return
	}

	if c, ok := whatsapp.ActiveClients[req.Username]; ok {
		c.Disconnect()
		delete(whatsapp.ActiveClients, req.Username)
	}

	_, _ = DB.Exec("DELETE FROM users WHERE username=$1", req.Username)
	respondJSON(w, 200, map[string]string{"status": "success", "message": "Account Deleted!"})
}

// 5. WHATSAPP STATUS & QR GENERATOR PER USER
func StatusHandler(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	if username == "" {
		username = "admin"
	}

	client := whatsapp.GetOrCreateClient(username)
	status := "Disconnected"
	jid := ""

	if client.IsConnected() && client.IsLoggedIn() {
		status = "Connected"
		if client.Store.ID != nil {
			jid = client.Store.ID.User
		}
	} else if whatsapp.QRChannelMap[username] != "" {
		status = "QR_Ready"
	}

	endpoint := "https://" + r.Host + "/api/send"
	respondJSON(w, 200, map[string]string{
		"status":   status,
		"qr":       whatsapp.QRChannelMap[username],
		"jid":      jid,
		"endpoint": endpoint,
		"username": username,
	})
}

// 6. PHONE PAIRING CODE API
func PairPhoneHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Phone    string `json:"phone"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" {
		req.Username = "admin"
	}

	client := whatsapp.GetOrCreateClient(req.Username)
	if client.IsLoggedIn() {
		respondJSON(w, 400, map[string]string{"error": "Account pehle se connected hai"})
		return
	}

	code, err := client.PairPhone(context.Background(), req.Phone, true, whatsmeow.PairClientChrome, "Chrome (Windows)")
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	respondJSON(w, 200, map[string]string{"code": code})
}

// 7. LOGOUT WHATSAPP DEVICE
func LogoutWAHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" {
		req.Username = "admin"
	}

	if c, ok := whatsapp.ActiveClients[req.Username]; ok {
		_ = c.Logout(context.Background())
		c.Disconnect()
		delete(whatsapp.ActiveClients, req.Username)
	}
	DB.Exec("UPDATE users SET wa_jid='' WHERE username=$1", req.Username)
	respondJSON(w, 200, map[string]string{"status": "success", "message": "WhatsApp Logged Out!"})
}

// 8. SINGLE MESSAGE / IMAGE SEND (External API + Manual Live Chat Reply)
func SendHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Phone    string `json:"phone"`
		Message  string `json:"message"`
		ImageURL string `json:"image_url"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" {
		req.Username = "admin"
	}

	client := whatsapp.GetOrCreateClient(req.Username)
	var err error
	if req.ImageURL != "" {
		err = whatsapp.SendImageFromURL(client, req.Phone, req.ImageURL, req.Message)
	} else {
		err = whatsapp.SendMessage(client, req.Phone, req.Message)
	}

	if err != nil {
		respondJSON(w, 500, map[string]string{"status": "error", "message": err.Error()})
		return
	}

	whatsapp.PushToLiveChat(req.Username, req.Phone, req.Message, "outgoing_manual")
	respondJSON(w, 200, map[string]string{"status": "success", "message": "Message Sent!"})
}

// 9. BULK CAMPAIGN SENDER (With Anti-Ban & Image Support)
func BulkSendHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username   string   `json:"username"`
		Phones     []string `json:"phones"`
		Message    string   `json:"message"`
		ImageURL   string   `json:"image_url"`
		UseAntiBan bool     `json:"use_anti_ban"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" {
		req.Username = "admin"
	}

	client := whatsapp.GetOrCreateClient(req.Username)
	if !client.IsLoggedIn() {
		respondJSON(w, 400, map[string]string{"status": "error", "message": "WhatsApp connected nahi hai!"})
		return
	}

	// Background goroutine me bhejna taaki Render request timeout na kare
	go func() {
		results := whatsapp.SendBulkMessages(client, req.Phones, req.Message, req.ImageURL, req.UseAntiBan)
		for _, res := range results {
			p, _ := res["phone"].(string)
			st, _ := res["status"].(string)
			whatsapp.PushToLiveChat(req.Username, p, "[Bulk "+st+"] "+req.Message, "outgoing_manual")
		}
	}()

	respondJSON(w, 200, map[string]string{
		"status":  "success",
		"message": fmt.Sprintf("Bulk campaign started for %d numbers in background!", len(req.Phones)),
	})
}

// 10. SAVE AI AUTO-REPLY SETTINGS
func SaveAISettingsHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username     string `json:"username"`
		AIProvider   string `json:"ai_provider"`
		APIKey       string `json:"api_key"`
		SystemPrompt string `json:"system_prompt"`
		AutoReply    bool   `json:"auto_reply"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	_, err := DB.Exec(`UPDATE users SET ai_provider=$1, api_key=$2, system_prompt=$3, auto_reply=$4 WHERE username=$5`,
		req.AIProvider, req.APIKey, req.SystemPrompt, req.AutoReply, req.Username)

	if err != nil {
		respondJSON(w, 500, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	respondJSON(w, 200, map[string]string{"status": "success", "message": "AI Bot Settings Saved!"})
}

// 11. PRODUCT IMAGE CATALOG (Save & List Neon Bucket URLs)
func MediaCatalogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Username string `json:"username"`
			Title    string `json:"title"`
			ImageURL string `json:"image_url"`
			Caption  string `json:"caption"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		_, err := DB.Exec("INSERT INTO user_media (username, title, image_url, caption) VALUES ($1, $2, $3, $4)",
			req.Username, req.Title, req.ImageURL, req.Caption)
		if err != nil {
			respondJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		respondJSON(w, 200, map[string]string{"status": "success", "message": "Product Image Saved!"})
		return
	}

	username := r.URL.Query().Get("username")
	rows, err := DB.Query("SELECT id, title, image_url, caption FROM user_media WHERE username=$1 ORDER BY id DESC", username)
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var items []map[string]interface{}
	for rows.Next() {
		var id int
		var title, url, cap string
		rows.Scan(&id, &title, &url, &cap)
		items = append(items, map[string]interface{}{"id": id, "title": title, "image_url": url, "caption": cap})
	}
	respondJSON(w, 200, map[string]interface{}{"media": items})
}

// 12. ZERO-LOAD CLIENT-SIDE LIVE CHAT STREAM (Server-Sent Events)
func LiveChatSSEHandler(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	if username == "" {
		username = "admin"
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	msgChan := make(chan map[string]string, 20)
	whatsapp.StreamLock.Lock()
	whatsapp.LiveChatStreams[username] = msgChan
	whatsapp.StreamLock.Unlock()

	defer func() {
		whatsapp.StreamLock.Lock()
		delete(whatsapp.LiveChatStreams, username)
		whatsapp.StreamLock.Unlock()
	}()

	// Initial ping
	fmt.Fprintf(w, "data: {\"type\":\"system\",\"text\":\"Live Chat Stream Connected\"}\n\n")
	flusher.Flush()

	notify := r.Context().Done()
	for {
		select {
		case <-notify:
			return
		case msg := <-msgChan:
			bytes, _ := json.Marshal(msg)
			fmt.Fprintf(w, "data: %s\n\n", string(bytes))
			flusher.Flush()
		}
	}
}
