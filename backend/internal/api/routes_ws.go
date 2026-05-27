package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/coder/websocket"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/dispatch"
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
	client.onSubscribe = func(roomID int64) {
		pending := svc.agentMgr.ConfirmationRegistry().List(roomID)
		for _, pc := range pending {
			data, err := json.Marshal(dispatch.StreamEvent{
				Type:    "confirmation.pending",
				Payload: pc,
			})
			if err != nil {
				continue
			}
			select {
			case client.send <- data:
			default:
				log.Printf("ws: send buffer full, dropping confirmation.pending replay for room %d node %s", roomID, pc.NodeID)
			}
		}
	}

	go client.writePump()
	client.readPump()
}
