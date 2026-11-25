package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// CheckWait is how long /api/check holds the answer for the probe and the rebuild it sets off:
// a cold rebuild on the Pi has taken 45 s.
const CheckWait = 90 * time.Second

// check asks LaLiga at once whether anything moved, for a pull, the button, or a tab coming back.
// What changed still reaches the page over SSE; the answer only says when the check is over.
func (s *Server) check(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	if s.opts.Check == nil {
		s.json(writer, http.StatusNotImplemented, map[string]any{"error": "sin comprobación"})
		return
	}
	var body struct {
		Wait bool `json:"wait"`
	}
	_ = json.NewDecoder(request.Body).Decode(&body)
	answer := s.opts.Check()
	if !body.Wait {
		s.json(writer, http.StatusAccepted, map[string]any{"pending": true})
		return
	}
	select {
	case found := <-answer:
		s.json(writer, http.StatusOK, map[string]any{"probed": found.Probed,
			"rebuilt": found.Rebuilt, "checked_at": found.At.Format(time.RFC3339)})
	case <-time.After(CheckWait):
		s.json(writer, http.StatusOK, map[string]any{"pending": true})
	case <-request.Context().Done():
	}
}
