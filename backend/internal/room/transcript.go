package room

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// TotalLineCount returns the total number of messages in the transcript.
// It reads the file sequentially to count lines. For bounded reads, prefer
// ReadMessagesTail which does not load the full file.
func TotalLineCount(roomsDir, roomID string) (int, error) {
	path := filepath.Join(roomsDir, roomID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan transcript: %w", err)
	}
	return count, nil
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

// ReadMessagesTail reads up to the last `limit` messages from the transcript
// file, but never reads before `offset` (the compaction cursor). It reads
// backward from the end of the file without loading the entire file into
// memory.  The returned slice is in chronological order.
func ReadMessagesTail(roomsDir, roomID string, offset, limit int) ([]Message, error) {
	if limit <= 0 {
		return []Message{}, nil
	}

	path := filepath.Join(roomsDir, roomID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Message{}, nil
		}
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat transcript: %w", err)
	}
	if stat.Size() == 0 {
		return []Message{}, nil
	}

	// Count total lines so we can enforce the offset boundary.
	// We reuse the already-open file handle for counting.
	totalCount := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			totalCount++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("count transcript lines: %w", err)
	}

	// Clamp limit so we never return messages before the compaction cursor.
	available := totalCount - offset
	if available <= 0 {
		return []Message{}, nil
	}
	if limit > available {
		limit = available
	}

	// Seek back to the start for the backward read.
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek transcript: %w", err)
	}

	// Read backward in chunks, collecting complete lines.
	const chunkSize = 64 * 1024 // 64 KiB
	var (
		lines      [][]byte // collected lines in reverse order (newest first)
		buf        []byte   // leftover partial line from previous chunk
		fileOffset = stat.Size()
	)

	for fileOffset > 0 && len(lines) < limit {
		// Determine chunk bounds
		readSize := int64(chunkSize)
		if fileOffset < readSize {
			readSize = fileOffset
		}
		fileOffset -= readSize

		_, err := f.Seek(fileOffset, io.SeekStart)
		if err != nil {
			return nil, fmt.Errorf("seek transcript: %w", err)
		}

		chunk := make([]byte, readSize)
		if _, err := io.ReadFull(f, chunk); err != nil {
			return nil, fmt.Errorf("read transcript chunk: %w", err)
		}

		// Prepend chunk to any leftover from the next (newer) chunk
		chunk = append(chunk, buf...)
		buf = nil

		// Split into lines
		for {
			if limit > 0 && len(lines) >= limit {
				break
			}
			idx := bytes.LastIndexByte(chunk, '\n')
			if idx == -1 {
				// No newline left; save remainder as prefix for next (older) chunk
				if len(chunk) > 0 {
					buf = chunk
				}
				break
			}
			line := chunk[idx+1:]
			chunk = chunk[:idx]
			if len(line) > 0 {
				lines = append(lines, line)
			}
		}
	}

	// If we have a leftover buf and we're at the start of the file, that's the first line
	if fileOffset == 0 && len(buf) > 0 {
		lines = append(lines, buf)
	}

	// Reverse to chronological order
	var msgs []Message
	for i := len(lines) - 1; i >= 0; i-- {
		var msg Message
		if err := json.Unmarshal(lines[i], &msg); err != nil {
			return nil, fmt.Errorf("unmarshal message: %w", err)
		}
		msgs = append(msgs, msg)
	}

	return msgs, nil
}

// ReadMessagesFromOffset reads messages starting at `offset` from the
// beginning of the transcript, returning up to `limit` messages.
// This reads the file from the beginning, so it is not suitable for
// tail reads on large files.
func ReadMessagesFromOffset(roomsDir, roomID string, offset, limit int) ([]Message, error) {
	if limit <= 0 {
		return []Message{}, nil
	}

	ctx := context.Background()
	msgs, err := ReadMessages(ctx, roomsDir, roomID, ReadOpts{})
	if err != nil {
		return nil, err
	}

	if offset >= len(msgs) {
		return []Message{}, nil
	}

	end := offset + limit
	if end > len(msgs) {
		end = len(msgs)
	}

	return msgs[offset:end], nil
}
