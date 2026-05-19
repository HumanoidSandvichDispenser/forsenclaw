package api

import (
	"net/http"

	"github.com/coder/websocket"
)

// handleWebSocket upgrades an HTTP connection to a WebSocket and manages
// the client's subscription lifecycle.
func (svc *Service) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	client := NewClient(svc.hub, conn)
	go client.writePump()
	client.readPump()
}
