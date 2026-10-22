package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx/httpxtest"
)

func TestAClientTaggedForWritesStillReadsWhileLaLigaHasUsPaused(t *testing.T) {
	fake := httpxtest.New(t)
	answered := 0
	fake.Handle("GET /api"+config.CMP+"/week/current", func(writer http.ResponseWriter, _ *http.Request) {
		answered++
		if answered == 1 {
			writer.Header().Set("Retry-After", "60")
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = writer.Write([]byte(`{"weekNumber":9}`))
	})
	t.Cleanup(httpx.SetTransport(fake))
	t.Cleanup(httpx.SetQueue(httpx.NewQueue(nil, httpx.DefaultLimits(), nil,
		httpx.LaLigaHosts()...)))

	client := NewGlobal()
	var limited *httpx.RateLimited
	if _, err := client.At(httpx.Probe).CurrentWeek(0); !errors.As(err, &limited) {
		t.Fatalf("the first answer is the 429: %v", err)
	}
	for _, priority := range []httpx.Priority{httpx.Interactive, httpx.Rebuild, httpx.Untagged} {
		var throttled *httpx.Throttled
		if _, err := client.At(priority).CurrentWeek(0); !errors.As(err, &throttled) ||
			throttled.Priority != priority {
			t.Errorf("%s went out during the pause: %v", priority, err)
		}
	}
	week, err := client.At(httpx.Write).CurrentWeek(0)
	if err != nil || week.WeekNumber != 9 || len(fake.Calls()) != 2 {
		t.Fatalf("week=%+v err=%v calls=%d", week, err, len(fake.Calls()))
	}
}
