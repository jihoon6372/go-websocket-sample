package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// connection is an middleman between the websocket connection and the hub.
type connection struct {
	// The websocket connection.
	ws *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte
}

// readPump pumps messages from the websocket connection to the hub.
func (s subscription) readPump(user string) {
	c := s.conn
	defer func() {
		users := []string{}
		for i := range h.roomusers[s.room] {
			if h.roomusers[s.room][i] != s.user {
				users = append(users, h.roomusers[s.room][i])
			}
		}

		sendData := map[string]interface{}{
			"type": "updated_users",
			"user": user,
			"data": nil,
			"users": users,
		}
		refineSendData, _ := json.Marshal(sendData)
		m := message{refineSendData, s.room}
		h.broadcast <- m
		h.unregister <- s
		c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, msg, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
				log.Printf("error: %v", err)
			}
			break
		}

		var objmap map[string]interface{}
		_ = json.Unmarshal(msg, &objmap)
		user := objmap["user"].(string)
		rcvmsg := objmap["message"].(string)

		sendData := map[string]interface{}{
			"type": "message",
			"user": user,
			"data": rcvmsg,
		}
		refineSendData, _ := json.Marshal(sendData)
		m := message{refineSendData, s.room}
		h.broadcast <- m
	}
}

// write writes a message with the given message type and payload.
func (c *connection) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(mt, payload)
}

// writePump pumps messages from the hub to the websocket connection.
func (s *subscription) writePump() {
	c := s.conn
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.write(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.write(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func (s *subscription) joinedPump(user string) {
	users := []string{}
	users = append(users, user)

	for i := range h.roomusers[s.room] {
		users = append(users, h.roomusers[s.room][i])
	}

	users = makeSliceUnique(users)

	sendData := map[string]interface{}{
		"type": "updated_users",
		"user": user,
		"data": nil,
		"users": users,
	}
	refineSendData, _ := json.Marshal(sendData)

	m := message{refineSendData, s.room}
	h.broadcast <- m
}

// serveWs handles websocket requests from the peer.
func serveWs(w http.ResponseWriter, r *http.Request, roomId string) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err.Error())
		return
	}

	user := r.URL.Query().Get("user")
	c := &connection{send: make(chan []byte, 256), ws: ws}
	s := subscription{c, roomId, user}
	h.register <- s

	go s.writePump()
	go s.readPump(user)
	go s.joinedPump(user)
}

func makeSliceUnique(s []string) []string { 
	keys := make(map[string]struct{}) 
	res := make([]string, 0) 
	for _, val := range s { 
		if _, ok := keys[val]; ok { 
			continue
		} else { 
			keys[val] = struct{}{}
			res = append(res, val)
		}
	} 

	return res 
}
