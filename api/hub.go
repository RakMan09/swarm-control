package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// envelope is the framing for every websocket message pushed to the dashboard.
type envelope struct {
	Type string `json:"type"` // "fleet" | "alert"
	Data any    `json:"data"`
}

// hub tracks connected websocket clients and broadcasts framed messages. Fleet
// state is pushed as coalesced snapshots on a timer (bounded regardless of
// ingest rate); alerts are pushed as they arrive.
type hub struct {
	mu      sync.Mutex
	clients map[*wsClient]struct{}
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

func newHub() *hub { return &hub{clients: map[*wsClient]struct{}{}} }

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1 << 16,
	CheckOrigin:     func(_ *http.Request) bool { return true },
}

func (h *hub) handle(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &wsClient{conn: conn, send: make(chan []byte, 64)}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()

	go h.writePump(c)
	h.readPump(c) // blocks until the client disconnects
}

func (h *hub) readPump(c *wsClient) {
	defer func() {
		h.mu.Lock()
		delete(h.clients, c)
		h.mu.Unlock()
		close(c.send)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(4096)
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *hub) writePump(c *wsClient) {
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// broadcast frames v and sends it to all clients, dropping for slow clients.
func (h *hub) broadcast(typ string, v any) {
	raw, err := json.Marshal(envelope{Type: typ, Data: v})
	if err != nil {
		log.Printf("api: marshal %s: %v", typ, err)
		return
	}
	h.mu.Lock()
	for c := range h.clients {
		select {
		case c.send <- raw:
		default: // slow client: drop this frame rather than block the hub
		}
	}
	h.mu.Unlock()
}

func (h *hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
