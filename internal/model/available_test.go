package model

import "testing"

// A doubt or a knock still plays: only a confirmed absence takes a player out of the eleven,
// which is what the lineup's automatic fix reads.
func TestOnlyConfirmedAbsencesAreUnavailable(t *testing.T) {
	for _, status := range []string{"ok", "doubtful", ""} {
		if severeStatus[status] {
			t.Errorf("%q should still play", status)
		}
	}
	for _, status := range []string{"injured", "suspended", "sanctioned"} {
		if !severeStatus[status] {
			t.Errorf("%q should be out", status)
		}
	}
}
