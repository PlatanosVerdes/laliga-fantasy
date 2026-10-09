package server

import (
	"net/http"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/necroporra"
)

// necroVote casts your two picks to the external app through the shared vote path. It moves no
// money, so it needs no two-step token; it still refuses when the server is read-only.
func (s *Server) necroVote(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	if !s.opts.AllowWrites {
		s.json(writer, http.StatusForbidden, map[string]any{"error": "el panel es de solo lectura"})
		return
	}
	body := s.body(request)
	a, b := text(body["a"]), text(body["b"])
	if a == "" || b == "" {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "faltan los dos equipos"})
		return
	}
	// The gameweek travels with the request so the vote lands on the round the page showed as
	// open; the configured one is only the fallback.
	gameweek := int(number(body["gw"]))
	if gameweek == 0 {
		gameweek = s.opts.NecroGameweek
	}
	if gameweek == 0 {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "falta la jornada"})
		return
	}

	cfg := necroporra.Config{Cookie: s.opts.NecroCookie, ActionID: s.opts.NecroAction}
	loggedOut, err := necroporra.CastVote(cfg, gameweek, a, b)
	if loggedOut {
		s.json(writer, http.StatusUnauthorized,
			map[string]any{"error": "sesión de la necroporra caducada: recaptura la cookie"})
		return
	}
	if err != nil {
		s.json(writer, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	s.json(writer, http.StatusOK, map[string]any{"ok": true})
}
