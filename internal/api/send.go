package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

type Call struct {
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body"`
}

var ErrForeign = errors.New("esa escritura es de otra liga o de otro equipo: no la envio")

// Send makes one attempt only: a write is not idempotent, and a retry is how you bid twice.
func (m *Member) Send(call Call) (any, error) {
	if err := m.bound(call); err != nil {
		return nil, err
	}
	headers := config.APIHeaders()
	bearer, err := m.source.Bearer()
	if err != nil {
		return nil, err
	}
	headers["Authorization"] = "Bearer " + bearer

	var body []byte
	if call.Body != nil {
		headers["Content-Type"] = "application/json"
		if body, err = json.Marshal(call.Body); err != nil {
			return nil, err
		}
	}
	separator := "?"
	if strings.Contains(call.Path, "?") {
		separator = "&"
	}
	raw, err := httpx.Fetch(httpx.Request{
		URL: config.APIBase + call.Path + separator + "x-lang=es", Method: call.Method,
		Headers: headers, Body: body, Retries: 1, Tag: "raw", Priority: httpx.Write,
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var answer any
	if err := json.Unmarshal([]byte(raw), &answer); err != nil {
		return nil, err
	}
	return answer, nil
}

// After team or teams, a lowercase route word such as daily-reward is not an id; ids never are.
func (m *Member) bound(call Call) error {
	path, _, _ := strings.Cut(call.Path, "?")
	segments := strings.Split(path, "/")
	anchored := false
	for index := 0; index+1 < len(segments); index++ {
		next := segments[index+1]
		want := ""
		switch segments[index] {
		case "league", "leagues":
			want = m.league.id
		case "team", "teams":
			if routeWord(next) {
				continue
			}
			want = m.team
		default:
			continue
		}
		if next != want {
			return fmt.Errorf("%w: %s %s", ErrForeign, call.Method, call.Path)
		}
		anchored = true
	}
	for key, want := range map[string]string{"leagueId": m.league.id, "teamId": m.team} {
		value, ok := call.Body[key]
		if !ok {
			continue
		}
		if text(value) != want {
			return fmt.Errorf("%w: %s %s con %s %v", ErrForeign, call.Method, call.Path, key, value)
		}
		anchored = true
	}
	if !anchored {
		return fmt.Errorf("%w: %s %s no dice de que liga ni de que equipo es", ErrForeign,
			call.Method, call.Path)
	}
	return nil
}

func routeWord(segment string) bool {
	if segment == "" {
		return false
	}
	for _, char := range segment {
		if (char < 'a' || char > 'z') && char != '-' {
			return false
		}
	}
	return true
}
