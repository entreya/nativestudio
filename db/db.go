package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

//go:embed migrations/*.sql
var migrationFiles embed.FS

// DB wraps the SQLite connection.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the SQLite file at path and runs migrations.
func Open(path string) (*DB, error) {
	// PRAGMA foreign_keys = ON can be passed in DSN but let's execute it directly
	// Actually for modernc.org/sqlite, PRAGMAs can be passed via query string e.g. ?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)
	// busy_timeout makes SQLite retry for up to 5s instead of immediately
	// returning SQLITE_BUSY, which matters here because the chat handler, the
	// background summarizer, and the indexer's enrichment loop can all write
	// concurrently.
	dsn := fmt.Sprintf("%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Run schema migrations idempotently
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("failed to execute schema: %w", err)
	}
	if err := runMigrations(db); err != nil {
		return nil, err
	}
	// Existing databases predate persisted conversation summaries.
	if _, err := db.Exec(`ALTER TABLE sessions ADD COLUMN summary TEXT NOT NULL DEFAULT ''`); err != nil {
		rows, verifyErr := db.Query(`SELECT summary FROM sessions LIMIT 0`)
		if verifyErr != nil {
			return nil, fmt.Errorf("failed to migrate session summaries: %w", err)
		}
		rows.Close()
	}
	// Older populated sessions may still carry the original placeholder because
	// metadata generation did not exist or failed. Give them a useful immediate
	// fallback; the agent will replace it with model-generated metadata later.
	if _, err := db.Exec(`
		UPDATE sessions
		SET title = SUBSTR(TRIM(REPLACE(REPLACE((
			SELECT content FROM messages
			WHERE messages.session_id = sessions.id AND role = 'user'
			ORDER BY sequence ASC LIMIT 1
		), CHAR(10), ' '), CHAR(13), ' ')), 1, 80),
			summary = CASE WHEN summary = '' THEN 'The user asked: ' || (
				SELECT content FROM messages
				WHERE messages.session_id = sessions.id AND role = 'user'
				ORDER BY sequence ASC LIMIT 1
			) ELSE summary END
		WHERE LOWER(TRIM(title)) IN ('', 'new conversation', 'untitled conversation')
		  AND EXISTS (
			SELECT 1 FROM messages
			WHERE messages.session_id = sessions.id AND role = 'user'
		  )
	`); err != nil {
		return nil, fmt.Errorf("failed to backfill conversation metadata: %w", err)
	}

	return &DB{db}, nil
}

func runMigrations(database *sql.DB) error {
	if _, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var applied int
		if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, entry.Name()).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		if applied > 0 {
			continue
		}
		sqlBytes, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		tx, err := database.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, entry.Name()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// Close closes the underlying database connection.
func (d *DB) Close() error {
	return d.DB.Close()
}
