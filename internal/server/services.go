package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/services"
)

// ServiceHeartbeat is the registry key of the push channel's heartbeat.
const ServiceHeartbeat = "heartbeat"

func (s *Server) heartbeat() time.Duration {
	if s.opts.Services == nil {
		return Heartbeat
	}
	if value := s.opts.Services.Get(ServiceHeartbeat); value > 0 {
		return value
	}
	return Heartbeat
}

// services lists the job clocks and changes one. Allowed in read-only: an interval is this
// server's own configuration, not an operation against the game.
func (s *Server) services(writer http.ResponseWriter, request *http.Request) {
	registry := s.opts.Services
	if registry == nil {
		s.json(writer, http.StatusNotImplemented, map[string]any{"error": "sin servicios"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		s.json(writer, http.StatusOK, map[string]any{"services": registry.List()})
	case http.MethodPost:
		var body struct {
			Key      string `json:"key"`
			Interval string `json:"interval"`
			Reset    bool   `json:"reset"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4096)).
			Decode(&body); err != nil {
			s.json(writer, http.StatusBadRequest, map[string]any{"error": "peticion no valida"})
			return
		}
		var err error
		if body.Reset || strings.TrimSpace(body.Interval) == "" {
			err = registry.Reset(body.Key)
		} else {
			var value time.Duration
			if value, err = services.ParseInterval(body.Interval); err == nil {
				err = registry.Set(body.Key, value)
			}
		}
		switch {
		case errors.Is(err, services.ErrUnknown):
			s.json(writer, http.StatusNotFound, map[string]any{"error": err.Error()})
		case errors.Is(err, services.ErrBounds) || errors.Is(err, services.ErrInvalid):
			s.json(writer, http.StatusBadRequest, map[string]any{"error": err.Error()})
		case err != nil:
			s.json(writer, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		default:
			s.json(writer, http.StatusOK, map[string]any{"services": registry.List()})
		}
	default:
		writer.Header().Set("Allow", "GET, POST")
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "metodo no permitido"})
	}
}
