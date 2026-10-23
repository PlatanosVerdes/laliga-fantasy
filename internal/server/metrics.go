package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/forecast"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

func (s *Server) metrics(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprint(writer, prometheusText(httpx.QueueStats()))
	if s.state == nil {
		return
	}
	if universe := s.state.Universe(); universe != nil {
		if log, err := forecast.Open(s.profile()).Load(); err == nil {
			fmt.Fprint(writer, forecastText(log, universe, time.Now()))
		}
	}
}

// forecastText is how each played matchday's forecast went, league-wide: no money, no names.
func forecastText(log forecast.Log, universe *model.Universe, now time.Time) string {
	var weeks []*forecast.Week
	for number := range log {
		if week := forecast.ReviewWeek(log, universe, number, now); week != nil && week.Complete && week.Counted > 0 {
			weeks = append(weeks, week)
		}
	}
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].Week < weeks[j].Week })
	var out strings.Builder
	series := func(name, help string, value func(*forecast.Week) (float64, bool)) {
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
		for _, week := range weeks {
			if got, ok := value(week); ok {
				fmt.Fprintf(&out, "%s{week=\"%d\"} %g\n", name, week.Week, got)
			}
		}
	}
	series("fantasy_forecast_hit_ratio", fmt.Sprintf("Share of fielded players who scored within %g points of their forecast.", forecast.HitWithin),
		func(w *forecast.Week) (float64, bool) { return float64(w.Hits) / float64(w.Counted), true })
	series("fantasy_forecast_mean_abs_error_points", "Average distance between a player's forecast and his points.",
		func(w *forecast.Week) (float64, bool) { return w.MeanAbsError, true })
	series("fantasy_forecast_actual_over_forecast", "Points the league's elevens scored over what was forecast for them: above 1, the forecast fell short.",
		func(w *forecast.Week) (float64, bool) {
			predicted, actual := 0.0, 0.0
			for _, team := range w.Managers {
				if !team.Short {
					predicted, actual = predicted+team.Forecast, actual+team.Actual
				}
			}
			return actual / predicted, predicted > 0
		})
	series("fantasy_forecast_players", "Fielded players compared on that matchday.",
		func(w *forecast.Week) (float64, bool) { return float64(w.Counted), true })
	return out.String()
}

// Named upstream_* rather than laliga_*: one alert rule covers every service that exports them.
func prometheusText(report httpx.QueueReport) string {
	var out strings.Builder
	series := func(name, help, kind string) {
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
	}
	series("upstream_paused", "1 while the upstream has us paused after a 429.", "gauge")
	for _, host := range report.Hosts {
		fmt.Fprintf(&out, "upstream_paused{upstream=%q} %d\n", host.Host, boolInt(host.PauseSeconds > 0))
	}
	series("upstream_pause_seconds", "Seconds left of the current pause.", "gauge")
	for _, host := range report.Hosts {
		fmt.Fprintf(&out, "upstream_pause_seconds{upstream=%q} %d\n", host.Host, host.PauseSeconds)
	}
	counters := []struct {
		name, help string
		value      func(httpx.ScopeCounters) int
	}{
		{"upstream_requests_total", "Requests sent to the upstream.", func(c httpx.ScopeCounters) int { return c.Sent }},
		{"upstream_rate_limited_total", "429 answers from the upstream.", func(c httpx.ScopeCounters) int { return c.RateLimited }},
		{"upstream_refused_total", "Requests the queue refused to send while paused or past their patience.", func(c httpx.ScopeCounters) int { return c.Throttled }},
	}
	for _, counter := range counters {
		series(counter.name, counter.help, "counter")
		for _, host := range report.Hosts {
			scopes := make([]string, 0, len(host.Scopes))
			for scope := range host.Scopes {
				scopes = append(scopes, scope)
			}
			sort.Strings(scopes)
			for _, scope := range scopes {
				fmt.Fprintf(&out, "%s{upstream=%q,scope=%q} %d\n", counter.name, host.Host, scope, counter.value(host.Scopes[scope]))
			}
		}
	}
	series("upstream_response_seconds", "How long the upstream took to answer, until its headers.", "histogram")
	for _, host := range report.Hosts {
		cumulative := 0
		for i, bound := range httpx.LatencyBuckets {
			if i < len(host.Latency.Counts) {
				cumulative += host.Latency.Counts[i]
			}
			fmt.Fprintf(&out, "upstream_response_seconds_bucket{upstream=%q,le=%q} %d\n", host.Host,
				strconv.FormatFloat(bound, 'f', -1, 64), cumulative)
		}
		fmt.Fprintf(&out, "upstream_response_seconds_bucket{upstream=%q,le=\"+Inf\"} %d\n", host.Host, host.Latency.Count)
		fmt.Fprintf(&out, "upstream_response_seconds_sum{upstream=%q} %g\n", host.Host, host.Latency.Sum)
		fmt.Fprintf(&out, "upstream_response_seconds_count{upstream=%q} %d\n", host.Host, host.Latency.Count)
	}
	series("upstream_responses_total", "Answers from the upstream by status class, or error when none came.", "counter")
	for _, host := range report.Hosts {
		classes := make([]string, 0, len(host.Answers))
		for class := range host.Answers {
			classes = append(classes, class)
		}
		sort.Strings(classes)
		for _, class := range classes {
			fmt.Fprintf(&out, "upstream_responses_total{upstream=%q,code=%q} %d\n", host.Host, class, host.Answers[class])
		}
	}
	return out.String()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
