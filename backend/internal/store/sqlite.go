package store

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// SQLiteStore implements RoomRepository and MessageRepository using GORM over
// SQLite.
type SQLiteStore struct {
	db *gorm.DB
}

// Ensure SQLiteStore implements both repositories.
var _ RoomRepository = (*SQLiteStore)(nil)
var _ MessageRepository = (*SQLiteStore)(nil)

// NewSQLiteStore opens (or creates) the rooms database at dbPath and runs
// AutoMigrate for all models.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	// SQLite is single-writer; one connection avoids "database is locked" errors.
	sqlDB.SetMaxOpenConns(1)
	// FK enforcement is off by default in SQLite. Enable it on the connection.
	if _, err := sqlDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}
	return sqlDB.Close()
}

// DB returns the underlying *gorm.DB for advanced use (e.g. in tests).
func (s *SQLiteStore) DB() *gorm.DB {
	return s.db
}

// migrate runs schema migrations then AutoMigrate for all models.
func (s *SQLiteStore) migrate() error {
	if err := s.migrateMessagesToTree(); err != nil {
		return fmt.Errorf("messages tree migration: %w", err)
	}

	if err := s.db.AutoMigrate(
		&room.Room{},
		&RoomParticipant{},
		&room.Message{},
		&CompactionCursor{},
		&MessageBranchCursor{},
	); err != nil {
		return err
	}

	return s.populateRoomHeads()
}

// migrateMessagesToTree converts the old (room_id, number) composite-PK
// messages table to the new single-autoincrement-id tree schema.
// It is a no-op if the messages table already has an "id" column.
func (s *SQLiteStore) migrateMessagesToTree() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}

	// Check whether the new schema is already in place.
	rows, err := sqlDB.Query("SELECT COUNT(*) FROM pragma_table_info('messages') WHERE name = 'id'")
	if err != nil {
		// Table doesn't exist yet — nothing to migrate.
		return nil
	}
	var count int
	if rows.Next() {
		_ = rows.Scan(&count)
	}
	rows.Close()
	if count > 0 {
		return nil // already migrated
	}

	// Check if old messages table exists at all.
	var tableCount int
	tableRows, err := sqlDB.Query("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages'")
	if err != nil {
		return err
	}
	if tableRows.Next() {
		_ = tableRows.Scan(&tableCount)
	}
	tableRows.Close()
	if tableCount == 0 {
		return nil // fresh install — AutoMigrate will create the new schema
	}

	// Migrate: recreate messages with new schema, preserving data.
	_, err = sqlDB.Exec(`
		CREATE TABLE messages_new (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			room_id            INTEGER NOT NULL,
			parent_id          INTEGER,
			timestamp          DATETIME,
			sender             TEXT,
			clearance_tag      INTEGER,
			type               TEXT,
			content            TEXT,
			usage_input_tokens INTEGER DEFAULT 0,
			usage_output_tokens INTEGER DEFAULT 0,
			tool_calls         TEXT,
			tool_call_id       TEXT DEFAULT '',
			tool_name          TEXT DEFAULT ''
		);

		INSERT INTO messages_new (room_id, timestamp, sender, clearance_tag, type,
		    content, usage_input_tokens, usage_output_tokens, tool_calls, tool_call_id, tool_name)
		SELECT room_id, timestamp, sender, clearance_tag, type,
		    content,
		    COALESCE(usage_input_tokens, 0),
		    COALESCE(usage_output_tokens, 0),
		    tool_calls, tool_call_id, tool_name
		FROM messages
		ORDER BY room_id, number;

		UPDATE messages_new AS mn
		SET parent_id = (
			SELECT prev.id
			FROM messages_new prev
			WHERE prev.room_id = mn.room_id AND prev.id < mn.id
			ORDER BY prev.id DESC
			LIMIT 1
		);

		DROP TABLE messages;
		ALTER TABLE messages_new RENAME TO messages;

		CREATE INDEX IF NOT EXISTS idx_messages_room_id ON messages(room_id);
		CREATE INDEX IF NOT EXISTS idx_messages_parent_id ON messages(parent_id);

		ALTER TABLE rooms ADD COLUMN head INTEGER;

		UPDATE rooms SET head = (
			SELECT MAX(id) FROM messages WHERE room_id = rooms.id
		);

		ALTER TABLE compaction_cursors ADD COLUMN compacted_id INTEGER DEFAULT 0;
		UPDATE compaction_cursors SET compacted_id = compacted_number;
	`)
	return err
}

// populateRoomHeads sets head for any room that has messages but no head set.
// Runs after AutoMigrate on fresh installs.
func (s *SQLiteStore) populateRoomHeads() error {
	return s.db.Exec(`
		UPDATE rooms SET head = (
			SELECT MAX(id) FROM messages WHERE room_id = rooms.id
		) WHERE head IS NULL AND EXISTS (
			SELECT 1 FROM messages WHERE room_id = rooms.id
		)
	`).Error
}
