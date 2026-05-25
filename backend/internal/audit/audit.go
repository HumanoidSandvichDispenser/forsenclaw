package audit

import (
	"context"
	"log"
	"path/filepath"
	"time"
)

// Level is the severity of an audit event.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseLevel parses a level string, returning LevelInfo for unrecognised values.
func ParseLevel(s string) Level {
	switch s {
	case "debug":
		return LevelDebug
	case "warn":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Kind identifies the category of an audit event.
type Kind string

const (
	// Permission decisions (agent layer)
	KindPermissionAllowed Kind = "permission.allowed"
	KindPermissionDenied  Kind = "permission.denied"

	// Confirmation flow (agent layer)
	KindConfirmationAsked    Kind = "confirmation.asked"
	KindConfirmationAccepted Kind = "confirmation.accepted"
	KindConfirmationRejected Kind = "confirmation.rejected"

	// Tool invocations (mcp layer)
	KindToolInvoked Kind = "tool.invoked"
	KindToolFailed  Kind = "tool.failed"

	// Agent lifecycle (manager layer)
	KindAgentStarted  Kind = "agent.started"
	KindAgentStopped  Kind = "agent.stopped"
	KindAgentReloaded Kind = "agent.reloaded"

	// Debug (development/tracing)
	KindDebugInference Kind = "debug.inference"
	KindDebugToolArgs  Kind = "debug.tool_args"
)

// Event is a single auditable occurrence.
type Event struct {
	Timestamp time.Time
	Level     Level
	Kind      Kind
	AgentID   string
	Fields    map[string]any
}

// Logger accepts audit events and fans them out to configured sinks.
// Log is non-blocking — events are dispatched via a buffered channel.
type Logger struct {
	sinks []filteredSink
	ch    chan Event
	done  chan struct{}
}

// Sink is an output destination for audit events.
type Sink interface {
	Write(Event) error
}

type filteredSink struct {
	sink     Sink
	patterns []string // glob patterns; empty means accept all
	minLevel Level
}

func (f filteredSink) matches(e Event) bool {
	if e.Level < f.minLevel {
		return false
	}
	if len(f.patterns) == 0 {
		return true
	}
	for _, p := range f.patterns {
		if ok, _ := filepath.Match(p, string(e.Kind)); ok {
			return true
		}
	}
	return false
}

// SinkConfig pairs a Sink with its filtering rules.
type SinkConfig struct {
	Sink     Sink
	Kinds    []string // glob patterns; empty means accept all
	MinLevel Level
}

// NewLogger creates a Logger that routes events to the given sinks.
func NewLogger(sinks []SinkConfig) *Logger {
	fs := make([]filteredSink, 0, len(sinks))
	for _, sc := range sinks {
		fs = append(fs, filteredSink{sink: sc.Sink, patterns: sc.Kinds, minLevel: sc.MinLevel})
	}
	l := &Logger{
		sinks: fs,
		ch:    make(chan Event, 256),
		done:  make(chan struct{}),
	}
	go l.loop()
	return l
}

// Nop returns a Logger that discards all events. Useful in tests.
func Nop() *Logger {
	l := &Logger{ch: make(chan Event, 1), done: make(chan struct{})}
	go l.loop()
	return l
}

// Log enqueues an event for delivery. Non-blocking; drops the event if the
// internal buffer is full.
func (l *Logger) Log(e Event) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	select {
	case l.ch <- e:
	default:
		log.Printf("audit: buffer full, dropping event %s for agent %q", e.Kind, e.AgentID)
	}
}

// ctxKey is the unexported context key type for audit values.
type ctxKey int

const agentIDKey ctxKey = iota

// WithAgentID returns a context carrying the given agent ID for audit logging.
func WithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, agentIDKey, agentID)
}

// AgentIDFromContext returns the agent ID stored in the context, or empty string.
func AgentIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(agentIDKey).(string); ok {
		return id
	}
	return ""
}

// Close drains the event buffer, stops the background goroutine, and blocks
// until all pending events have been delivered to sinks.
func (l *Logger) Close() {
	close(l.ch)
	<-l.done
}

func (l *Logger) loop() {
	defer close(l.done)
	for e := range l.ch {
		for _, fs := range l.sinks {
			if !fs.matches(e) {
				continue
			}
			if err := fs.sink.Write(e); err != nil {
				log.Printf("audit: sink write error for event %s: %v", e.Kind, err)
			}
		}
	}
}
