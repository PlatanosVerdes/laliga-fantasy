// Package schedule decides when to wake up and whether anything actually moved. It is a
// port of fantasy/schedule.py, and the harness compares the two decision for decision:
// the whole saving of the design is that Go must not rebuild when Python would not.
//
// A fixed interval is the wrong shape for this game. Nothing happens for hours, and then
// several things happen at an exact second: a clause unlocks, an auction closes, an offer
// expires, a match kicks off. Polling often enough to catch the second wastes the hours,
// and polling calmly enough for the hours arrives late to the second.
package schedule

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Constants shared with the Python side. Changing one here alone would make the harness
// red for a reason that is not a bug.
const (
	// Wake this much before a deadline: enough for one request to land on time, not so
	// much that we act on a clause which is still locked.
	Lead     = 2 * time.Second
	MinSleep = 3 * time.Second
	// Even with nothing moving and no deadline in sight, rebuild this often: values,
	// points and futbolfantasy all drift without generating an event.
	Ceiling = 15 * time.Minute
	// How close a deadline has to be for the league to count as busy. Inside this window
	// the probe runs at the base tick; outside it, it backs off — but never while
	// somebody has the page open, because for them the page is supposed to be live.
	BusyWindow = 10 * time.Minute
	IdleFactor = 4
	// How long a match is worth treating as under way: kick-off plus stoppage, halftime
	// and the minutes it takes for the last points to settle.
	MatchWindow = 130 * time.Minute
	// While the ball is rolling, points move and nothing announces it. Two minutes rather
	// than one because a live rebuild has to re-read the player master.
	LiveTick = 2 * time.Minute
	// matchState 7 is a finished match; see model.LoadFixtures.
	FinishedMatch = 7
)

// Kind of wake-up. A deadline rebuilds unconditionally; a probe usually does nothing.
const (
	Probe        = "probe"
	Rebuild      = "rebuild"
	KindDeadline = "deadline"
)

// Deadline is an instant that changes what we would do, and why in words: the reason is
// logged and shown, so it has to read plainly.
type Deadline struct {
	At  time.Time
	Why string
}

// Payload is the slice of the world the scheduler reads. Deliberately its own type rather
// than the model's: what matters here is dates and a couple of flags, and keeping it
// narrow means the scheduler can be fed a recorded JSON payload in a test.
type Payload struct {
	Market   []Listing          `json:"market"`
	Players  []Player           `json:"players"`
	Fixtures []Fixture          `json:"fixtures"`
	Week     Week               `json:"week"`
	Policies map[string]Policy  `json:"policies"`
}

type Listing struct {
	PlayerID string  `json:"player_id"`
	Expires  *string `json:"expires"`
}

type Player struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	TeamID      string  `json:"team_id"`
	IsMine      bool    `json:"is_mine"`
	ClauseUntil *string `json:"clause_locked_until"`
	Offers      []Offer `json:"offers"`
}

type Offer struct {
	ExpirationDate *string `json:"expirationDate"`
	Expires        *string `json:"expires"`
}

type Fixture struct {
	Kickoff   string `json:"kickoff"`
	State     int    `json:"state"`
	LocalID   string `json:"local_id"`
	VisitorID string `json:"visitor_id"`
	Local     string `json:"local"`
	Visitor   string `json:"visitor"`
}

type Week struct {
	WeekNumber      int    `json:"weekNumber"`
	ClosingWeekDate string `json:"closingWeekDate"`
}

type Policy struct {
	Raid bool `json:"raid"`
}

// parse is epoch from whatever shape the API used for this particular date.
func parse(value *string) (time.Time, bool) {
	if value == nil || *value == "" {
		return time.Time{}, false
	}
	text := strings.TrimSpace(*value)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if when, err := time.Parse(layout, text); err == nil {
			return when, true
		}
	}
	return time.Time{}, false
}

// Deadlines is every future instant that changes what we would do, soonest first.
//
// Only moments that need an action or a fresh read belong here. A clause that unlocks is a
// deadline when a raid is scheduled on it; otherwise it is a date on a calendar, and a
// calendar can be redrawn at leisure.
//
// `after` discards deadlines already acted on. Without it, an expiry the API keeps
// reporting a second into the future would be woken for again and again, and a tight loop
// of rebuilds is a worse failure than a late one.
func Deadlines(payload Payload, now time.Time, after time.Time) []Deadline {
	names := make(map[string]string, len(payload.Players))
	for _, player := range payload.Players {
		names[player.ID] = player.Name
	}

	var found []Deadline
	add := func(when time.Time, why string) {
		if when.After(now) && (after.IsZero() || when.After(after)) {
			found = append(found, Deadline{At: when, Why: why})
		}
	}

	for _, listing := range payload.Market {
		if when, ok := parse(listing.Expires); ok {
			who := names[listing.PlayerID]
			if who == "" {
				who = "un jugador"
			}
			add(when, "cierra la subasta de "+who)
		}
	}

	for _, fixture := range payload.Fixtures {
		kickoff, ok := parse(&fixture.Kickoff)
		if !ok {
			continue
		}
		pair := fixture.Local + " - " + fixture.Visitor
		if kickoff.After(now) {
			add(kickoff, "empieza "+pair)
		} else if fixture.State != FinishedMatch && kickoff.Add(MatchWindow).After(now) {
			// The final points land after the whistle, not on it.
			add(kickoff.Add(MatchWindow), "termina "+pair)
		}
	}

	if when, ok := parse(&payload.Week.ClosingWeekDate); ok {
		add(when, fmt.Sprintf("cierra la jornada %d", payload.Week.WeekNumber))
	}

	for _, player := range payload.Players {
		if policy, ok := payload.Policies[player.ID]; ok && policy.Raid {
			if when, ok := parse(player.ClauseUntil); ok {
				add(when, "se libera la clausula de "+player.Name)
			}
		}
		for _, offer := range player.Offers {
			when, ok := parse(offer.ExpirationDate)
			if !ok {
				when, ok = parse(offer.Expires)
			}
			if ok {
				add(when, "caduca una oferta por "+player.Name)
			}
		}
	}

	// Sorted by instant, ties by reason, so the choice is deterministic and matches
	// Python's sort over (timestamp, text) tuples.
	sort.SliceStable(found, func(i, j int) bool {
		if !found[i].At.Equal(found[j].At) {
			return found[i].At.Before(found[j].At)
		}
		return found[i].Why < found[j].Why
	})
	return found
}

// LiveMatches are the matches whose ball is presumably rolling, judged by the clock.
//
// The API's code for a live match is not known — 1 is pending and 7 is finished, and that
// is all that has been observed — so this asks whether we are inside the window that
// starts at kick-off. Over-polling a postponed match costs a request; missing a live one
// costs the points.
//
// `mineOnly` keeps the matches our own players are in. Somebody else's fixture moves the
// standings, ours moves our score, and only the second deserves the fast cadence.
func LiveMatches(payload Payload, now time.Time, mineOnly bool) []Fixture {
	teams := map[string]bool{}
	if mineOnly {
		for _, player := range payload.Players {
			if player.IsMine && player.TeamID != "" {
				teams[player.TeamID] = true
			}
		}
	}

	var live []Fixture
	for _, fixture := range payload.Fixtures {
		kickoff, ok := parse(&fixture.Kickoff)
		if !ok || fixture.State == FinishedMatch {
			continue
		}
		if kickoff.After(now) || now.After(kickoff.Add(MatchWindow)) {
			continue
		}
		if mineOnly && !teams[fixture.LocalID] && !teams[fixture.VisitorID] {
			continue
		}
		live = append(live, fixture)
	}
	return live
}

// Decision is when to wake, why, and what to do when we get there.
type Decision struct {
	At   time.Time
	Why  string
	Kind string
}

// NextWake picks the soonest thing that matters.
func NextWake(payload Payload, now time.Time, tick time.Duration, lastFull time.Time,
	watched bool, after time.Time) Decision {
	upcoming := Deadlines(payload, now, after)
	busy := watched || (len(upcoming) > 0 && upcoming[0].At.Sub(now) <= BusyWindow)

	if len(LiveMatches(payload, now, true)) > 0 {
		// Our own players on the pitch: points move and no endpoint announces it.
		if tick > LiveTick {
			tick = LiveTick
		}
		busy = true
	} else if len(LiveMatches(payload, now, false)) > 0 {
		busy = true // somebody else's match: the standings move, our score does not
	}

	sleep := tick
	if !busy {
		sleep = tick * IdleFactor
	}
	decision := Decision{At: now.Add(sleep), Why: "a ver si se ha movido algo", Kind: Probe}

	rebuildAt := lastFull.Add(Ceiling)
	if lastFull.IsZero() {
		rebuildAt = now.Add(Ceiling)
	}
	if rebuildAt.Before(decision.At) {
		decision = Decision{At: rebuildAt, Why: "reconstruccion completa periodica", Kind: Rebuild}
	}

	if len(upcoming) > 0 {
		due := upcoming[0].At.Add(-Lead)
		if due.Before(decision.At) {
			decision = Decision{At: due, Why: upcoming[0].Why, Kind: KindDeadline}
		}
	}

	if floor := now.Add(MinSleep); decision.At.Before(floor) {
		decision.At = floor
	}
	return decision
}

// --- the change detector ----------------------------------------------------

// ProbeParts is the digest of the two cheap answers — did the league move, did the market
// move — split in two because the halves mean different things. A new activity event means
// somebody changed hands, so every squad is stale; a market-only change is a listing or a
// rival bid and nothing moved owner.
//
// It must produce the same hex as fantasy/schedule.py for the same state, so what it
// hashes is shaped to match Python's json.dumps of the same lists.
func ProbeParts(activity []map[string]any, market []map[string]any) map[string]string {
	ids := make([]string, 0, 8)
	for index, event := range activity {
		if index >= 8 {
			break
		}
		source := event
		if raw, ok := event["raw"].(map[string]any); ok {
			source = raw
		}
		ids = append(ids, pythonRepr(source["id"]))
	}

	rows := make([]string, 0, len(market))
	for _, entry := range market {
		rows = append(rows, fmt.Sprintf("%s:%s:%s:%s",
			pythonRepr(firstPresent(entry, "id", "market_id")),
			amount(firstPresent(entry, "salePrice", "min_bid")),
			pythonRepr(firstPresent(entry, "numberOfBids", "bids")),
			pythonRepr(firstPresent(entry, "numberOfOffers", "offers"))))
	}
	sort.Strings(rows)

	return map[string]string{
		"events": sha1Hex(pythonJSON(ids)),
		"market": sha1Hex(pythonJSON(rows)),
	}
}

func firstPresent(entry map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := entry[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

// pythonRepr renders a value the way an f-string would, because that is what the Python
// digest hashed: None for absent, no trailing .0 for whole numbers.
func pythonRepr(value any) string {
	switch typed := value.(type) {
	case nil:
		return "None"
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case float64:
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	default:
		return fmt.Sprint(typed)
	}
}

// amount renders a number as Python's str(int(float(value))) does.
func amount(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case float64:
		return fmt.Sprintf("%d", int64(typed))
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(typed, "%f", &parsed); err != nil {
			return ""
		}
		return fmt.Sprintf("%d", int64(parsed))
	}
	return ""
}

// pythonJSON mimics json.dumps of a list of strings: ", " between items.
func pythonJSON(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		blob, _ := json.Marshal(value)
		quoted = append(quoted, string(blob))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func sha1Hex(text string) string {
	sum := sha1.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}
