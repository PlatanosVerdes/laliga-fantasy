package server

import (
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/forecast"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

func TestMetricsSayWhenLaLigaHasUsPaused(t *testing.T) {
	text := prometheusText(httpx.QueueReport{Hosts: []httpx.HostReport{{
		Host: "fantasy-api.llt-services.com", PauseSeconds: 120,
		Scopes:  map[string]httpx.ScopeCounters{"instance": {Sent: 40, RateLimited: 1, Throttled: 3}},
		Latency: httpx.Latency{Counts: []int{0, 3, 1, 0, 0, 0, 0, 0, 1}, Sum: 41.5, Count: 5},
		Answers: map[string]int{"2xx": 4, "5xx": 1},
	}}})
	for _, line := range []string{
		`upstream_paused{upstream="fantasy-api.llt-services.com"} 1`,
		`upstream_pause_seconds{upstream="fantasy-api.llt-services.com"} 120`,
		`upstream_requests_total{upstream="fantasy-api.llt-services.com",scope="instance"} 40`,
		`upstream_rate_limited_total{upstream="fantasy-api.llt-services.com",scope="instance"} 1`,
		`upstream_refused_total{upstream="fantasy-api.llt-services.com",scope="instance"} 3`,
		`upstream_response_seconds_bucket{upstream="fantasy-api.llt-services.com",le="0.25"} 3`,
		`upstream_response_seconds_bucket{upstream="fantasy-api.llt-services.com",le="0.5"} 4`,
		`upstream_response_seconds_bucket{upstream="fantasy-api.llt-services.com",le="30"} 4`,
		`upstream_response_seconds_bucket{upstream="fantasy-api.llt-services.com",le="+Inf"} 5`,
		`upstream_response_seconds_count{upstream="fantasy-api.llt-services.com"} 5`,
		`upstream_responses_total{upstream="fantasy-api.llt-services.com",code="5xx"} 1`,
	} {
		if !strings.Contains(text, line) {
			t.Errorf("missing %s in:\n%s", line, text)
		}
	}
}

func TestMetricsNeedNoSession(t *testing.T) {
	freshRoot(t)
	if answer := visit(New(nil, Options{}).Handler(), "GET", "/metrics", ""); answer.Code != 200 ||
		!strings.Contains(answer.Body.String(), "# TYPE upstream_paused gauge") {
		t.Errorf("/metrics without a session: %d %.200s", answer.Code, answer.Body.String())
	}
}

func TestMetricsScoreOnlyPlayedMatchdays(t *testing.T) {
	two, five, nine := 2.0, 5.0, 9.0
	log := forecast.Log{
		8: {"T1": {Players: map[string]*forecast.Pick{
			"1": {XPts: 4, Points: &five}, "2": {XPts: 6, Points: &two}, "3": {XPts: 8, Points: &nine}}}},
		10: {"T1": {Players: map[string]*forecast.Pick{"1": {XPts: 4, Points: &five}}}},
	}
	universe := &model.Universe{Week: api.Week{WeekNumber: 10}}
	text := forecastText(log, universe, time.Now())
	for _, line := range []string{
		`fantasy_forecast_hit_ratio{week="8"} 0.6666666666666666`,
		`fantasy_forecast_mean_abs_error_points{week="8"} 2`,
		`fantasy_forecast_actual_over_forecast{week="8"} 0.8888888888888888`,
		`fantasy_forecast_players{week="8"} 3`,
	} {
		if !strings.Contains(text, line) {
			t.Errorf("missing %s in:\n%s", line, text)
		}
	}
	if strings.Contains(text, `week="10"`) {
		t.Errorf("the matchday in play was scored:\n%s", text)
	}
}
