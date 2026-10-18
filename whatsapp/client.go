package whatsapp

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/proto/waE2E"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

var (
	StoreContainer *sqlstore.Container
	// ActiveClients ek dictionary hai jo har user ke alag WhatsApp session ko memory me rakhegi
	ActiveClients  = make(map[string]*whatsmeow.Client)
)

// 1. WhatsApp Database Store Initialize Karna
func InitStore(dbURL string) {
	dbLog := waLog.Stdout("WhatsAppStore", "ERROR", true)
	var err error
	StoreContainer, err = sqlstore.New(context.Background(), "postgres", dbURL, dbLog)
	if err != nil {
		panic("Failed to connect WhatsApp store to DB: " + err.Error())
	}
}

// 2. Single Message Sender
func SendMessage(client *whatsmeow.Client, phone string, message string) error {
	if !client.IsLoggedIn() {
		return fmt.Errorf("whatsapp account is not connected")
	}
	
	targetJID := types.NewJID(phone, types.DefaultUserServer)
	msg := &waE2E.Message{Conversation: proto.String(message)}
	
	_, err := client.SendMessage(context.Background(), targetJID, msg)
	return err
}

// 3. Bulk Sender Engine with ANTI-BAN Logic
func SendBulkMessages(client *whatsmeow.Client, phones []string, message string, useAntiBan bool) []map[string]interface{} {
	var results []map[string]interface{}

	for i, phone := range phones {
		// 🟢 ANTI-BAN ENGINE: Agar user ne Anti-Ban ON (true) kiya hai
		if useAntiBan && i > 0 {
			// 3 se 8 second ke beech ek random delay (Pause) generate karo
			delay := time.Duration(rand.Intn(6)+3) * time.Second
			fmt.Printf("Anti-Ban Active: Waiting %v before sending to next number...\n", delay)
			time.Sleep(delay) // Server ko utni der ke liye rok do
		}

		err := SendMessage(client, phone, message)
		
		// Status record karo taaki Frontend Dashboard par report dikhayi ja sake
		if err != nil {
			results = append(results, map[string]interface{}{"phone": phone, "status": "failed", "error": err.Error()})
		} else {
			results = append(results, map[string]interface{}{"phone": phone, "status": "success"})
		}
	}

	return results
}
