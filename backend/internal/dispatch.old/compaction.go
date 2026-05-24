package dispatch

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/memory"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// maybeCompact checks if the assembled context exceeds the compaction trigger
// threshold and, if so, summarizes the oldest eligible messages into the
// agent's daily note and advances the compaction cursor.
//
// Returns true if compaction ran (caller should reassemble context).
// Compaction failure is non-fatal — the caller should log and proceed.
func (d *Dispatcher) maybeCompact(ctx context.Context, ag *agent.Agent, roomID string, cursor *room.CompactionCursor, assembled *memory.AssembledContext) (bool, error) {
	assembledSize := assembledContextSize(assembled)
	if assembledSize < d.ctxConfig.CompactionTrigger {
		return false, nil
	}

	totalCount, err := room.TotalLineCount(d.paths.RoomsDir(), roomID)
	if err != nil {
		return false, fmt.Errorf("count transcript lines: %w", err)
	}

	guaranteed := d.ctxConfig.MinimumGuaranteed
	available := totalCount - cursor.Offset - guaranteed
	if available < guaranteed {
		log.Printf("dispatcher: compaction skipped for room %s, not enough messages outside guaranteed window (%d available)", roomID, available)
		return false, nil
	}

	bytesToRemove := assembledSize - d.ctxConfig.CompactionTarget
	batchSize, accumulatedBytes, err := d.calculateCompactionBatch(roomID, cursor.Offset, bytesToRemove, available)
	if err != nil {
		return false, fmt.Errorf("calculate compaction batch: %w", err)
	}
	if batchSize <= 0 {
		log.Printf("dispatcher: compaction skipped for room %s, batch size <= 0", roomID)
		return false, nil
	}

	log.Printf("dispatcher: compacting %d messages (%d bytes) from room %s for agent %s", batchSize, accumulatedBytes, roomID, ag.Name())

	batchMessages, err := room.ReadMessagesFromOffset(d.paths.RoomsDir(), roomID, cursor.Offset, batchSize)
	if err != nil {
		return false, fmt.Errorf("read compaction batch: %w", err)
	}

	summary, err := d.summariseBatch(ctx, ag, roomID, batchMessages)
	if err != nil {
		return false, err
	}

	if ag.Definition.FeatureFlags.DailyNotes {
		if err := d.writeSummaryToDailyNote(ag.Name(), roomID, summary); err != nil {
			return false, err
		}
	}

	newCursor := &room.CompactionCursor{
		AgentName: ag.Name(),
		RoomID:    roomID,
		Offset:    cursor.Offset + batchSize,
	}
	if err := d.store.SetCompactionCursor(ctx, newCursor); err != nil {
		return false, fmt.Errorf("advance compaction cursor: %w", err)
	}

	log.Printf("dispatcher: compaction complete for room %s, cursor advanced to %d", roomID, newCursor.Offset)
	return true, nil
}

// compactAndReassembleIfNeeded runs compaction if the context exceeds the
// trigger threshold, then reassembles. Returns the original assembled context
// unchanged if compaction was skipped or failed.
func (d *Dispatcher) compactAndReassembleIfNeeded(ctx context.Context, ag *agent.Agent, rfc room.RFC, cursor *room.CompactionCursor, assembled *memory.AssembledContext, interjections []room.Message) (*memory.AssembledContext, error) {
	compacted, err := d.maybeCompact(ctx, ag, rfc.RoomID, cursor, assembled)
	if err != nil || !compacted {
		return assembled, err
	}

	// Compaction advanced the cursor — reassemble with the updated window.
	reassembled, _, err := d.assembleContext(ctx, ag, rfc, interjections)
	if err != nil {
		return assembled, fmt.Errorf("reassemble after compaction: %w", err)
	}
	return reassembled, nil
}

// summariseBatch calls the routine model to produce a summary of the given
// batch of messages.
func (d *Dispatcher) summariseBatch(ctx context.Context, ag *agent.Agent, roomID string, messages []room.Message) (string, error) {
	var batchContent strings.Builder
	batchContent.WriteString("## Compaction Request\n\n")
	batchContent.WriteString("Summarize the following conversation messages into a concise summary. Write this summary to your daily note.\n\n")
	for _, m := range messages {
		batchContent.WriteString(fmt.Sprintf("%s: %s\n", m.Sender.Name, m.Content))
	}

	provider, modelID, err := d.registry.ResolveTier(ag.Definition, inference.TierRoutine)
	if err != nil {
		return "", fmt.Errorf("resolve routine model for compaction: %w", err)
	}

	ch, err := provider.Infer(ctx, inference.ContextPayload{
		Model:        modelID,
		SystemPrompt: ag.Definition.RoleDescription,
		RFC:          batchContent.String(),
	})
	if err != nil {
		return "", fmt.Errorf("compaction inference: %w", err)
	}

	var summary strings.Builder
	streamComplete := false
	for chunk := range ch {
		if chunk.FinishReason != "" {
			streamComplete = true
		}
		summary.WriteString(chunk.Content)
	}
	if !streamComplete {
		return "", fmt.Errorf("compaction stream ended unexpectedly")
	}

	return summary.String(), nil
}

// writeSummaryToDailyNote appends a compaction summary entry to the agent's
// daily note file for today.
func (d *Dispatcher) writeSummaryToDailyNote(agentName, roomID, summary string) error {
	memoryDir := filepath.Join(d.paths.AgentDataDir(agentName), "memory")
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return fmt.Errorf("mkdir memory: %w", err)
	}

	todayFile := filepath.Join(memoryDir, time.Now().UTC().Format("2006-01-02")+".md")
	f, err := os.OpenFile(todayFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open daily note: %w", err)
	}
	defer f.Close()

	header := fmt.Sprintf("\n\n## Compacted summary from room %s (%s)\n\n", roomID, time.Now().UTC().Format("2006-01-02 15:04"))
	if _, err := f.WriteString(header); err != nil {
		return fmt.Errorf("write compaction header: %w", err)
	}
	if _, err := f.WriteString(summary); err != nil {
		return fmt.Errorf("write compaction summary: %w", err)
	}
	if _, err := f.WriteString("\n"); err != nil {
		return fmt.Errorf("write newline: %w", err)
	}
	return nil
}

// calculateCompactionBatch reads forward from the current cursor offset and
// returns how many messages should be compacted to remove at least bytesToRemove.
// It never returns more than maxAvailable.
func (d *Dispatcher) calculateCompactionBatch(roomID string, cursorOffset, bytesToRemove, maxAvailable int) (int, int, error) {
	if maxAvailable <= 0 || bytesToRemove <= 0 {
		return 0, 0, nil
	}

	msgs, err := room.ReadMessagesFromOffset(d.paths.RoomsDir(), roomID, cursorOffset, maxAvailable)
	if err != nil {
		return 0, 0, err
	}

	accumulated := 0
	batchSize := 0
	for _, m := range msgs {
		accumulated += len(m.Content)
		batchSize++
		if accumulated >= bytesToRemove {
			break
		}
	}

	return batchSize, accumulated, nil
}

// assembledContextSize returns the total byte size of the assembled context
// fields, used to determine if compaction is needed.
//
// CurrentRoomHistory excludes the last message because that message is
// included in RFC, so counting both would double-count it.
func assembledContextSize(assembled *memory.AssembledContext) int {
	total := len(assembled.SystemPrompt) + len(assembled.Memory) + len(assembled.RFC)
	for _, s := range assembled.DailyNotes {
		total += len(s)
	}
	for _, s := range assembled.CrossRoomFeed {
		total += len(s)
	}
	// Exclude the last history message since it is also in RFC.
	historyLen := len(assembled.CurrentRoomHistory)
	if historyLen > 0 {
		historyLen--
	}
	for i := 0; i < historyLen; i++ {
		total += len(assembled.CurrentRoomHistory[i])
	}
	for _, s := range assembled.RAGResults {
		total += len(s)
	}
	for _, s := range assembled.ToolSchemas {
		total += len(s)
	}
	total += len(assembled.TurnBudget)
	return total
}

// AssembledContextSize returns the total byte size of the assembled context.
func AssembledContextSize(assembled *memory.AssembledContext) int {
	return assembledContextSize(assembled)
}
