package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/agent"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/inference"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/paths"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
	"github.com/humanoidsandvichdispenser/hearth/backend/internal/store"
)

// compactionSummaryPrompt instructs the routine model to condense the messages
// that are leaving the context window into a durable summary.
const compactionSummaryPrompt = "You are compacting an agent's conversation history. " +
	"Summarize the following earlier messages into a concise record that preserves " +
	"durable facts, decisions, open questions, and commitments. Drop pleasantries and " +
	"transient back-and-forth. Write in past tense as a third-person recap. Output only " +
	"the summary."

// Compactor advances an agent's per-room compaction offset when the post-offset
// transcript outgrows the configured trigger. When the agent has daily notes
// enabled, the messages that fall out are first summarized (via the routine
// model) into today's note at the operating clearance, so continuity survives
// the drop; otherwise they are hard-dropped.
//
// Compactor satisfies agent.Compactor.
type Compactor struct {
	paths    *paths.Paths
	rooms    store.RoomRepository
	messages store.MessageRepository
	registry *inference.Registry
	trigger  int
	target   int
}

// NewCompactor creates a compactor. trigger and target are byte thresholds:
// compaction runs when the post-offset transcript exceeds trigger and drops the
// oldest messages until it is back under target. A non-positive trigger disables
// compaction.
func NewCompactor(
	p *paths.Paths,
	rooms store.RoomRepository,
	messages store.MessageRepository,
	registry *inference.Registry,
	trigger int,
	target int,
) *Compactor {
	return &Compactor{
		paths:    p,
		rooms:    rooms,
		messages: messages,
		registry: registry,
		trigger:  trigger,
		target:   target,
	}
}

// MaybeCompact compacts the agent's transcript for the room if it has grown past
// the configured trigger, bringing it back under the configured target. This is
// the automatic post-turn path; it is best-effort, so callers log errors but do
// not fail the turn. A non-positive configured trigger disables it.
func (c *Compactor) MaybeCompact(ctx context.Context, ag *agent.Agent, roomID int64) error {
	if c.trigger <= 0 {
		return nil
	}
	return c.compact(ctx, ag, roomID, c.trigger, c.target)
}

// Compact forces compaction now, down to the given target, regardless of the
// configured trigger — the entry point for an on-demand compact command. A
// non-positive target falls back to the configured target.
func (c *Compactor) Compact(ctx context.Context, ag *agent.Agent, roomID int64, target int) error {
	if target <= 0 {
		target = c.target
	}
	// trigger == target: compact whenever the transcript exceeds the target,
	// i.e. always bring it under the requested size.
	return c.compact(ctx, ag, roomID, target, target)
}

// compact drops the oldest messages until the post-offset transcript is under
// target, but only when it currently exceeds trigger.
func (c *Compactor) compact(ctx context.Context, ag *agent.Agent, roomID int64, trigger, target int) error {
	if roomID == 0 || c.messages == nil || c.rooms == nil {
		return nil
	}

	offset, err := c.messages.GetCompactionOffset(ctx, ag.Name(), roomID)
	if err != nil {
		return fmt.Errorf("get compaction offset: %w", err)
	}

	msgs, err := c.messages.GetMessages(ctx, roomID, store.ReadOpts{CompactionID: offset})
	if err != nil {
		return fmt.Errorf("get messages: %w", err)
	}

	total := 0
	for _, m := range msgs {
		total += len(m.Content)
	}
	if total <= trigger {
		return nil
	}

	// Drop oldest-first until back under target, but never drop the most recent
	// message: it anchors the just-finished turn and the next request's history.
	var dropped []room.Message
	var boundaryID int64
	remaining := total
	for i, m := range msgs {
		if remaining <= target || i == len(msgs)-1 {
			break
		}
		dropped = append(dropped, m)
		remaining -= len(m.Content)
		boundaryID = m.ID
	}
	if len(dropped) == 0 {
		return nil
	}

	r, err := c.rooms.GetRoom(ctx, roomID)
	if err != nil {
		return fmt.Errorf("get room: %w", err)
	}
	effClear := min(ag.Definition.Clearance, r.Clearance)

	if ag.Definition.FeatureFlags.DailyNotes {
		summary, err := c.summarize(ctx, ag, dropped)
		if err != nil {
			return fmt.Errorf("summarize: %w", err)
		}
		note := fmt.Sprintf(
			"Compacted %d earlier message(s) from room %d:\n\n%s",
			len(dropped), roomID, summary,
		)
		if err := WriteDailyNote(c.paths.AgentClearanceDir(ag.Name(), effClear), note); err != nil {
			return fmt.Errorf("write daily note: %w", err)
		}
	}

	return c.messages.SetCompactionOffset(ctx, ag.Name(), roomID, boundaryID)
}

// CompactionStats describes the current compaction state of an agent's
// transcript in a room: where the compaction cursor sits and how large the live
// (post-cursor) transcript is, alongside the configured thresholds.
type CompactionStats struct {
	Offset   int64 // cursor: messages at or before this ID are compacted away
	Messages int   // live message count after the cursor
	Bytes    int   // total content bytes of the live transcript
	Trigger  int   // configured auto-compaction trigger (0 = disabled)
	Target   int   // configured compaction target
}

// Stats reports the agent's compaction state for the room without changing
// anything: the cursor position and the size of the live transcript that would
// be subject to the next compaction.
func (c *Compactor) Stats(ctx context.Context, ag *agent.Agent, roomID int64) (CompactionStats, error) {
	stats := CompactionStats{Trigger: c.trigger, Target: c.target}
	if roomID == 0 || c.messages == nil {
		return stats, nil
	}

	offset, err := c.messages.GetCompactionOffset(ctx, ag.Name(), roomID)
	if err != nil {
		return stats, fmt.Errorf("get compaction offset: %w", err)
	}
	msgs, err := c.messages.GetMessages(ctx, roomID, store.ReadOpts{CompactionID: offset})
	if err != nil {
		return stats, fmt.Errorf("get messages: %w", err)
	}

	stats.Offset = offset
	stats.Messages = len(msgs)
	for _, m := range msgs {
		stats.Bytes += len(m.Content)
	}
	return stats, nil
}

// summarize condenses the dropped messages with the agent's routine model.
func (c *Compactor) summarize(ctx context.Context, ag *agent.Agent, msgs []room.Message) (string, error) {
	provider, modelID, err := c.registry.ResolveTier(ag.Definition, inference.TierRoutine)
	if err != nil {
		return "", fmt.Errorf("resolve routine model: %w", err)
	}

	var b strings.Builder
	for _, m := range msgs {
		if m.Content == "" {
			continue
		}
		b.WriteString(m.Sender.Name)
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}

	summary, _, err := inference.InferSync(ctx, provider, inference.ContextPayload{
		Model:        modelID,
		SystemPrompt: compactionSummaryPrompt,
		Request:      b.String(),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(summary), nil
}
