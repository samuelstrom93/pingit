package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Message struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

type Client struct {
	conn *websocket.Conn
	send chan []byte
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*Client]struct{})}
}

func (h *Hub) Add(matchID string, conn *websocket.Conn) *Client {
	h.mu.Lock()
	defer h.mu.Unlock()

	client := &Client{
		conn: conn,
		send: make(chan []byte, 16),
	}
	if h.clients[matchID] == nil {
		h.clients[matchID] = make(map[*Client]struct{})
	}
	h.clients[matchID][client] = struct{}{}
	return client
}

func (h *Hub) Remove(matchID string, client *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if group := h.clients[matchID]; group != nil {
		delete(group, client)
		if len(group) == 0 {
			delete(h.clients, matchID)
		}
	}
	close(client.send)
}

func (h *Hub) Broadcast(matchID string, msg Message) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients[matchID] {
		select {
		case client.send <- payload:
		default:
		}
	}
}

func (c *Client) Run(ctx context.Context) {
	go c.readLoop(ctx)
	c.writeLoop(ctx)
}

func (c *Client) readLoop(ctx context.Context) {
	defer c.conn.CloseNow()
	for {
		if _, _, err := c.conn.Read(ctx); err != nil {
			return
		}
	}
}

func (c *Client) writeLoop(ctx context.Context) {
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	defer c.conn.CloseNow()

	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-c.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.conn.Write(writeCtx, websocket.MessageText, message)
			cancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
