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

// buildCompactionPrompt builds the system prompt for the routine model that
// condenses messages leaving the context window. The note is written in the
// agent's own first-person voice — it is read back as the agent's recollection,
// in this room and (as a byproduct of cross-room note loading) in others — so it
// is anchored to its room and carries durable facts, decisions, and open threads
// rather than narrating the compaction mechanism. Caller-supplied instructions,
// when present, steer what to emphasize.
func buildCompactionPrompt(ag *agent.Agent, roomLabel, instructions string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s", ag.Name())
	if role := ag.Definition.RoleDescription; role != "" {
		fmt.Fprintf(&b, ", %s", role)
	}
	fmt.Fprintf(&b,
		". Summarize the earlier messages below from %s into a first-person note in your "+
			"own voice, recording what happened for your future self. Preserve durable facts, "+
			"decisions, open questions, and commitments; drop pleasantries and transient "+
			"back-and-forth. Write in past tense (\"I …\"). Output only the note.",
		roomLabel,
	)
	if instructions = strings.TrimSpace(instructions); instructions != "" {
		fmt.Fprintf(&b, "\n\nAdditional instructions: %s", instructions)
	}
	return b.String()
}

// roomLabel is a stable, human-readable anchor for a room, used both in the
// summary prompt and the note header so the note stays interpretable when it
// surfaces in another room.
func roomLabel(r *room.Room) string {
	if r.Name != "" {
		return r.Name
	}
	return fmt.Sprintf("room #%d", r.ID)
}

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
	return c.compact(ctx, ag, roomID, c.trigger, c.target, "")
}

// Compact forces compaction now, down to the given target, regardless of the
// configured trigger — the entry point for an on-demand compact command. A
// non-positive target falls back to the configured target. instructions, when
// present, steer what the summary emphasizes.
func (c *Compactor) Compact(ctx context.Context, ag *agent.Agent, roomID int64, target int, instructions string) error {
	if target <= 0 {
		target = c.target
	}
	// trigger == target: compact whenever the transcript exceeds the target,
	// i.e. always bring it under the requested size.
	return c.compact(ctx, ag, roomID, target, target, instructions)
}

// compact drops the oldest messages until the post-offset transcript is under
// target, but only when it currently exceeds trigger.
func (c *Compactor) compact(ctx context.Context, ag *agent.Agent, roomID int64, trigger, target int, instructions string) error {
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
		label := roomLabel(r)
		summary, err := c.summarize(ctx, ag, label, dropped, instructions)
		if err != nil {
			return fmt.Errorf("summarize: %w", err)
		}
		note := fmt.Sprintf("**%s**\n\n%s", label, summary)
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

// summarize condenses the dropped messages with the agent's routine model,
// writing in the agent's first-person voice and anchored to roomLabel.
func (c *Compactor) summarize(
	ctx context.Context,
	ag *agent.Agent,
	roomLabel string,
	msgs []room.Message,
	instructions string,
) (string, error) {
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
		SystemPrompt: buildCompactionPrompt(ag, roomLabel, instructions),
		Request:      b.String(),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(summary), nil
}
