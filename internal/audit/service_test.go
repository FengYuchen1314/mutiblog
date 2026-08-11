package audit

import (
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
)

func TestRecordAndListSecurityEvents(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(repository)
	if err := service.Record(Event{Action: "login", Outcome: "failed", Actor: "admin", ClientHash: "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := service.Record(Event{Action: "login", Outcome: "succeeded", Actor: "admin", ClientHash: "abc"}); err != nil {
		t.Fatal(err)
	}
	events, err := service.List(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != "login" || events[0].Outcome != "succeeded" || events[0].ClientHash != "abc" {
		t.Fatalf("events = %#v", events)
	}
}
