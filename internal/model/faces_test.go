package model

import "testing"

// Every player gets a face: the squad slot's first, then the market's, and the players list's
// for the many nobody owns and nobody lists.
func TestFaceForFallsBackToThePlayersList(t *testing.T) {
	entry := map[string]any{"id": "637", "image": "https://assets/p637.png"}
	if got := faceFor(entry, "", ""); got != "https://assets/p637.png" {
		t.Errorf("from the players list: %q", got)
	}
	if got := faceFor(entry, "https://market/p637.png", ""); got != "https://market/p637.png" {
		t.Errorf("the market's is more specific: %q", got)
	}
	if got := faceFor(entry, "https://market/p637.png", "https://squad/p637.png"); got != "https://squad/p637.png" {
		t.Errorf("the squad slot's first: %q", got)
	}
	if got := faceFor(map[string]any{}, "", ""); got != "" {
		t.Errorf("nothing known, no face: %q", got)
	}
}
