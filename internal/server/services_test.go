package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/services"
)

func servicesServer(t *testing.T) *Server {
	t.Helper()
	registry := services.New(filepath.Join(t.TempDir(), "services.json"),
		services.Job{Key: ServiceHeartbeat, Label: "Latido", Default: Heartbeat,
			Min: 5 * time.Second, Max: time.Minute})
	// Read-only on purpose: an interval is local configuration, not a write against the game.
	return &Server{opts: Options{Services: registry, AllowWrites: false}}
}

func call(server *Server, method, body string) (int, map[string]any) {
	recorder := httptest.NewRecorder()
	server.services(recorder, httptest.NewRequest(method, "http://x/api/services",
		strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &out)
	return recorder.Code, out
}

func TestServicesListsTheClocks(t *testing.T) {
	code, out := call(servicesServer(t), http.MethodGet, "")
	list, _ := out["services"].([]any)
	if code != http.StatusOK || len(list) != 1 {
		t.Fatalf("%d %v", code, out)
	}
	entry := list[0].(map[string]any)
	if entry["interval"] != "20s" || entry["source"] != "default" ||
		entry["env"] != "FANTASY_HEARTBEAT_INTERVAL" {
		t.Errorf("entrada: %v", entry)
	}
}

func TestServicesChangesAClockEvenReadOnly(t *testing.T) {
	server := servicesServer(t)
	code, out := call(server, http.MethodPost, `{"key":"heartbeat","interval":"45s"}`)
	if code != http.StatusOK {
		t.Fatalf("en solo lectura tambien se cambia un intervalo: %d %v", code, out)
	}
	if server.heartbeat() != 45*time.Second {
		t.Errorf("el latido no ha cambiado: %v", server.heartbeat())
	}
	if code, _ := call(server, http.MethodPost, `{"key":"heartbeat","reset":true}`); code != 200 ||
		server.heartbeat() != Heartbeat {
		t.Errorf("por defecto vuelve a %v: %d %v", Heartbeat, code, server.heartbeat())
	}
}

func TestServicesRefusesWhatDoesNotFit(t *testing.T) {
	server := servicesServer(t)
	cases := map[string]int{
		`{"key":"heartbeat","interval":"1s"}`:     http.StatusBadRequest,
		`{"key":"heartbeat","interval":"pronto"}`: http.StatusBadRequest,
		`{"key":"nada","interval":"30s"}`:         http.StatusNotFound,
		`no es json`:                              http.StatusBadRequest,
	}
	for body, want := range cases {
		if code, out := call(server, http.MethodPost, body); code != want || out["error"] == "" {
			t.Errorf("%s: %d %v, esperaba %d", body, code, out, want)
		}
	}
	if server.heartbeat() != Heartbeat {
		t.Errorf("un rechazo no cambia nada: %v", server.heartbeat())
	}
}
