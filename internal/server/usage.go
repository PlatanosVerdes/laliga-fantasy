package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/usage"
)

// usageRecord is the wire shape, kept apart from the stored one so a field added to the file
// does not quietly become an input.
type usageRecord struct {
	At      string  `json:"at"`
	Kind    string  `json:"kind"`
	What    string  `json:"what"`
	Where   string  `json:"where"`
	Label   string  `json:"label"`
	Seconds float64 `json:"seconds"`
	Phone   bool    `json:"phone"`
}

// usage takes a batch of events from the page. A bad event inside a good body is dropped rather
// than failing the request: the page must never lose a click of real work to a measurement it
// was making on the side.
//
// Nothing here is a write in the sense --read-only means, which is why it is allowed while
// writes are refused.
func (s *Server) usage(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		s.json(writer, http.StatusMethodNotAllowed, map[string]any{"error": "solo POST"})
		return
	}
	var batch []usageRecord
	// 256 KiB is far past MaxBatch events of this shape, and is here so a body that is not a
	// batch at all cannot be read into memory in full before being rejected.
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 256<<10)).
		Decode(&batch); err != nil {
		s.json(writer, http.StatusBadRequest, map[string]any{"error": "no es un lote de eventos"})
		return
	}
	if len(batch) > usage.MaxBatch {
		batch = batch[:usage.MaxBatch]
	}

	events := make([]usage.Event, 0, len(batch))
	for _, record := range batch {
		when, err := time.Parse(time.RFC3339, record.At)
		if err != nil {
			when = time.Now()
		}
		events = append(events, usage.Event{At: when, Kind: record.Kind, What: record.What,
			Where: record.Where, Label: record.Label, Seconds: record.Seconds,
			Phone: record.Phone})
	}
	stored, err := usage.Append(events)
	if err != nil {
		slog.Warn("could not record usage", "reason", err.Error())
		s.json(writer, http.StatusOK, map[string]any{"stored": 0})
		return
	}
	s.json(writer, http.StatusOK, map[string]any{"stored": stored})
}
