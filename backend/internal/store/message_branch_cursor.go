package store

// MessageBranchCursor tracks the last-visited child for each fork point in
// the conversation tree. It is mutable navigation state, kept separate from
// the append-only messages table.
//
// When the user switches branches at a fork, the cursor for the fork's parent
// message is upserted to point at the chosen child. Following cursors from
// any message downward reaches the tip of the most recently visited subtree.
type MessageBranchCursor struct {
	ParentID int64 `gorm:"primaryKey"`
	ChildID  int64 `gorm:"not null;index"`
}
