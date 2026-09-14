// Package usage records what the page is actually used for, so a redesign is a measurement
// rather than a memory of last Sunday.
//
// The access log already answers part of it (how often the page is opened, which player cards,
// which operations) because every one of those is a request. What it cannot answer is anything
// that never leaves the browser: which tab is open, what is clicked inside it, which column a
// table is sorted by. With nine tabs and thirty sections that is the half that decides a layout,
// so it is the half recorded here.
//
// One user, so no sampling and no aggregation on the way in: the raw events fit in a file and
// the summary is computed when it is asked for. Time is counted only while the tab is visible,
// which is the difference between reading a page and leaving it open.
package usage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// Event is one thing that happened on the page. Kind is the verb: "tab" is a tab left, with the
// seconds it was visible, "click" is something pressed, "sort" is a table reordered, "op" is an
// operation started or confirmed. Which of the three nouns carries meaning depends on it.
type Event struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	What    string    `json:"what,omitempty"`
	Where   string    `json:"where,omitempty"`
	Label   string    `json:"label,omitempty"`
	Seconds float64   `json:"seconds,omitempty"`
	Phone   bool      `json:"phone,omitempty"`
}

// Kinds are the only verbs accepted: a page can change faster than this file, and an unknown
// kind from an old tab would land in the summary as a column nobody can read.
var Kinds = map[string]bool{"tab": true, "click": true, "sort": true, "op": true}

// MaxBatch is what one POST may carry. The collector batches every 15 seconds and on the way
// out, so a body larger than this is not a busy afternoon, it is a mistake or somebody else.
const MaxBatch = 200

// maxBytes caps the file. Old events are the ones a redesign has already answered, so the tail
// is what matters and the head is dropped when it grows past this.
const maxBytes = 4 << 20

var mu sync.Mutex

// Append writes the events that survive validation and reports how many that was. A rejected one
// is dropped in silence on purpose: instrumentation that can fail a request is not worth having.
func Append(events []Event) (int, error) {
	clean := make([]Event, 0, len(events))
	for _, event := range events {
		if !Kinds[event.Kind] {
			continue
		}
		if event.At.IsZero() {
			event.At = time.Now()
		}
		// A tab that was visible for a day was a laptop lid, not a reading. Twelve hours is
		// past anything real and still keeps a genuinely long Sunday afternoon.
		if event.Seconds < 0 || event.Seconds > 12*60*60 {
			event.Seconds = 0
		}
		event.What = trim(event.What)
		event.Where = trim(event.Where)
		event.Label = trim(event.Label)
		clean = append(clean, event)
	}
	if len(clean) == 0 {
		return 0, nil
	}

	mu.Lock()
	defer mu.Unlock()
	file, err := os.OpenFile(config.UsageFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, event := range clean {
		line, err := json.Marshal(event)
		if err != nil {
			continue
		}
		writer.Write(line)
		writer.WriteByte('\n')
	}
	if err := writer.Flush(); err != nil {
		return 0, err
	}
	trim0()
	return len(clean), nil
}

// trim keeps a label short enough to be a label. A button's text can be a whole sentence and
// the summary groups by this string, so an untrimmed one would be its own row every time.
func trim(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 80 {
		return value[:80]
	}
	return value
}

// trim0 drops the oldest half once the file passes the cap. Called with the lock held.
func trim0() {
	info, err := os.Stat(config.UsageFile)
	if err != nil || info.Size() <= maxBytes {
		return
	}
	body, err := os.ReadFile(config.UsageFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(body), "\n")
	os.WriteFile(config.UsageFile, []byte(strings.Join(lines[len(lines)/2:], "\n")), 0o600)
}

// Read is every event still on file, oldest first. A line that will not parse is skipped: the
// file is appended to by a live server and the last line can be half written.
func Read(since time.Time) ([]Event, error) {
	file, err := os.Open(config.UsageFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var events []Event
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		var event Event
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if !since.IsZero() && event.At.Before(since) {
			continue
		}
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	return events, scanner.Err()
}

// Count is one row of a summary: a name, how many times, and how long where that means anything.
type Count struct {
	Name    string
	Times   int
	Seconds float64
}

// Summary is what the events add up to.
type Summary struct {
	From, To time.Time
	Events   int
	Tabs     []Count // by visible seconds, which is the ranking a layout is decided on
	Clicks   []Count
	Sorts    []Count
	Phone    int
}

// Of turns events into the four rankings. Tabs are ranked by time and everything else by
// count, because a tab is somewhere you are and a click is something you do.
func Of(events []Event) Summary {
	summary := Summary{Events: len(events)}
	if len(events) == 0 {
		return summary
	}
	summary.From, summary.To = events[0].At, events[len(events)-1].At

	tabs := map[string]*Count{}
	clicks := map[string]*Count{}
	sorts := map[string]*Count{}
	for _, event := range events {
		if event.Phone {
			summary.Phone++
		}
		switch event.Kind {
		case "tab":
			add(tabs, event.What, event.Seconds)
		case "click", "op":
			add(clicks, label(event), 0)
		case "sort":
			add(sorts, event.Where+" · "+event.What, 0)
		}
	}
	summary.Tabs = byTime(tabs)
	summary.Clicks = byCount(clicks)
	summary.Sorts = byCount(sorts)
	return summary
}

// label is the section plus the thing pressed. Without the section the same word from three
// tables collapses into one row that says nothing about where to put it.
func label(event Event) string {
	name := event.Label
	if name == "" {
		name = event.What
	}
	if event.Where == "" {
		return name
	}
	return event.Where + " · " + name
}

func add(into map[string]*Count, name string, seconds float64) {
	if name == "" {
		return
	}
	row, found := into[name]
	if !found {
		row = &Count{Name: name}
		into[name] = row
	}
	row.Times++
	row.Seconds += seconds
}

func byCount(rows map[string]*Count) []Count {
	out := flatten(rows)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Times != out[j].Times {
			return out[i].Times > out[j].Times
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func byTime(rows map[string]*Count) []Count {
	out := flatten(rows)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Seconds != out[j].Seconds {
			return out[i].Seconds > out[j].Seconds
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func flatten(rows map[string]*Count) []Count {
	out := make([]Count, 0, len(rows))
	for _, row := range rows {
		out = append(out, *row)
	}
	return out
}

// Duration in the units a person would use: "4920s" is not an answer to how long a tab was open.
func Duration(seconds float64) string {
	switch {
	case seconds >= 3600:
		return fmt.Sprintf("%.1f h", seconds/3600)
	case seconds >= 60:
		return fmt.Sprintf("%.0f min", seconds/60)
	default:
		return fmt.Sprintf("%.0f s", seconds)
	}
}
