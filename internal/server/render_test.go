package server

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
)

// painter is a page that takes as long as the test says: each render waits for a release.
type painter struct {
	renders atomic.Int32
	release chan struct{}
}

func (p *painter) page() Rendered {
	Rendering.Reached("Fichas de futbolfantasy", 3, 10)
	<-p.release
	number := p.renders.Add(1)
	return Rendered{HTML: fmt.Sprintf("page %d", number), Views: map[string]any{"n": number}}
}

func paintedServer(t *testing.T) (*Server, *state.State, *painter) {
	t.Helper()
	config.StateDir = t.TempDir()
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	world := state.New(func() (*model.Universe, error) {
		return &model.Universe{Players: []model.Player{{ID: "1", Name: "Unai Lopez"}}}, nil
	})
	if err := world.Refresh("start"); err != nil {
		t.Fatal(err)
	}
	painted := &painter{release: make(chan struct{})}
	server := New(world, Options{Page: painted.page})
	go func() { painted.release <- struct{}{} }()
	if got := server.render().HTML; got != "page 1" {
		t.Fatalf("first render: %q", got)
	}
	return server, world, painted
}

func healthOf(t *testing.T, server *Server) map[string]any {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.health(recorder, httptest.NewRequest("GET", "http://x/healthz", nil))
	var out map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A rebuild that found nothing new still invalidates the page, and on the Pi drawing it again
// after the futbolfantasy details expired took forty seconds of a blank tab. The page drawn for
// the same version is served at once and the new one is drawn behind it.
func TestSameVersionServesTheLastPageWhileItRedraws(t *testing.T) {
	server, world, painted := paintedServer(t)
	if err := world.Refresh("mercado"); err != nil {
		t.Fatal(err)
	}
	page, ready := server.current()
	if !ready || page.HTML != "page 1" {
		t.Fatalf("the last page is served at once: %q %v", page.HTML, ready)
	}
	if healthOf(t, server)["page_ready"] != true {
		t.Error("the panel opens at once, so it is ready")
	}
	painted.release <- struct{}{}
	// The views the page asks for next are the redrawn ones.
	if got := server.render().HTML; got != "page 2" {
		t.Errorf("the redraw lands in the cache: %q", got)
	}
}

// A new version has to be drawn before it is shown: the request waits a little, then gets the
// page that follows the render's progress, which /healthz reports step by step.
func TestNewVersionShowsTheRenderInsteadOfABlankWait(t *testing.T) {
	server, world, painted := paintedServer(t)
	defer func(was time.Duration) { patience = was }(patience)
	patience = 20 * time.Millisecond
	if err := world.RefreshWith("bid", true); err != nil {
		t.Fatal(err)
	}
	if _, ready := server.current(); ready {
		t.Fatal("a page for an older version is not served for a new one")
	}
	health := healthOf(t, server)
	rendering, _ := health["rendering"].(map[string]any)
	if health["page_ready"] != false || rendering["step"] != "Fichas de futbolfantasy" ||
		rendering["done"] != 3.0 || rendering["total"] != 10.0 {
		t.Errorf("the wait page has the render's step to show: %v", health)
	}
	painted.release <- struct{}{}
	if got := server.render().HTML; got != "page 2" {
		t.Errorf("the render goes on after the request gave up on it: %q", got)
	}
	health = healthOf(t, server)
	if health["page_ready"] != true || health["rendering"] != nil {
		t.Errorf("drawn, the wait page reloads into it: %v", health)
	}
}

// Four tabs opened together used to queue four renders behind one lock; they share one.
func TestRequestsTogetherShareOneRender(t *testing.T) {
	server, world, painted := paintedServer(t)
	if err := world.RefreshWith("bid", true); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	pages := make([]string, 4)
	for index := range pages {
		wait.Add(1)
		go func() { defer wait.Done(); pages[index] = server.render().HTML }()
	}
	time.Sleep(20 * time.Millisecond)
	painted.release <- struct{}{}
	wait.Wait()
	if painted.renders.Load() != 2 {
		t.Errorf("one render for the four of them, plus the first: %d", painted.renders.Load())
	}
	for _, page := range pages {
		if page != "page 2" {
			t.Errorf("every request got the new page: %v", pages)
			break
		}
	}
}

func TestHealthCarriesTheQueueToLaLiga(t *testing.T) {
	server, _, _ := paintedServer(t)
	queue, _ := healthOf(t, server)["queue"].(map[string]any)
	if queue["rate"] != 2.0 || queue["burst"] != 10.0 || queue["hosts"] == nil {
		t.Errorf("queue %v", queue)
	}
}
