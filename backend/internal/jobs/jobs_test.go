package jobs

import (
	"context"
	"errors"
	"github.com/fengyuchen/mutiblog/internal/state"
	"path/filepath"
	"testing"
	"time"
)

func TestQueueDeduplicatesAndClaims(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := New(db)
	first, err := q.Enqueue(
		context.Background(),
		Job{Kind: "render", DedupeKey: "en:post:x", Payload: []byte(`{"id":"x"}`)},
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.Enqueue(
		context.Background(),
		Job{Kind: "render", DedupeKey: "en:post:x", Payload: []byte(`{"id":"x","fresh":true}`)},
	)
	if err != nil || first != second {
		t.Fatalf("ids %d %d err %v", first, second, err)
	}
	job, err := q.Claim(context.Background(), "render", "test")
	if err != nil || job == nil || job.ID != first {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	if err := q.Complete(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
}

func TestQueueDeduplicatesRunningJob(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := New(db)
	first, err := q.Enqueue(
		context.Background(),
		Job{Kind: "render", DedupeKey: "en:post:x", Payload: []byte(`{"revision":1}`)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = q.Claim(context.Background(), "render", "worker"); err != nil {
		t.Fatal(err)
	}
	second, err := q.Enqueue(
		context.Background(),
		Job{Kind: "render", DedupeKey: "en:post:x", Payload: []byte(`{"revision":2}`)},
	)
	if err != nil || first != second {
		t.Fatalf("ids %d %d err %v", first, second, err)
	}
	var jobs int
	if err := db.Write().QueryRow("SELECT count(*) FROM jobs WHERE kind='render'").Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 1 {
		t.Fatalf("jobs=%d", jobs)
	}
}

func TestQueueStartAndGetKeepTaskIdentity(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := New(db)
	first, err := q.Enqueue(
		context.Background(),
		Job{Kind: "import", DedupeKey: "one", Payload: []byte(`{"name":"one.md"}`)},
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.Enqueue(
		context.Background(),
		Job{Kind: "import", DedupeKey: "two", Payload: []byte(`{"name":"two.md"}`)},
	)
	if err != nil {
		t.Fatal(err)
	}
	started, err := q.Start(context.Background(), second, "request-two")
	if err != nil || !started {
		t.Fatalf("start=%t err=%v", started, err)
	}
	if started, err = q.Start(context.Background(), second, "duplicate"); err != nil || started {
		t.Fatalf("repeat start=%t err=%v", started, err)
	}
	job, err := q.Get(context.Background(), second)
	if err != nil || job.Status != "running" || string(job.Payload) != `{"name":"two.md"}` {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	firstJob, err := q.Get(context.Background(), first)
	if err != nil || firstJob.Status != "pending" {
		t.Fatalf("first=%#v err=%v", firstJob, err)
	}
}

func TestQueueStats(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := New(db)
	if _, err = q.Enqueue(context.Background(), Job{Kind: "render", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	insert := "INSERT INTO jobs(kind,payload,priority,status,max_attempts,run_after,created_at,updated_at) " +
		"VALUES('render','{}',100,'failed',3,?,?,?)"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = db.Write().Exec(insert, now, now, now); err != nil {
		t.Fatal(err)
	}
	stats, err := q.Stats(context.Background())
	if err != nil || stats.Pending != 1 || stats.Failed != 1 || stats.Running != 0 {
		t.Fatalf("stats=%#v err=%v", stats, err)
	}
}

func TestQueueDedupeSlidesRunAfter(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := New(db)
	base := time.Now()
	first, err := q.Enqueue(
		context.Background(),
		Job{
			Kind:      "render",
			DedupeKey: "hreflang:x:en",
			Payload:   []byte(`{"v":1}`),
			RunAfter:  base.Add(30 * time.Second),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	// A second arrival inside the merge window must slide the run time
	// forward instead of creating a second job (docs/13 P21).
	later := base.Add(60 * time.Second)
	second, err := q.Enqueue(
		context.Background(),
		Job{Kind: "render", DedupeKey: "hreflang:x:en", Payload: []byte(`{"v":2}`), RunAfter: later},
	)
	if err != nil || first != second {
		t.Fatalf("ids %d %d err %v", first, second, err)
	}
	job, err := q.Get(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	if job.RunAfter.Before(later.Add(-time.Second)) || job.RunAfter.After(later.Add(time.Second)) {
		t.Fatalf("run_after=%v, want ~%v", job.RunAfter, later)
	}
	if string(job.Payload) != `{"v":2}` {
		t.Fatalf("payload=%s", job.Payload)
	}
}

func TestQueueRetriesAndReclaimsWithoutConsumingAttempts(t *testing.T) {
	db, err := state.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := New(db)
	id, err := q.Enqueue(context.Background(), Job{Kind: "render", Payload: []byte(`{}`), MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	job, err := q.Claim(context.Background(), "render", "worker")
	if err != nil || job == nil {
		t.Fatalf("claim = %#v, %v", job, err)
	}
	if err := q.Fail(context.Background(), id, errors.New("temporary")); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	if err := db.Write().QueryRow("SELECT status,attempts FROM jobs WHERE id=?", id).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 {
		t.Fatalf("after retry: status=%q attempts=%d", status, attempts)
	}
	markRunning := "UPDATE jobs SET status='running',locked_at='2000-01-01T00:00:00Z',locked_by='dead' WHERE id=?"
	if _, err := db.Write().Exec(markRunning, id); err != nil {
		t.Fatal(err)
	}
	count, err := q.ReclaimStale(context.Background(), time.Second)
	if err != nil || count != 1 {
		t.Fatalf("reclaim count=%d err=%v", count, err)
	}
	if err := db.Write().QueryRow("SELECT status,attempts FROM jobs WHERE id=?", id).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 1 {
		t.Fatalf("after reclaim: status=%q attempts=%d", status, attempts)
	}
}
