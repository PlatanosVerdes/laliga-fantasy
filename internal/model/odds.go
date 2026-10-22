package model

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/matching"
)

type Odds struct {
	Probability float64
	Week        *int
}

const FichaSource = "ficha"

// oddsWorkers bounds the pages read at once; futbolfantasy's own throttle spaces them anyway.
const oddsWorkers = 3

var (
	readFicha   = futbolfantasy.PlayerPageFor
	storedFicha = func(ffID string) string {
		detail, _ := futbolfantasy.StoredDetail(ffID)
		return detail.URL
	}
)

type oddsWanted struct {
	id, slug, ffID string
}

func squadOdds(raw []map[string]any, ownership map[string]slot, myTeamID string, bridge *Bridge,
	ttl time.Duration) map[string]*Odds {
	if myTeamID == "" {
		return nil
	}
	var wanted []oddsWanted
	for _, entry := range raw {
		id := text(entry["id"])
		if owned, ok := ownership[id]; !ok || owned.TeamID != myTeamID ||
			int(number(entry["positionId"])) == PositionCoach {
			continue
		}
		var trend Trend
		if bridge != nil {
			trend = bridge.Trends[id]
		}
		if trend.StartProbability != nil {
			continue
		}
		name := label(entry)
		if trend.FFName != "" {
			name = trend.FFName
		}
		slug := matching.SlugifyFF(name)
		if trend.FFID != "" {
			if url := storedFicha(trend.FFID); url != "" {
				slug = url[strings.LastIndex(url, "/")+1:]
			}
		}
		wanted = append(wanted, oddsWanted{id: id, slug: slug, ffID: trend.FFID})
	}
	return readOdds(wanted, ttl)
}

func readOdds(wanted []oddsWanted, ttl time.Duration) map[string]*Odds {
	out := map[string]*Odds{}
	var mu sync.Mutex
	var limited atomic.Bool
	jobs := make(chan oddsWanted)
	var workers sync.WaitGroup
	for range min(oddsWorkers, len(wanted)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if limited.Load() {
					continue
				}
				page, err := readFicha(job.slug, job.ffID, ttl)
				if err != nil {
					var tooMany *httpx.RateLimited
					if errors.As(err, &tooMany) && !limited.Swap(true) {
						slog.Warn("futbolfantasy rate limited: squad odds stopped")
					}
					slog.Debug("squad odds skipped", "slug", job.slug, "reason", err.Error())
					continue
				}
				chance, ok := page["start_probability"].(*int)
				if !ok || chance == nil {
					continue
				}
				odds := &Odds{Probability: float64(*chance)}
				if week, ok := page["start_week"].(*int); ok {
					odds.Week = week
				}
				mu.Lock()
				out[job.id] = odds
				mu.Unlock()
			}
		}()
	}
	for _, job := range wanted {
		jobs <- job
	}
	close(jobs)
	workers.Wait()
	return out
}

func (p *Player) applyOdds(trend *Trend, ficha *Odds) {
	switch {
	case trend != nil && trend.StartProbability != nil:
		p.StartProbability = trend.StartProbability
	case ficha != nil:
		chance := ficha.Probability
		p.StartProbability = &chance
		p.StartSource = FichaSource
		p.StartWeek = ficha.Week
	}
}

func (p *Player) score(status string) {
	// Unknown minutes and a price-derived baseline are both guesses; discount them
	// so a fringe player with no data cannot outrank a known starter.
	p.Confidence = 1.0
	if p.StartProbability == nil {
		p.Confidence *= NoTrendConfidence
	}
	if p.PriorBased {
		p.Confidence *= PriorConfidence
	}
	if p.Available {
		p.XPts = p.BaseWeek * Availability(p.StartProbability) * p.FixtureFactor * p.Confidence
		if status == "doubtful" {
			p.XPts *= DoubtfulXPts
		}
	}
}
