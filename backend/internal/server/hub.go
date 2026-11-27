package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"support-assistant/internal/model"
)

// Hub — подписчики WebSocket /api/ws. Менеджер получает все события,
// клиент — только по своему обращению (присылает {"type":"watch","ticketId":"t-…"}).
type Hub struct {
	mu      sync.Mutex
	clients map[*client]bool
}

type client struct {
	conn     *websocket.Conn
	send     chan []byte
	mu       sync.Mutex
	operator bool
	ticketID string
}

func NewHub() *Hub { return &Hub{clients: map[*client]bool{}} }

// Emit — разослать событие. Не блокируется: медленному клиенту события пропускаются.
func (h *Hub) Emit(e model.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		slog.Error("событие не сериализуется", "ошибка", err)
		return
	}
	ticketID := e.EventTicketID()
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.mu.Lock()
		allowed := c.operator || ticketID == "" || ticketID == c.ticketID
		c.mu.Unlock()
		if !allowed {
			continue
		}
		select {
		case c.send <- data:
		default:
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize: 1024, WriteBufferSize: 4096,
	// Фронтенд и API на одном адресе; в режиме разработки Vite проксирует /api
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Serve — подключение WebSocket. operator — вошедший менеджер.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, operator bool) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &client{conn: conn, send: make(chan []byte, 256), operator: operator}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()

	// Запись: события и пинги, чтобы прокси не закрывал тихое соединение
	go func() {
		ping := time.NewTicker(30 * time.Second)
		defer func() {
			ping.Stop()
			conn.Close()
		}()
		for {
			select {
			case data, ok := <-c.send:
				if !ok {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if conn.WriteMessage(websocket.TextMessage, data) != nil {
					return
				}
			case <-ping.C:
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if conn.WriteMessage(websocket.PingMessage, nil) != nil {
					return
				}
			}
		}
	}()

	// Чтение: клиент сообщает своё обращение
	conn.SetReadLimit(4096)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg struct {
			Type     string `json:"type"`
			TicketID string `json:"ticketId"`
		}
		if json.Unmarshal(data, &msg) == nil && msg.Type == "watch" && len(msg.TicketID) < 100 {
			c.mu.Lock()
			c.ticketID = msg.TicketID
			c.mu.Unlock()
		}
	}
	h.mu.Lock()
	delete(h.clients, c)
	close(c.send)
	h.mu.Unlock()
}
