// Package state owns disposable SQLite runtime state and its migrations.
package state

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrRecreated = errors.New("state database was corrupt and has been recreated")

type DB struct {
	read, write *sql.DB
	path        string
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	read, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		read.Close()
		return nil, err
	}
	read.SetMaxOpenConns(min(4, runtime.NumCPU()))
	read.SetMaxIdleConns(2)
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxLifetime(0)
	d := &DB{read: read, write: write, path: path}
	if err = d.Read().Ping(); err != nil {
		return recoverCorrupt(path, d, err)
	}
	var integrity string
	if err = d.Read().QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		return recoverCorrupt(path, d, err)
	}
	if integrity != "ok" {
		return recoverCorrupt(path, d, fmt.Errorf("integrity check: %s", integrity))
	}
	if err = Migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func recoverCorrupt(path string, d *DB, cause error) (*DB, error) {
	_ = d.Close()
	corrupt := path + ".corrupt." + fmt.Sprint(time.Now().Unix())
	if err := os.Rename(path, corrupt); err != nil {
		return nil, fmt.Errorf("%w: %v", cause, err)
	}
	fresh, err := Open(path)
	if err != nil {
		return nil, err
	}
	return fresh, ErrRecreated
}
func (d *DB) Read() *sql.DB  { return d.read }
func (d *DB) Write() *sql.DB { return d.write }
func (d *DB) Close() error {
	if d == nil {
		return nil
	}
	_ = Checkpoint(d)
	a := d.read.Close()
	b := d.write.Close()
	if a != nil {
		return a
	}
	return b
}
func Checkpoint(d *DB) error { _, err := d.Write().Exec("PRAGMA wal_checkpoint(TRUNCATE)"); return err }
func (d *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	var err error
	for _, wait := range []time.Duration{0, 50 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond} {
		if wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		tx, e := d.Write().BeginTx(ctx, nil)
		if e != nil {
			err = e
			continue
		}
		err = fn(tx)
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		if !busy(err) {
			return err
		}
	}
	return err
}
func busy(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "SQLITE_LOCKED"))
}
func Migrate(d *DB) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if _, err = d.Write().Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)"); err != nil {
		return err
	}
	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil {
			return fmt.Errorf("invalid migration %s", name)
		}
		var exists int
		if err := d.Read().QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		sqlBytes, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if err = d.Tx(context.Background(), func(tx *sql.Tx) error {
			if _, err := tx.Exec(string(sqlBytes)); err != nil {
				return fmt.Errorf("migration %s: %w", name, err)
			}
			_, err := tx.Exec("INSERT INTO schema_migrations(version, applied_at) VALUES (?,?)", version, time.Now().UTC().Format(time.RFC3339Nano))
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
