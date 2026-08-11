package auth

import (
	"testing"
	"time"
)

func TestSessionStoreClearInvalidatesEverySession(t *testing.T) {
	store := NewSessionStore(time.Hour)
	first, _, err := store.Create("admin")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.Create("admin")
	if err != nil {
		t.Fatal(err)
	}
	store.Clear()
	if _, ok := store.Get(first); ok {
		t.Fatal("first session survived Clear")
	}
	if _, ok := store.Get(second); ok {
		t.Fatal("second session survived Clear")
	}
}
