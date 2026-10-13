package usage

import (
	"sort"
	"strings"
	"time"
)

// Behaviour is what the counts cannot say: where a visit stumbles. A tab left at once, a quick
// hop through one tab on the way to another, a round trip, a dialog abandoned, a Back, a card
// closed as soon as it opened, a click on something that only looked like a button.
type Behaviour struct {
	Sessions   int
	Tabs       []TabStats
	Hops       []Count // "A → B" where A was left within BounceSeconds
	RoundTrips []Count // "A → B → A" where B was left within RoundTripSeconds
	Dialogs    []DialogStats
	Backs      []Count // "from → to", by count
	Drawers    []DrawerStats
	Dead       []Count
}

// TabStats is one tab: visits, visible time, the median visit and how many were bounces.
type TabStats struct {
	Name    string
	Visits  int
	Seconds float64
	Median  float64
	Bounces int
}

// DialogStats is one operation's dialog: opened, confirmed, closed without going through.
type DialogStats struct {
	Name                         string
	Opened, Confirmed, Cancelled int
}

// DrawerStats is one kind of card: how often it was opened and how often closed at once.
type DrawerStats struct {
	Name   string
	Times  int
	Median float64
	Quick  int
}

const (
	// BounceSeconds is a visit too short to have read anything: a wrong tab.
	BounceSeconds = 3
	// RoundTripSeconds is how briefly the middle tab of a there-and-back was looked at.
	RoundTripSeconds = 10
	// sessionGap splits the older events, which carry no session, into visits.
	sessionGap = 30 * time.Minute
)

// Behave reads the events as visits. Tabs are logged when left, so their order in a session is
// the order they were seen in.
func Behave(events []Event) Behaviour {
	out := Behaviour{}
	sessions := split(events)
	out.Sessions = len(sessions)

	tabs := map[string]*TabStats{}
	visits := map[string][]float64{}
	hops, trips, backs, dead := map[string]*Count{}, map[string]*Count{}, map[string]*Count{}, map[string]*Count{}
	dialogs := map[string]*DialogStats{}
	drawers := map[string]*DrawerStats{}
	drawerTimes := map[string][]float64{}

	for _, session := range sessions {
		var seen []Event
		for _, event := range session {
			switch event.Kind {
			case "tab":
				seen = append(seen, event)
				row := tabs[event.What]
				if row == nil {
					row = &TabStats{Name: event.What}
					tabs[event.What] = row
				}
				row.Visits++
				row.Seconds += event.Seconds
				if event.Seconds < BounceSeconds {
					row.Bounces++
				}
				visits[event.What] = append(visits[event.What], event.Seconds)
			case "nav":
				add(backs, event.Label+" → "+event.What, 0)
			case "dead":
				add(dead, event.Where+" · "+event.What, 0)
			case "drawer":
				row := drawers[event.What]
				if row == nil {
					row = &DrawerStats{Name: event.What}
					drawers[event.What] = row
				}
				row.Times++
				if event.Seconds < BounceSeconds {
					row.Quick++
				}
				drawerTimes[event.What] = append(drawerTimes[event.What], event.Seconds)
			case "op":
				verb, name, ok := strings.Cut(event.What, " ")
				if !ok {
					continue
				}
				row := dialogs[name]
				if row == nil {
					row = &DialogStats{Name: name}
					dialogs[name] = row
				}
				switch verb {
				case "abrir":
					row.Opened++
				case "confirmar":
					row.Confirmed++
				case "cancelar":
					row.Cancelled++
				}
			}
		}
		// The same tab twice in a row is the page reloaded or come back to, not a move.
		for i := 0; i+1 < len(seen); i++ {
			if seen[i].Seconds < BounceSeconds && seen[i].What != seen[i+1].What {
				add(hops, seen[i].What+" → "+seen[i+1].What, 0)
			}
			if i+2 < len(seen) && seen[i].What == seen[i+2].What && seen[i].What != seen[i+1].What &&
				seen[i+1].Seconds < RoundTripSeconds {
				add(trips, seen[i].What+" → "+seen[i+1].What+" → "+seen[i].What, 0)
			}
		}
	}

	for name, row := range tabs {
		row.Median = median(visits[name])
		out.Tabs = append(out.Tabs, *row)
	}
	sort.Slice(out.Tabs, func(i, j int) bool { return out.Tabs[i].Seconds > out.Tabs[j].Seconds })
	for name, row := range drawers {
		row.Median = median(drawerTimes[name])
		out.Drawers = append(out.Drawers, *row)
	}
	sort.Slice(out.Drawers, func(i, j int) bool { return out.Drawers[i].Times > out.Drawers[j].Times })
	for _, row := range dialogs {
		out.Dialogs = append(out.Dialogs, *row)
	}
	sort.Slice(out.Dialogs, func(i, j int) bool {
		one, two := out.Dialogs[i], out.Dialogs[j]
		if one.Opened+one.Confirmed != two.Opened+two.Confirmed {
			return one.Opened+one.Confirmed > two.Opened+two.Confirmed
		}
		return one.Name < two.Name
	})
	out.Hops, out.RoundTrips = byCount(hops), byCount(trips)
	out.Backs, out.Dead = byCount(backs), byCount(dead)
	return out
}

// split cuts the events into visits: by session where they carry one, by gaps where they do not.
func split(events []Event) [][]Event {
	bySession := map[string][]Event{}
	var order []string
	var loose [][]Event
	for _, event := range events {
		if event.Session != "" {
			if _, seen := bySession[event.Session]; !seen {
				order = append(order, event.Session)
			}
			bySession[event.Session] = append(bySession[event.Session], event)
			continue
		}
		if n := len(loose); n == 0 || event.At.Sub(loose[n-1][len(loose[n-1])-1].At) > sessionGap {
			loose = append(loose, nil)
		}
		loose[len(loose)-1] = append(loose[len(loose)-1], event)
	}
	for _, id := range order {
		loose = append(loose, bySession[id])
	}
	return loose
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}
