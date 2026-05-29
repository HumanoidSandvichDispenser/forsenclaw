package audit

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

// ConsoleSink writes audit events as human-readable lines to the standard logger.
type ConsoleSink struct{}

func NewConsoleSink() *ConsoleSink {
	return &ConsoleSink{}
}

func (s *ConsoleSink) Write(e Event) error {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s", strings.ToUpper(e.Level.String()), e.Kind)
	if e.AgentID != "" {
		fmt.Fprintf(&b, " agent=%s", e.AgentID)
	}

	// Sort field keys for deterministic output.
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%v", k, e.Fields[k])
	}

	log.Printf("audit: %s", b.String())
	return nil
}
