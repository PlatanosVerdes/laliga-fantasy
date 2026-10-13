package model

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func cachedPlayerPage(t *testing.T, slug, ffID string) {
	t.Helper()
	url := strings.ReplaceAll(config.FFPlayerURL, "{slug}", slug)
	sum := sha1.Sum([]byte("ff_player|" + url))
	path := filepath.Join(config.CacheDir, "ff_player_"+hex.EncodeToString(sum[:])[:20]+".cache")
	page := `<div data-jugador="` + ffID + `"></div>` +
		`<tr class="plegado plegable" data-posicion-laliga-fantasy="Titular">` +
		`<td class="data points x"><span class="high laliga-fantasy">76</span></td></tr>`
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A name slug can land on a namesake's page; his points must not become this player's baseline.
func TestDeepEnrichIgnoresAPageOfAnotherPlayer(t *testing.T) {
	config.CacheDir = t.TempDir()
	cachedPlayerPage(t, "angel-perez", "999")

	theirs, ours := "999", "123"
	stranger := &Player{Name: "Angel Perez", PriorBased: true, FFID: &ours}
	if fixed := DeepEnrich([]*Player{stranger}, 1, time.Hour); fixed != 0 || !stranger.PriorBased {
		t.Fatalf("enriched from another player's page: fixed=%d", fixed)
	}

	same := &Player{Name: "Angel Perez", PriorBased: true, FFID: &theirs}
	if fixed := DeepEnrich([]*Player{same}, 1, time.Hour); fixed != 1 || same.LastSeason != 76 {
		t.Fatalf("own page not used: fixed=%d last=%v", fixed, same.LastSeason)
	}
}
