package room

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TranscriptWriter provides append-only write access to a room's JSONL
// transcript file. Each Append call serialises a Message as a single JSON
// line and writes it atomically to disk.
type TranscriptWriter struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// NewTranscriptWriter opens (or creates) the transcript file for roomID in
// roomsDir. The file is opened in append mode.
func NewTranscriptWriter(roomsDir, roomID string) (*TranscriptWriter, error) {
	if err := os.MkdirAll(roomsDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir rooms: %w", err)
	}

	path := filepath.Join(roomsDir, roomID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}

	return &TranscriptWriter{file: f, path: path}, nil
}

// Append serialises msg as JSON and appends it as a single line to the
// transcript. The write is protected by a mutex so multiple goroutines can
// safely append to the same transcript concurrently.
func (w *TranscriptWriter) Append(ctx context.Context, msg Message) error {
	if err := msg.Validate(); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	// Append the newline before writing so the JSON object and its line
	// terminator are written in a single syscall, avoiding a partial line on
	// crash between two separate Write calls.
	data = append(data, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Write(data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}

	return nil
}

// Close closes the underlying file handle.
func (w *TranscriptWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// ReadOpts controls filtering and pagination for transcript reads.
type ReadOpts struct {
	// Limit is the maximum number of messages to return (0 = all).
	Limit int

	// After returns only messages strictly after this time.
	After *time.Time

	// Before returns only messages strictly before this time.
	Before *time.Time
}

// ReadMessages reads all messages from the transcript file, applying the
// given filters. Messages are returned in chronological order (the order they
// appear in the file).
func ReadMessages(ctx context.Context, roomsDir, roomID string, opts ReadOpts) ([]Message, error) {
	path := filepath.Join(roomsDir, roomID + ".jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Message{}, nil
		}
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	const maxTokenSize = 16 * 1024 * 1024 // 16 MiB per line
	var msgs []Message
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxTokenSize)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil, fmt.Errorf("unmarshal message: %w", err)
		}

		if opts.After != nil && !msg.Timestamp.After(*opts.After) {
			continue
		}
		if opts.Before != nil && !msg.Timestamp.Before(*opts.Before) {
			continue
		}

		msgs = append(msgs, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan transcript: %w", err)
	}

	if opts.Limit > 0 && len(msgs) > opts.Limit {
		msgs = msgs[len(msgs)-opts.Limit:]
	}

	return msgs, nil
}
