package server

import "testing"

func TestPublicMediaPathPatternRequiresCanonicalMonth(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		path string
		want bool
	}{
		{path: "2026/01/" + id + ".png", want: true},
		{path: "2026/12/" + id + ".webp", want: true},
		{path: "2026/00/" + id + ".png", want: false},
		{path: "2026/13/" + id + ".png", want: false},
		{path: "2026/99/" + id + ".png", want: false},
	} {
		if got := publicMediaPathPattern.MatchString(test.path); got != test.want {
			t.Errorf("publicMediaPathPattern.MatchString(%q) = %t, want %t", test.path, got, test.want)
		}
	}
}
