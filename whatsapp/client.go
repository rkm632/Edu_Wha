package whatsapp

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var (
	StoreContainer *sqlstore.Container
	ActiveClients  = make(map[string]*whatsmeow.Client)
	QRChannelMap   = make(map[string]string)
	ClientsLock    sync.Mutex
	DB             *sql.DB

	// Live Chat Listeners (Browser SSE channels per user)
	LiveChatStreams = make(map[string]chan map[string]string)
	StreamLock      sync.Mutex
)

func InitStore(dbURL string, dbConn *sql.DB) {
	DB = dbConn
	dbLog := waLog.Stdout("WhatsAppStore", "ERROR", true)
	var err error
	StoreContainer, err = sqlstore.New(context.Background(), "postgres", dbURL, dbLog)
	if err != nil {
		panic("WhatsApp Store Error: " + err.Error())
	}
}

// Har User/Account ke liye alag WhatsApp Client banana ya load karna
func GetOrCreateClient(username string) *whatsmeow.Client {
	ClientsLock.Lock()
	defer ClientsLock.Unlock()

	if c, exists := ActiveClients[username]; exists {
		return c
	}

	deviceStore, err := StoreContainer.GetFirstDevice(context.Background())
	if err != nil || deviceStore == nil {
		deviceStore = StoreContainer.NewDevice()
	}

	// Agar dusra user hai aur pehla device kisi aur ka hai, to naya device banao
	if username != "admin" && deviceStore.ID != nil {
		var savedJID string
		_ = DB.QueryRow("SELECT wa_jid FROM users WHERE username=$1", username).Scan(&savedJID)
		if savedJID == "" {
			deviceStore = StoreContainer.NewDevice()
		}
	}

	clientLog := waLog.Stdout("Client-"+username, "ERROR", true)
	client := whatsmeow.NewClient(deviceStore, clientLog)

	// Incoming Messages & Events Listener
	client.AddEventHandler(func(evt interface{}) {
		handleWhatsAppEvents(username, client, evt)
	})

	ActiveClients[username] = client

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		_ = client.Connect()
		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					QRChannelMap[username] = evt.Code
				} else if evt.Event == "success" {
					QRChannelMap[username] = ""
					if client.Store.ID != nil {
						DB.Exec("UPDATE users SET wa_jid=$1 WHERE username=$2", client.Store.ID.String(), username)
					}
				}
			}
		}()
	} else {
		_ = client.Connect()
	}

	return client
}

// Incoming Message Handler (Client-side Chat + AI Auto-Reply)
func handleWhatsAppEvents(username string, client *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		if v.Info.IsFromMe || v.Info.IsGroup {
			return
		}

		senderPhone := v.Info.Sender.User
		msgText := v.Message.GetConversation()
		if msgText == "" && v.Message.ExtendedTextMessage != nil {
			msgText = v.Message.ExtendedTextMessage.GetText()
		}
		if msgText == "" {
			return
		}

		// 1. ZERO-LOAD LIVE CHAT: Browser par seedha push karo
		PushToLiveChat(username, senderPhone, msgText, "incoming")

		// 2. AI AUTO-REPLY CHECK
		go func() {
			var provider, apiKey, sysPrompt string
			var autoReply bool
			err := DB.QueryRow("SELECT ai_provider, api_key, system_prompt, auto_reply FROM users WHERE username=$1", username).
				Scan(&provider, &apiKey, &sysPrompt, &autoReply)

			if err == nil && autoReply && apiKey != "" {
				replyText, imgURL := GenerateAIReply(username, senderPhone, msgText, provider, apiKey, sysPrompt)
				if imgURL != "" {
					_ = SendImageFromURL(client, senderPhone, imgURL, replyText)
					PushToLiveChat(username, senderPhone, "[Image Sent] "+replyText, "outgoing_ai")
				} else if replyText != "" {
					_ = SendMessage(client, senderPhone, replyText)
					PushToLiveChat(username, senderPhone, replyText, "outgoing_ai")
				}
			}
		}()
	}
}

func PushToLiveChat(username, phone, text, msgType string) {
	StreamLock.Lock()
	ch, exists := LiveChatStreams[username]
	StreamLock.Unlock()

	if exists {
		select {
		case ch <- map[string]string{
			"phone": phone,
			"text":  text,
			"type":  msgType,
			"time":  time.Now().Format("15:04"),
		}:
		default:
			// Agar browser tab band hai to drop kar do (Zero RAM leak!)
		}
	}
}

// 1. Text Message Bhejna
func SendMessage(client *whatsmeow.Client, phone string, message string) error {
	if client == nil || !client.IsLoggedIn() {
		return fmt.Errorf("whatsapp connected nahi hai")
	}
	targetJID := types.NewJID(phone, types.DefaultUserServer)
	msg := &waE2E.Message{Conversation: proto.String(message)}
	_, err := client.SendMessage(context.Background(), targetJID, msg)
	return err
}

// 2. Image URL (Neon Bucket) Se Photo + Caption Bhejna
func SendImageFromURL(client *whatsmeow.Client, phone, imageURL, caption string) error {
	if client == nil || !client.IsLoggedIn() {
		return fmt.Errorf("whatsapp connected nahi hai")
	}

	// Neon Storage ya URL se image bytes download karna
	resp, err := http.Get(imageURL)
	if err != nil {
		return fmt.Errorf("image download failed: %v", err)
	}
	defer resp.Body.Close()

	imgBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// WhatsApp Server par encrypt karke upload karna
	uploaded, err := client.Upload(context.Background(), imgBytes, whatsmeow.MediaImage)
	if err != nil {
		return fmt.Errorf("whatsapp media upload failed: %v", err)
	}

	mimeType := http.DetectContentType(imgBytes)
	targetJID := types.NewJID(phone, types.DefaultUserServer)

	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption:       proto.String(caption),
			URL:           proto.String(uploaded.URL),
			DirectPath:    proto.String(uploaded.DirectPath),
			MediaKey:      uploaded.MediaKey,
			Mimetype:      proto.String(mimeType),
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    proto.Uint64(uint64(len(imgBytes))),
		},
	}

	_, err = client.SendMessage(context.Background(), targetJID, msg)
	return err
}

// 3. Bulk Sender (Text + Optional Image URL + Anti-Ban Delay)
func SendBulkMessages(client *whatsmeow.Client, phones []string, message string, imageURL string, useAntiBan bool) []map[string]interface{} {
	var results []map[string]interface{}

	for i, phone := range phones {
		if useAntiBan && i > 0 {
			delay := time.Duration(rand.Intn(6)+3) * time.Second
			time.Sleep(delay)
		}

		var err error
		if imageURL != "" {
			err = SendImageFromURL(client, phone, imageURL, message)
		} else {
			err = SendMessage(client, phone, message)
		}

		if err != nil {
			results = append(results, map[string]interface{}{"phone": phone, "status": "failed", "error": err.Error()})
		} else {
			results = append(results, map[string]interface{}{"phone": phone, "status": "success"})
		}
	}
	return results
}
