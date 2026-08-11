package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
)

func TestPublicCommentViewUsesAnExplicitFieldAllowlist(t *testing.T) {
	comment := domain.Comment{
		SchemaVersion: 1,
		Kind:          "Comment",
		ID:            "comment-id",
		Subject:       domain.CommentSubject{Kind: "Post", ID: "post-id"},
		Status:        "approved",
		Author: domain.CommentAuthor{
			Name:      "Visitor",
			EmailHash: "private-email-digest",
			Website:   "https://example.com",
		},
		Content:         "Hello",
		Locale:          "en",
		CreatedAt:       time.Now().UTC(),
		IPHash:          "private-ip-digest",
		UserAgentFamily: "PrivateBrowser",
	}
	data, err := json.Marshal(toPublicCommentView(comment))
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(data)
	for _, forbidden := range []string{"schemaVersion", "subject", "status", "private-email-digest", "private-ip-digest", "PrivateBrowser"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("public comment JSON contains %q: %s", forbidden, encoded)
		}
	}
	for _, required := range []string{"comment-id", "Visitor", "https://example.com", "Hello"} {
		if !strings.Contains(encoded, required) {
			t.Fatalf("public comment JSON is missing %q: %s", required, encoded)
		}
	}
}
