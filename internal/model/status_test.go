package model

import "testing"

// Miguel Sierra, 2026-10-12: his own page said injured for hours while the bulk list said ok.
func TestTheGraverStatusWins(t *testing.T) {
	for _, c := range []struct{ bulk, own, want string }{
		{"ok", "injured", "injured"},
		{"injured", "ok", "injured"},
		{"ok", "doubtful", "doubtful"},
		{"suspended", "doubtful", "suspended"},
		{"doubtful", "injured", "injured"},
		{"ok", "", "ok"},
		{"", "injured", "injured"},
	} {
		if got := graverStatus(c.bulk, c.own); got != c.want {
			t.Errorf("graverStatus(%q, %q) = %q, want %q", c.bulk, c.own, got, c.want)
		}
	}
}
