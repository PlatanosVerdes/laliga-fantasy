package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// A matchday he was lined up for carries its forecast, and the one in play shows up before it
// has any points.
func TestAddForecasts(t *testing.T) {
	config.ForecastFile = filepath.Join(t.TempDir(), "forecasts.json")
	blob, _ := json.Marshal(map[string]any{
		"7": map[string]any{"1": map[string]any{"manager": "yo",
			"players": map[string]any{"42": map[string]any{"xpts": 7.2}}}},
		"8": map[string]any{"2": map[string]any{"manager": "otro",
			"players": map[string]any{"42": map[string]any{"xpts": 8.1}}}},
	})
	if err := os.WriteFile(config.ForecastFile, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	weeks := addForecasts("42", []map[string]any{
		{"week": 6.0, "points": 3.0}, {"week": 7.0, "points": 9.0}})
	if len(weeks) != 3 {
		t.Fatalf("weeks = %v, want 6, 7 and 8", weeks)
	}
	if _, has := weeks[0]["forecast"]; has {
		t.Error("J6 was not recorded, it has no forecast")
	}
	if weeks[1]["forecast"] != 7.2 || weeks[2]["forecast"] != 8.1 || weeks[2]["points"] != nil {
		t.Errorf("weeks = %v", weeks)
	}
}
