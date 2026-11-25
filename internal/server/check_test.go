package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/engine"
)

func checkCall(server *Server, method, body string) (int, map[string]any) {
	recorder := httptest.NewRecorder()
	server.check(recorder, httptest.NewRequest(method, "http://x/api/check", strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &out)
	return recorder.Code, out
}

func TestCheckWaitsForTheProbeWhenAsked(t *testing.T) {
	at := time.Date(2026, 11, 25, 19, 0, 41, 0, time.UTC)
	asked := 0
	server := &Server{opts: Options{Check: func() <-chan engine.Check {
		asked++
		answer := make(chan engine.Check, 1)
		answer <- engine.Check{Probed: true, Rebuilt: true, At: at}
		return answer
	}}}

	code, out := checkCall(server, http.MethodPost, `{"wait":true}`)
	if code != http.StatusOK || out["probed"] != true || out["rebuilt"] != true ||
		out["checked_at"] != "2026-11-25T19:00:41Z" {
		t.Fatalf("%d %v", code, out)
	}
	if code, out := checkCall(server, http.MethodPost, `{}`); code != http.StatusAccepted ||
		out["pending"] != true {
		t.Fatalf("sin wait responde al momento: %d %v", code, out)
	}
	if code, _ := checkCall(server, http.MethodGet, ""); code != http.StatusMethodNotAllowed {
		t.Fatalf("un GET no debe sondear: %d", code)
	}
	if asked != 2 {
		t.Fatalf("esperaba dos comprobaciones, hubo %d", asked)
	}
}
