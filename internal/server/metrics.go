package server

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

func (s *Server) metrics(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprint(writer, prometheusText(httpx.QueueStats()))
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
