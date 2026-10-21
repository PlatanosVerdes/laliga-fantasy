package futbolfantasy

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

// A ceiling read before tonight's price adjustment is stale, however young; after it, only age
// makes it stale.
func TestACeilingGoesStaleAtThePriceAdjustment(t *testing.T) {
	at := func(stamp string) time.Time {
		when, err := time.ParseInLocation("2006-01-02 15:04", stamp, madrid)
		if err != nil {
			t.Fatal(err)
		}
		return when
	}
	read := Ceiling{FetchedAt: at("2026-10-20 23:50")}
	if !read.Fresh(at("2026-10-21 00:10")) {
		t.Error("before the adjustment has been read again it still stands")
	}
	if read.Fresh(at("2026-10-21 00:31")) {
		t.Error("forty minutes old, but from before the adjustment")
	}
	morning := Ceiling{FetchedAt: at("2026-10-21 00:40")}
	if !morning.Fresh(at("2026-10-21 12:00")) || morning.Fresh(at("2026-10-21 12:41")) {
		t.Errorf("after the adjustment it lasts CeilingTTL (%v)", CeilingTTL)
	}
	if got := morning.Age(at("2026-10-21 09:45")); got != "hace 9 h" {
		t.Errorf("age %q", got)
	}
}

// What is read is stored on disk, and a restart starts with it.
func TestCeilingsSurviveARestart(t *testing.T) {
	config.CacheRoot, config.CacheDir = t.TempDir(), t.TempDir()
	defer func(was bool) { httpx.Frozen = was }(httpx.Frozen)
	httpx.Frozen = true
	ceilingMu.Lock()
	ceilings = nil
	ceilingMu.Unlock()
	url := strings.ReplaceAll(config.FFDetailURL, "{ff_id}", "stored")
	cachedPage(t, url, `puja_ideal = parsePujaIdeal(4500000 );`)
	read, err := FetchCeiling("stored")
	if err != nil || read.IdealBid != 4_500_000 {
		t.Fatalf("read %+v %v", read, err)
	}
	if _, err := os.Stat(CeilingFile()); err != nil {
		t.Fatalf("not stored: %v", err)
	}

	ceilingMu.Lock()
	ceilings = nil
	ceilingMu.Unlock()
	if stored, fresh := LoadCeilings(); stored != 1 || fresh != 1 {
		t.Errorf("loaded %d, %d fresh", stored, fresh)
	}
	if got, ok := StoredCeiling("stored"); !ok || got.IdealBid != 4_500_000 {
		t.Errorf("after a restart: %+v %v", got, ok)
	}
	if got, err := CeilingFor("stored"); err != nil || !got.FetchedAt.Equal(read.FetchedAt) {
		t.Errorf("a fresh stored ceiling is not read again: %+v %v", got, err)
	}
}
