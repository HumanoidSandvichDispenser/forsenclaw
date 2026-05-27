package store

import "time"

// CompactionCursor tracks the last message number that has been compacted for
// a given agent+room pair.
type CompactionCursor struct {
	AgentName        string    `gorm:"primaryKey"`
	RoomID           int64     `gorm:"primaryKey"`
	CompactedNumber  int       `gorm:"default:0"`
	UpdatedAt        time.Time
}
