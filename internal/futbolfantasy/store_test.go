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
	read := Detail{FetchedAt: at("2026-10-20 23:50")}
	if !read.Fresh(at("2026-10-21 00:10")) {
		t.Error("before the adjustment has been read again it still stands")
	}
	if read.Fresh(at("2026-10-21 00:31")) {
		t.Error("forty minutes old, but from before the adjustment")
	}
	morning := Detail{FetchedAt: at("2026-10-21 00:40")}
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
	storeMu.Lock()
	store = nil
	storeMu.Unlock()
	url := strings.ReplaceAll(config.FFDetailURL, "{ff_id}", "stored")
	cachedPage(t, url, `puja_ideal = parsePujaIdeal(4500000 );`)
	read, err := FetchDetail("stored")
	if err != nil || read.IdealBid != 4_500_000 {
		t.Fatalf("read %+v %v", read, err)
	}
	if _, err := os.Stat(StoreFile()); err != nil {
		t.Fatalf("not stored: %v", err)
	}

	storeMu.Lock()
	store = nil
	storeMu.Unlock()
	if stored, fresh := LoadDetails(); stored != 1 || fresh != 1 {
		t.Errorf("loaded %d, %d fresh", stored, fresh)
	}
	if got, ok := StoredDetail("stored"); !ok || got.IdealBid != 4_500_000 {
		t.Errorf("after a restart: %+v %v", got, ok)
	}
	if got, err := DetailFor("stored"); err != nil || !got.FetchedAt.Equal(read.FetchedAt) {
		t.Errorf("a fresh stored ceiling is not read again: %+v %v", got, err)
	}
}

func restart() {
	storeMu.Lock()
	store = nil
	storeMu.Unlock()
}

// A file written before the season's range and the injury mark were kept still loads, and says
// nothing about them.
func TestAStoreFromBeforeTheExtrasStillLoads(t *testing.T) {
	config.CacheRoot = t.TempDir()
	old := `{"8693":{"ideal_bid":3200000,"url":"https://www.futbolfantasy.com/jugadores/nico-williams",` +
		`"fetched_at":"2026-10-20T22:40:00Z"}}`
	if err := os.WriteFile(StoreFile(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	restart()
	got, ok := StoredDetail("8693")
	if !ok || got.IdealBid != 3_200_000 || got.URL == "" || got.FetchedAt.IsZero() {
		t.Fatalf("old record: %+v %v", got, ok)
	}
	if got.MaxValue != 0 || got.MinValue != 0 || got.MaxDate != "" || got.Mark != nil {
		t.Errorf("an old record knows nothing of the extras: %+v", got)
	}
}

// What a real page says is stored whole and comes back after a restart.
func TestTheExtrasSurviveARestart(t *testing.T) {
	config.CacheRoot, config.CacheDir = t.TempDir(), t.TempDir()
	defer func(was bool) { httpx.Frozen = was }(httpx.Frozen)
	httpx.Frozen = true
	restart()
	page, err := os.ReadFile("testdata/detalle_sancet_lesionado.html")
	if err != nil {
		t.Fatal(err)
	}
	cachedPage(t, strings.ReplaceAll(config.FFDetailURL, "{ff_id}", "7006"), string(page))
	if _, err := FetchDetail("7006"); err != nil {
		t.Fatal(err)
	}
	restart()
	got, ok := StoredDetail("7006")
	want := Mark{State: "lesionado", Reason: "Lesión en el sóleo", Since: "07/10"}
	if !ok || got.MaxValue != 52_140_968 || got.MaxDate != "09 ago." || got.MinValue != 23_659_485 ||
		got.MinDate != "21 oct." || got.Mark == nil || *got.Mark != want {
		t.Errorf("after a restart: %+v %v", got, ok)
	}
}

func TestAWriteKeepsThePagesAnotherProcessStored(t *testing.T) {
	config.CacheRoot, config.CacheDir = t.TempDir(), t.TempDir()
	defer func(was bool) { httpx.Frozen = was }(httpx.Frozen)
	httpx.Frozen = true
	restart()
	LoadDetails()
	theirs := `{"theirs":{"ideal_bid":1000000,"fetched_at":"2026-10-21T08:00:00Z"}}`
	if err := os.WriteFile(StoreFile(), []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}
	cachedPage(t, strings.ReplaceAll(config.FFDetailURL, "{ff_id}", "ours"),
		`puja_ideal = parsePujaIdeal(2000000 );`)
	if _, err := FetchDetail("ours"); err != nil {
		t.Fatal(err)
	}
	if got, ok := StoredDetail("theirs"); !ok || got.IdealBid != 1_000_000 {
		t.Errorf("this process does not see theirs: %+v %v", got, ok)
	}
	restart()
	for id, bid := range map[string]float64{"theirs": 1_000_000, "ours": 2_000_000} {
		if got, ok := StoredDetail(id); !ok || got.IdealBid != bid {
			t.Errorf("%s on disk: %+v %v", id, got, ok)
		}
	}
}
