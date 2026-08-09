// Package jobs implements the durable SQLite task queue shared by rendering and translation.
package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/fengyuchen/mutiblog/internal/state"
	"time"
)

type Job struct {
	ID                              int64
	Kind, DedupeKey                 string
	Payload                         json.RawMessage
	Priority, Attempts, MaxAttempts int
	RunAfter                        time.Time
	Status, LastError               string
	CreatedAt, UpdatedAt            time.Time
}
type Queue struct{ db *state.DB }

type Stats struct {
	Pending int `json:"pending"`
	Running int `json:"running"`
	Failed  int `json:"failed"`
}

func New(db *state.DB) *Queue { return &Queue{db: db} }
func (q *Queue) Stats(ctx context.Context) (Stats, error) {
	var stats Stats
	rows, err := q.db.Write().QueryContext(ctx, "SELECT status,count(*) FROM jobs GROUP BY status")
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return stats, err
		}
		switch status {
		case "pending":
			stats.Pending = count
		case "running":
			stats.Running = count
		case "failed":
			stats.Failed = count
		}
	}
	return stats, rows.Err()
}
func (q *Queue) Enqueue(ctx context.Context, j Job) (int64, error) {
	if j.Priority == 0 {
		j.Priority = 100
	}
	if j.MaxAttempts == 0 {
		j.MaxAttempts = 3
	}
	if j.RunAfter.IsZero() {
		j.RunAfter = time.Now()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var dedupe any
	if j.DedupeKey != "" {
		dedupe = j.DedupeKey
	}
	var id int64
	err := q.db.Tx(ctx, func(tx *sql.Tx) error {
		if j.DedupeKey != "" {
			var existing int64
			selectSQL := "SELECT id FROM jobs WHERE kind=? AND dedupe_key=? " +
				"AND status IN ('pending','running') ORDER BY id DESC LIMIT 1"
			err := tx.QueryRow(selectSQL, j.Kind, j.DedupeKey).
				Scan(&existing)
			if err == nil {
				// Workers resolve the current bundle from the index, so a job already
				// running will render the newest saved content rather than an obsolete
				// payload. Keeping a single job makes repeated Publish idempotent.
				// run_after slides forward on every arrival so a deferred
				// re-render (e.g. the hreflang merge window) coalesces bursts
				// into a single job that runs after the last request.
				_, err = tx.Exec(
					"UPDATE jobs SET payload=?,priority=MIN(priority,?),run_after=?,updated_at=? WHERE id=?",
					string(j.Payload),
					j.Priority,
					j.RunAfter.UTC().Format(time.RFC3339Nano),
					now,
					existing,
				)
				id = existing
				return err
			}
		}
		result, err := tx.Exec(
			"INSERT INTO jobs(kind,dedupe_key,payload,priority,status,max_attempts,"+
				"run_after,created_at,updated_at) VALUES(?,?,?,?, 'pending',?,?,?,?)",
			j.Kind,
			dedupe,
			string(j.Payload),
			j.Priority,
			j.MaxAttempts,
			j.RunAfter.UTC().Format(time.RFC3339Nano),
			now,
			now,
		)
		if err == nil {
			id, _ = result.LastInsertId()
		}
		return err
	})
	return id, err
}
func (q *Queue) Claim(ctx context.Context, kind, worker string) (*Job, error) {
	var job Job
	var payload, runAfter string
	now := time.Now().UTC().Format(time.RFC3339Nano)
	err := q.db.Tx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRow(
			"SELECT id,kind,COALESCE(dedupe_key,''),payload,priority,attempts,max_attempts,"+
				"run_after FROM jobs WHERE status='pending' AND kind=? AND run_after<=? ORDER BY priority,id LIMIT 1",
			kind,
			now,
		)
		if err := row.Scan(
			&job.ID, &job.Kind, &job.DedupeKey, &payload, &job.Priority,
			&job.Attempts, &job.MaxAttempts, &runAfter,
		); err != nil {
			return err
		}
		job.Payload = json.RawMessage(payload)
		job.RunAfter, _ = time.Parse(time.RFC3339Nano, runAfter)
		_, err := tx.Exec(
			"UPDATE jobs SET status='running',locked_by=?,locked_at=?,updated_at=? WHERE id=?",
			worker,
			now,
			now,
			job.ID,
		)
		return err
	})
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &job, err
}

// Start atomically claims an explicitly addressed task. It is used by request-
// initiated jobs such as uploads, where a goroutine must never accidentally
// claim another request's queued work.
func (q *Queue) Start(ctx context.Context, id int64, worker string) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := q.db.Write().
		ExecContext(
			ctx,
			"UPDATE jobs SET status='running',locked_by=?,locked_at=?,updated_at=? WHERE id=? AND status='pending'",
			worker, now, now, id,
		)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}
func (q *Queue) Complete(ctx context.Context, id int64) error {
	_, err := q.db.Write().
		ExecContext(
			ctx,
			"UPDATE jobs SET status='done',updated_at=? WHERE id=?",
			time.Now().UTC().Format(time.RFC3339Nano), id,
		)
	return err
}

// Get returns durable task state for status APIs. Payload remains owned by the
// task producer, allowing a completed import to expose its compact report.
func (q *Queue) Get(ctx context.Context, id int64) (*Job, error) {
	var job Job
	var payload, runAfter, createdAt, updatedAt string
	err := q.db.Read().
		QueryRowContext(
			ctx,
			"SELECT id,kind,COALESCE(dedupe_key,''),payload,priority,attempts,max_attempts,"+
				"run_after,status,COALESCE(last_error,''),created_at,updated_at FROM jobs WHERE id=?",
			id,
		).
		Scan(
			&job.ID, &job.Kind, &job.DedupeKey, &payload, &job.Priority,
			&job.Attempts, &job.MaxAttempts, &runAfter, &job.Status,
			&job.LastError, &createdAt, &updatedAt,
		)
	if err != nil {
		return nil, err
	}
	job.Payload = json.RawMessage(payload)
	job.RunAfter, _ = time.Parse(time.RFC3339Nano, runAfter)
	job.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	job.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &job, nil
}

func (q *Queue) UpdatePayload(ctx context.Context, id int64, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = q.db.Write().
		ExecContext(
			ctx,
			"UPDATE jobs SET payload=?,updated_at=? WHERE id=?",
			string(encoded), time.Now().UTC().Format(time.RFC3339Nano), id,
		)
	return err
}

// Touch records real worker progress. It is a heartbeat, not a retry, so it
// never changes the attempt counter.
func (q *Queue) Touch(ctx context.Context, id int64, worker string) error {
	_, err := q.db.Write().
		ExecContext(
			ctx,
			"UPDATE jobs SET locked_at=?,updated_at=? WHERE id=? AND status='running' AND locked_by=?",
			time.Now().UTC().Format(time.RFC3339Nano),
			time.Now().UTC().Format(time.RFC3339Nano), id, worker,
		)
	return err
}

// ReclaimStale returns abandoned work to the queue without penalising it. A
// process kill is infrastructure failure, not a failed job attempt.
func (q *Queue) ReclaimStale(ctx context.Context, staleAfter time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-staleAfter).Format(time.RFC3339Nano)
	result, err := q.db.Write().
		ExecContext(
			ctx,
			"UPDATE jobs SET status='pending',locked_by=NULL,locked_at=NULL,updated_at=? WHERE status='running' AND locked_at<?",
			time.Now().UTC().Format(time.RFC3339Nano), cutoff,
		)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (q *Queue) Fail(ctx context.Context, id int64, cause error) error {
	return q.db.Tx(ctx, func(tx *sql.Tx) error {
		var attempts, maxAttempts int
		selectAttempts := "SELECT attempts,max_attempts FROM jobs WHERE id=?"
		if err := tx.QueryRowContext(ctx, selectAttempts, id).Scan(&attempts, &maxAttempts); err != nil {
			return err
		}
		next := attempts + 1
		now := time.Now().UTC()
		status := "pending"
		runAfter := now
		if next >= maxAttempts {
			status = "failed"
		} else {
			backoff := time.Second * time.Duration(1<<min(next-1, 6))
			runAfter = now.Add(backoff)
		}
		_, err := tx.ExecContext(
			ctx,
			"UPDATE jobs SET status=?,attempts=?,last_error=?,run_after=?,locked_by=NULL,locked_at=NULL,updated_at=? WHERE id=?",
			status,
			next,
			cause.Error(),
			runAfter.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
			id,
		)
		return err
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
