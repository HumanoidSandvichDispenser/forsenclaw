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

// migrate runs AutoMigrate for all models. Since this is pre-v1, we do not
// maintain versioned migrations; AutoMigrate creates tables if they don't exist.
func (s *SQLiteStore) migrate() error {
	return s.db.AutoMigrate(
		&room.Room{},
		&RoomParticipant{},
		&room.Message{},
		&CompactionCursor{},
	)
}
