package dispatch

import (
	"context"
	"fmt"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// StartProtocol instantiates and starts the protocol for a room. This must be
// called after a room is created before messages can be handled.
func (d *Dispatcher) StartProtocol(r *room.Room) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	var proto room.Protocol
	switch r.ProtocolType {
	case room.ProtocolFreeForm:
		p, err := room.NewFreeFormProtocol(r)
		if err != nil {
			return fmt.Errorf("create freeform protocol: %w", err)
		}
		proto = p
	default:
		return fmt.Errorf("unsupported protocol type: %q", r.ProtocolType)
	}

	if err := proto.Start(r, d); err != nil {
		return fmt.Errorf("start protocol: %w", err)
	}

	d.protocols[r.ID] = proto
	return nil
}

// getProtocol returns the active protocol for a room, starting it if necessary.
func (d *Dispatcher) getProtocol(ctx context.Context, r *room.Room) (room.Protocol, error) {
	d.mu.RLock()
	proto, ok := d.protocols[r.ID]
	d.mu.RUnlock()
	if ok {
		return proto, nil
	}

	if err := d.StartProtocol(r); err != nil {
		return nil, err
	}

	d.mu.RLock()
	proto = d.protocols[r.ID]
	d.mu.RUnlock()
	return proto, nil
}
