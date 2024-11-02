package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
)

type Message struct {
	ID         int    `json:"id"`
	SenderID   int    `json:"sender_id"`
	ReceiverID int    `json:"receiver_id"`
	Content    string `json:"content"`
	Timestamp  string `json:"timestamp"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

var connections = make(map[*websocket.Conn]User)

// MARK: /ws
// wsEndpoint is a handler to handle websocket connection
func wsEndpoint(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a websocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("error upgrading to websocket: ", err)
		return
	}

	defer conn.Close()

	// Read the first message to get token
	_, msg, err := conn.ReadMessage()
	if err != nil {
		fmt.Println("error reading message: ", err)
		return
	}

	token := string(msg)

	currentUser, err := getHttpUserFromToken(token)
	if err != nil {
		fmt.Println("error getting user from token: ", err)
		return
	}

	connections[conn] = currentUser

	for {
		var message Message

		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("error reading message: ", err)
			delete(connections, conn)
			return
		}

		err = json.Unmarshal(msg, &message)
		if err != nil {
			fmt.Println("error unmarshalling message: ", err)
			delete(connections, conn)
			return
		}

		fmt.Println("message: ", string(msg), message)

		err = messageInsert(message.SenderID, message.ReceiverID, message.Content)
		if err != nil {
			fmt.Println("error inserting message: ", err)
			delete(connections, conn)
			return
		}

		// Send message to sender and receiver
		for connection, user := range connections {
			if user.ID == message.ReceiverID || user.ID == message.SenderID {
				fmt.Println("sending message to receiver: ", message)
				if err = connection.WriteMessage(msgType, msg); err != nil {
					fmt.Println("error writing message: ", err)
					delete(connections, connection)
					return
				}
			}
		}
	}
}

// MARK: /conversation
// conversationEndpoint is a handler to get messages between two users
func conversationEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"message": "only POST method allowed"}`))
		return
	}

	user, err := getHttpUser(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	// Get receiver ID
	r.ParseForm()
	receiverID := r.Form.Get("receiver_id")
	if len(receiverID) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "receiver_id is required"}`))
		return
	}

	// Convert receiver ID to integer
	receiverIDInt, err := strconv.Atoi(receiverID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "receiver_id must be an integer"}`))
		return
	}

	messages, err := getConversation(user.ID, receiverIDInt)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	messagesJSON, err := json.Marshal(messages)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(messagesJSON)
}

// MARK: /allmessages
// allMessagesEndpoint is a handler to get all messages of a user
func allMessagesEndpoint(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte(`{"message": "only POST method allowed"}`))
		return
	}

	user, err := getHttpUser(r)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	messages, err := getAllMessages(user.ID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	messagesJSON, err := json.Marshal(messages)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + err.Error() + `"}`))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(messagesJSON)
}

func getAllMessages(userID int) ([]Message, error) {
	db, err := sql.Open("sqlite3", "./database.db")
	if err != nil {
		return nil, err
	}

	defer db.Close()

	rows, err := db.Query("SELECT * FROM messages WHERE sender_id = ? OR receiver_id = ? ORDER BY timestamp", userID, userID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var message Message
		err = rows.Scan(&message.ID, &message.SenderID, &message.ReceiverID, &message.Content, &message.Timestamp)
		if err != nil {
			return nil, err
		}

		messages = append(messages, message)
	}

	return messages, nil
}

func getConversation(senderID int, receiverID int) ([]Message, error) {
	db, err := sql.Open("sqlite3", "./database.db")
	if err != nil {
		return nil, err
	}

	defer db.Close()

	rows, err := db.Query("SELECT * FROM messages WHERE (sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?) ORDER BY timestamp", senderID, receiverID, receiverID, senderID)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var message Message
		err = rows.Scan(&message.ID, &message.SenderID, &message.ReceiverID, &message.Content, &message.Timestamp)
		if err != nil {
			return nil, err
		}

		messages = append(messages, message)
	}

	return messages, nil
}

func messageInsert(senderID int, receiverID int, content string) error {
	if condition := len(content) > 0; !condition {
		return errors.New("content cannot be empty")
	}

	if condition := senderID > 0; !condition {
		return errors.New("sender_id must be greater than 0")
	}

	if condition := receiverID > 0; !condition {
		return errors.New("receiver_id must be greater than 0")
	}

	db, err := sql.Open("sqlite3", "./database.db")
	if err != nil {
		return err
	}

	defer db.Close()

	_, err = db.Exec("INSERT INTO messages (sender_id, receiver_id, content) VALUES (?, ?, ?)", senderID, receiverID, content)
	if err != nil {
		return err
	}

	return nil
}
