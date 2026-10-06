package server

import (
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/schedule"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/state"
)

// shieldBudget is one matchday's two shields: the ones the log says were bought and the ones
// booked here that will be. Known is false with no calendar, and then nothing is refused.
type shieldBudget struct {
	Known  bool              `json:"known"`
	Round  schedule.Round    `json:"round"`
	Limit  int               `json:"limit"`
	Used   []state.ShieldUse `json:"used"`
	Booked []state.ShieldUse `json:"booked"`
}

func (b shieldBudget) Left() int {
	return b.Limit - len(b.Used) - len(b.Booked)
}

// Describe names who took each shield, for a refusal a person can act on.
func (b shieldBudget) Describe() string {
	parts := []string{}
	for _, use := range b.Used {
		parts = append(parts, use.Player+" "+use.At.Local().Format("02/01 15:04"))
	}
	for _, use := range b.Booked {
		parts = append(parts, use.Player+" "+use.At.Local().Format("02/01 15:04")+" programado")
	}
	return strings.Join(parts, ", ")
}

func (s *Server) shieldBudget(at time.Time) shieldBudget {
	round, used, ok := s.state.ShieldQuota(at)
	if !ok {
		return shieldBudget{}
	}
	budget := shieldBudget{Known: true, Round: round, Limit: schedule.ShieldsPerRound, Used: used,
		Booked: []state.ShieldUse{}}
	armed, err := policies.Load()
	if err != nil {
		return budget
	}
	for _, id := range policies.SortedIDs(armed) {
		for _, stamp := range armed[id].ShieldTimes() {
			when, err := time.Parse(time.RFC3339, stamp)
			if err == nil && round.Contains(when) {
				budget.Booked = append(budget.Booked, state.ShieldUse{Player: armed[id].Name, At: when})
			}
		}
	}
	return budget
}
