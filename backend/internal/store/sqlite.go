package store

import (
	"fmt"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/humanoidsandvichdispenser/hearth/backend/internal/room"
)

// canonicalMessagesDDL is the messages-table schema exactly as GORM generates
// it for room.Message. The tree migration originally wrote a tab-indented,
// space-aligned CREATE statement that glebarez/sqlite's migrator misparses
// (it reads NOT NULL columns as nullable, decides a rebuild is needed, then
// drops every other column during that rebuild — violating room_id NOT NULL).
// Rewriting the table in this canonical form makes AutoMigrate a clean no-op.
const canonicalMessagesDDL = "CREATE TABLE `messages` (" +
	"`id` integer PRIMARY KEY AUTOINCREMENT," +
	"`room_id` integer NOT NULL," +
	"`parent_id` integer," +
	"`timestamp` datetime," +
	"`sender` text," +
	"`clearance_tag` integer," +
	"`type` text," +
	"`content` text," +
	"`usage_input_tokens` integer DEFAULT 0," +
	"`usage_output_tokens` integer DEFAULT 0," +
	"`tool_calls` text," +
	"`tool_call_id` text DEFAULT \"\"," +
	"`tool_name` text DEFAULT \"\")"

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

	if err := s.normalizeMessagesTable(); err != nil {
		return fmt.Errorf("normalize messages table: %w", err)
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

// normalizeMessagesTable rebuilds the messages table with canonicalMessagesDDL
// when the stored schema is the legacy hand-written format that glebarez/sqlite
// cannot round-trip. It is data-preserving and idempotent: once the table is in
// canonical form (backtick-quoted columns, no tabs) it is a no-op. Runs before
// AutoMigrate so AutoMigrate never attempts its lossy rebuild.
func (s *SQLiteStore) normalizeMessagesTable() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}

	var ddl string
	if err := sqlDB.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='messages'`,
	).Scan(&ddl); err != nil {
		// No messages table yet (fresh install) — AutoMigrate will create it.
		return nil
	}

	// Canonical DDL has backtick-quoted columns and no tab indentation. Legacy
	// DDL is tab-indented and space-aligned, which is what GORM misparses.
	if !strings.Contains(ddl, "\t") && strings.Contains(ddl, "`room_id`") {
		return nil // already canonical
	}

	const cols = "id,room_id,parent_id,timestamp,sender,clearance_tag,type," +
		"content,usage_input_tokens,usage_output_tokens,tool_calls,tool_call_id,tool_name"

	_, err = sqlDB.Exec(`
		ALTER TABLE messages RENAME TO messages_legacy;
		` + canonicalMessagesDDL + `;
		INSERT INTO messages (` + cols + `) SELECT ` + cols + ` FROM messages_legacy;
		DROP TABLE messages_legacy;
		CREATE INDEX IF NOT EXISTS idx_messages_room_id ON messages(room_id);
		CREATE INDEX IF NOT EXISTS idx_messages_parent_id ON messages(parent_id);
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
