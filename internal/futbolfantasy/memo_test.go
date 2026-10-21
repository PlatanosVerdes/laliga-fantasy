package futbolfantasy

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

func cachedPage(t *testing.T, url, body string) {
	t.Helper()
	sum := sha1.Sum([]byte("ff_detail|" + url))
	path := filepath.Join(config.CacheDir, "ff_detail_"+hex.EncodeToString(sum[:])[:20]+".cache")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func maxValue(detail map[string]any) float64 {
	if value, ok := detail["max_value"].(*float64); ok && value != nil {
		return *value
	}
	return 0
}

// Redrawing the panel parsed the same pages every time; a fresh one is parsed once.
func TestAFreshPageIsParsedOnce(t *testing.T) {
	config.CacheDir = t.TempDir()
	url := strings.ReplaceAll(config.FFDetailURL, "{ff_id}", "memo")
	cachedPage(t, url, `var max_valor = 111;`)
	first, err := PlayerDetail("memo", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cachedPage(t, url, `var max_valor = 222;`)
	again, err := PlayerDetail("memo", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if maxValue(first) != 111 || maxValue(again) != 111 {
		t.Errorf("parsed again: %v then %v", maxValue(first), maxValue(again))
	}
}

// Kept no longer than the page it came from would have been served from disk.
func TestAStalePageIsParsedAgain(t *testing.T) {
	config.CacheDir = t.TempDir()
	url := strings.ReplaceAll(config.FFDetailURL, "{ff_id}", "memo-stale")
	cachedPage(t, url, `var max_valor = 111;`)
	if _, err := PlayerDetail("memo-stale", time.Hour); err != nil {
		t.Fatal(err)
	}
	cachedPage(t, url, `var max_valor = 222;`)
	detailMu.Lock()
	kept := detailSeen[url]
	kept.written = time.Now().Add(-2 * time.Hour)
	detailSeen[url] = kept
	detailMu.Unlock()
	again, err := PlayerDetail("memo-stale", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if maxValue(again) != 222 {
		t.Errorf("a stale parse was served: %v", maxValue(again))
	}
}

// A page counts as in hand only once parsed and while fresh; a page on disk is not read for it.
func TestKnownDetailIsOnlyAFreshParsedPage(t *testing.T) {
	config.CacheDir = t.TempDir()
	url := strings.ReplaceAll(config.FFDetailURL, "{ff_id}", "known")
	cachedPage(t, url, `var max_valor = 333;`)
	if _, ok := KnownDetail("known", time.Hour); ok {
		t.Fatal("a page only on disk is not in hand")
	}
	if _, err := PlayerDetail("known", time.Hour); err != nil {
		t.Fatal(err)
	}
	if detail, ok := KnownDetail("known", time.Hour); !ok || maxValue(detail) != 333 {
		t.Errorf("parsed and fresh: %v %v", detail, ok)
	}
	detailMu.Lock()
	kept := detailSeen[url]
	kept.written = time.Now().Add(-2 * time.Hour)
	detailSeen[url] = kept
	detailMu.Unlock()
	if _, ok := KnownDetail("known", time.Hour); ok {
		t.Error("a stale page is not in hand")
	}
}
