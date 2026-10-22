package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

func samplePreview(stage int) model.Preview {
	cash, clause := 4_500_000.0, 30_000_000.0
	seller := "Rafa"
	preview := model.Preview{Stage: stage, Cash: &cash, Mine: model.PreviewTeam{Name: "Jorge",
		Value: 50_000_000, Points: 250, Players: []model.PreviewPlayer{
			{ID: "11", Name: "Pedri", PositionID: 3, Team: "Barcelona", Value: 30_000_000, Status: "injured"},
			{ID: "10", Name: "Courtois", PositionID: 1, Team: "Real Madrid", Value: 20_000_000,
				Clause: &clause, Starter: true}}}}
	if stage >= model.PreviewMarket {
		preview.Market = []model.PreviewListing{{
			Player:  model.PreviewPlayer{ID: "30", Name: "Isco", PositionID: 3, Team: "Betis", Value: 9_000_000},
			Listing: model.Listing{MinBid: 9_500_000, Seller: &seller}}}
	}
	if stage >= model.PreviewRivals {
		preview.Rivals = []model.PreviewTeam{{Name: "Rafa", Points: 300, Value: 25_000_000,
			Players: []model.PreviewPlayer{{ID: "20", Name: "Lewandowski", Value: 25_000_000}}}}
	}
	return preview
}

// Each stage shows what it has and says, in Spanish, what is still on its way; none of them
// offers a single button to act with.
func TestThePreviewShowsWhatIsReadAndWaitsForTheRest(t *testing.T) {
	cases := []struct {
		stage    int
		has, not []string
	}{
		{model.PreviewMine, []string{"Mi equipo", "Courtois", "titular", "lesionado", "4.5M",
			"Cargando mercado…", "Cargando rivales…", "Cargando ofertas, movimientos"}, []string{"Isco", "Lewandowski"}},
		{model.PreviewMarket, []string{"Courtois", "Isco", "9.5M", "Cargando rivales…"},
			[]string{"Cargando mercado…", "Lewandowski"}},
		{model.PreviewRivals, []string{"Courtois", "Isco", "Lewandowski", "Cargando ofertas, movimientos"},
			[]string{"Cargando mercado…", "Cargando rivales…"}},
		{model.PreviewComplete, []string{"Dibujando el panel completo"}, []string{"Cargando ofertas"}},
	}
	for _, test := range cases {
		preview := samplePreview(min(test.stage, model.PreviewRivals))
		preview.Stage = test.stage
		page := previewPage(&preview)
		for _, want := range test.has {
			if !strings.Contains(page, want) {
				t.Errorf("stage %d lacks %q", test.stage, want)
			}
		}
		for _, unwanted := range test.not {
			if strings.Contains(page, unwanted) {
				t.Errorf("stage %d shows %q", test.stage, unwanted)
			}
		}
		if strings.Contains(page, "<button") || strings.Contains(page, "<form") {
			t.Errorf("stage %d offers something to act with", test.stage)
		}
		if !strings.Contains(page, fmt.Sprintf(`data-stage="%d"`, test.stage)) {
			t.Errorf("stage %d does not say which it is", test.stage)
		}
	}
}

func loadingServer(t *testing.T) (*Server, *state.State) {
	t.Helper()
	config.StateDir = t.TempDir()
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	world := state.New(func() (*model.Universe, error) {
		return &model.Universe{Players: []model.Player{{ID: "10", Name: "Courtois", IsMine: true}}}, nil
	})
	return New(world, Options{AllowWrites: true, Guard: writes.NewGuard(nil), LeagueID: "L",
		MyTeamID: "me"}), world
}

func post(handler http.Handler, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "http://x"+path,
		strings.NewReader(`{"id":"10","name":"Courtois","operation":"bid","amount":1}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// Until the first build is over every operation is refused by the server itself, whatever the
// page in front of it still shows, and with the message the page puts on screen.
func TestEveryOperationIsRefusedWhileTheLeagueLoads(t *testing.T) {
	server, world := loadingServer(t)
	world.ShowPreview(samplePreview(model.PreviewRivals))
	handler := asOwner(t, server)
	for path := range operations {
		answer := post(handler, path)
		var body map[string]any
		_ = json.Unmarshal(answer.Body.Bytes(), &body)
		if answer.Code != http.StatusServiceUnavailable || body["error"] != "Cargando la liga, espera a que termine" {
			t.Errorf("%s while loading: %d %s", path, answer.Code, answer.Body.String())
		}
	}
	if raw, err := os.ReadFile(config.PolicyFile); err == nil && strings.Contains(string(raw), "10") {
		t.Errorf("an instruction was written while loading: %s", raw)
	}

	// Reading is not acting: the lineup still answers a GET.
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://x/api/lineup", nil))
	if recorder.Code == http.StatusServiceUnavailable {
		t.Error("a read was refused as if it were a write")
	}

	if err := world.Refresh("arranque"); err != nil {
		t.Fatal(err)
	}
	if answer := post(handler, "/api/always"); answer.Code == http.StatusServiceUnavailable {
		t.Errorf("an instruction refused after the build: %s", answer.Body.String())
	}
}

// The guard says no on its own too, for whoever reaches it without going through a route.
func TestTheGuardRefusalMapsToUnavailable(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).writeError(recorder, writes.ErrLoading)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d", recorder.Code)
	}
}

// A tab opened during the cold start gets the preview, then the preview while the first page is
// drawn, and the panel once it is.
func TestTheColdStartServesThePreviewUntilThePanelIsDrawn(t *testing.T) {
	server, world := loadingServer(t)
	handler := asOwner(t, server)
	config.League = "L"
	painted := &painter{release: make(chan struct{})}
	server.opts.Page = painted.page
	open := func() string {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://x/", nil))
		return recorder.Body.String()
	}

	if page := open(); !strings.Contains(page, "Preparando la liga") {
		t.Fatalf("before any stage: %.200s", page)
	}
	world.ShowPreview(samplePreview(model.PreviewMine))
	if page := open(); !strings.Contains(page, "Mi equipo") || !strings.Contains(page, "Cargando mercado…") {
		t.Fatalf("stage one: %.300s", page)
	}
	if health := healthOf(t, server); health["loading"] != true || health["preview"] != 1.0 {
		t.Errorf("health while loading: %v", health)
	}
	if err := world.Refresh("arranque"); err != nil {
		t.Fatal(err)
	}
	defer func(was time.Duration) { patience = was }(patience)
	patience = 20 * time.Millisecond
	if page := open(); !strings.Contains(page, "Dibujando el panel completo") {
		t.Fatalf("while the first page is drawn: %.300s", page)
	}
	painted.release <- struct{}{}
	<-server.draw(server.state.RenderStamp())
	if page := open(); page != "page 1" {
		t.Errorf("once drawn: %.200s", page)
	}
}

func TestARequestTheQueueRefusedSaysWhenToTryAgain(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).writeError(recorder, &httpx.Throttled{Paused: true, RetryIn: 90 * time.Second})
	var body map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") != "90" ||
		body["error"] != "LaLiga nos frena: reintenta en 2 min" {
		t.Errorf("%d %q %v", recorder.Code, recorder.Header().Get("Retry-After"), body)
	}
}
