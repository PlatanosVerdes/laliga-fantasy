package render

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/advice"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
)

func season(high, low float64) futbolfantasy.Detail {
	return futbolfantasy.Detail{FetchedAt: time.Now(), MaxValue: high, MaxDate: "09 ago.",
		MinValue: low, MinDate: "21 oct."}
}

func usePages(t *testing.T, pages map[string]futbolfantasy.Detail) {
	t.Helper()
	was := FFPages
	FFPages = func(ffID string) (futbolfantasy.Detail, bool) {
		page, ok := pages[ffID]
		return page, ok
	}
	t.Cleanup(func() { FFPages = was })
}

func TestTheValueRangeLine(t *testing.T) {
	now := time.Now()
	page := season(52_140_968, 23_659_485)
	if got, want := ValueRange(page, 23_659_485, now),
		"Máx 52.1M (9 ago) · Mín 23.7M (21 oct) · ahora al 45 % de su máximo"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := ValueRange(page, 53_000_000, now); !strings.HasSuffix(got, "· ahora en su máximo") {
		t.Errorf("above the stored high: %q", got)
	}
	page.MaxDate, page.MinValue = "15 sept.", 0
	if got, want := ValueRange(page, 0, now), "Máx 52.1M (15 sep)"; got != want {
		t.Errorf("without a low or a value: %q, want %q", got, want)
	}
	page.FetchedAt = now.Add(-30 * time.Hour)
	if got := ValueRange(page, 0, now); !strings.HasSuffix(got, "· ficha de hace 30 h") {
		t.Errorf("a stale page says its age: %q", got)
	}
	if got := ValueRange(futbolfantasy.Detail{FetchedAt: now}, 10_000_000, now); got != "" {
		t.Errorf("a page without a high draws nothing: %q", got)
	}
}

// Near is within NearBand of the end, and only in a season wide enough that the two ends do
// not overlap.
func TestNearTheEndsOfHisSeason(t *testing.T) {
	page := season(20_000_000, 10_000_000)
	for _, check := range []struct {
		value     float64
		high, low bool
	}{
		{20_000_000, true, false}, {18_000_000, true, false}, {17_900_000, false, false},
		{11_000_000, false, true}, {11_100_000, false, false}, {9_000_000, false, true},
		{25_000_000, true, false}, {0, false, false},
	} {
		if got := nearHigh(check.value, page); got != check.high {
			t.Errorf("%v near the high: %v", check.value, got)
		}
		if got := nearLow(check.value, page); got != check.low {
			t.Errorf("%v near the low: %v", check.value, got)
		}
	}
	flat := season(10_500_000, 10_000_000)
	if nearHigh(10_300_000, flat) || nearLow(10_300_000, flat) {
		t.Error("a flat season is near neither end")
	}
	if nearHigh(10_000_000, season(10_000_000, 0)) {
		t.Error("without a low there is no range")
	}
}

// A sale is tagged at its high, a signing at its low, and a swap names who each tag is about.
func TestDecidirTagsSalesNearTheHighAndSigningsNearTheLow(t *testing.T) {
	usePages(t, map[string]futbolfantasy.Detail{
		"ff-high": season(20_000_000, 5_000_000), "ff-low": season(30_000_000, 6_000_000)})
	high := map[string]any{"id": "1", "ff_id": "ff-high", "name": "Arriba", "value": 19_000_000.0}
	low := map[string]any{"id": "2", "ff_id": "ff-low", "name": "Abajo", "value": 6_200_000.0}
	tags := func(card Card) string {
		var out []string
		for _, seg := range cardTags(card, time.Now()) {
			out = append(out, seg.T)
		}
		return strings.Join(out, " | ")
	}
	for _, check := range []struct {
		card Card
		want string
	}{
		{Card{Kind: "offer", Player: high}, "cerca de su máximo"},
		{Card{Kind: "offer", Player: low}, ""},
		{Card{Kind: "sign", Player: low}, "cerca de su mínimo"},
		{Card{Kind: "sign", Player: high}, ""},
		{Card{Kind: "swap", Player: low}, "cerca de su mínimo"},
		{Card{Kind: "swap", Player: high, In: low}, "Arriba cerca de su máximo | Abajo cerca de su mínimo"},
		{Card{Kind: "raise", Player: high}, ""},
	} {
		if got := tags(check.card); got != check.want {
			t.Errorf("%s of %s: %q, want %q", check.card.Kind, text(check.card.Player["name"]), got, check.want)
		}
	}
	for _, seg := range cardTags(Card{Kind: "offer", Player: high}, time.Now()) {
		if !strings.Contains(seg.Tip, "20.0M (9 ago)") {
			t.Errorf("the tip says which high: %q", seg.Tip)
		}
	}
}

// futbolfantasy's word on his fitness is drawn only where it differs from the panel's status,
// and only when it is understood.
func TestTheSecondOpinionOnlyWhenTheyDisagree(t *testing.T) {
	injured := &futbolfantasy.Mark{State: "lesionado", Reason: "Lesión en el sóleo", Since: "07/10"}
	page := func(mark *futbolfantasy.Mark) futbolfantasy.Detail {
		return futbolfantasy.Detail{FetchedAt: time.Now(), Mark: mark}
	}
	usePages(t, map[string]futbolfantasy.Detail{
		"ff-injured": page(injured), "ff-fit": page(&futbolfantasy.Mark{}),
		"ff-doubt": page(&futbolfantasy.Mark{State: "duda"}), "ff-unknown": page(nil),
		"ff-old": {FetchedAt: time.Now().Add(-30 * time.Hour), Mark: injured},
	})
	for _, check := range []struct{ ffID, status, want string }{
		{"ff-injured", "ok", "futbolfantasy: lesionado"},
		{"ff-injured", "", "futbolfantasy: lesionado"},
		{"ff-injured", "doubtful", "futbolfantasy: lesionado"},
		{"ff-injured", "injured", ""},
		{"ff-injured", "out_of_league", ""},
		{"ff-fit", "injured", "futbolfantasy: disponible"},
		{"ff-fit", "doubtful", "futbolfantasy: disponible"},
		{"ff-fit", "ok", ""},
		{"ff-fit", "suspended", ""},
		{"ff-doubt", "doubtful", ""},
		{"ff-doubt", "ok", "futbolfantasy: duda"},
		{"ff-unknown", "injured", ""},
		{"ff-missing", "injured", ""},
		{"ff-old", "ok", "futbolfantasy: lesionado · hace 30 h"},
	} {
		got := ""
		if seg := secondOpinion(map[string]any{"ff_id": check.ffID, "status": check.status}, false,
			time.Now()); seg != nil {
			got = seg.T
		}
		if got != check.want {
			t.Errorf("%s against %q: %q, want %q", check.ffID, check.status, got, check.want)
		}
	}
	seg := secondOpinion(map[string]any{"ff_id": "ff-injured", "status": "ok"}, false, time.Now())
	if want := "Su ficha de futbolfantasy lo da lesionado desde el 7 oct (Lesión en el sóleo); " +
		"LaLiga, disponible."; !strings.HasPrefix(seg.Tip, want) {
		t.Errorf("tip %q", seg.Tip)
	}
}

// stripped is a view without what the new fields add to it: the cards' tags, the eleven's
// badges and the squad rows' futbolfantasy tag.
func stripped(t *testing.T, view any) string {
	t.Helper()
	var tree any
	if err := json.Unmarshal([]byte(viewJSON(t, view)), &tree); err != nil {
		t.Fatal(err)
	}
	var walk func(node any) any
	walk = func(node any) any {
		switch value := node.(type) {
		case map[string]any:
			delete(value, "ff")
			for key, child := range value {
				value[key] = walk(child)
			}
			if tags, ok := value["tags"].([]any); ok {
				kept := []any{}
				for _, tag := range tags {
					if seg, ok := tag.(map[string]any); !ok || !strings.Contains(text(seg["c"]), "ff-tag") {
						kept = append(kept, tag)
					}
				}
				value["tags"] = kept
				if len(kept) == 0 {
					delete(value, "tags")
				}
			}
		case []any:
			for index, child := range value {
				value[index] = walk(child)
			}
		}
		return node
	}
	return viewJSON(t, walk(tree))
}

// The new fields are informative only: with every one of them saying the most it can (each of
// mine at the top of his season, every other player at the bottom of his, all of them injured
// on futbolfantasy and fit for LaLiga), the advice, the plan, the decisions, their order and the
// standing instructions are the ones drawn without them, and the page differs in the tags alone.
func TestTheExtrasChangeNoDecision(t *testing.T) {
	now := time.Now()
	type output struct {
		advice, swaps, cards, plan string
		views                      map[string]any
	}
	run := func(extras bool) output {
		universe := fillWorld()
		stored := map[string]futbolfantasy.Detail{}
		for _, player := range rows(universe["players"]) {
			ffID := text(player["ff_id"])
			page, _ := (&ceilings{}).fetch(ffID)
			if extras {
				value := number(player["value"])
				page.MaxValue, page.MaxDate, page.MinValue, page.MinDate = value*3, "09 ago.", value, "21 oct."
				if truthy(player["is_mine"]) {
					page.MaxValue, page.MinValue = value, value/3
				}
				page.Mark = &futbolfantasy.Mark{State: "lesionado", Reason: "Lesión", Since: "07/10"}
			}
			stored[ffID] = page
		}
		source := &ceilings{stored: stored, delay: time.Hour}
		FFPages = nil
		if extras {
			FFPages = source.keep
		}
		buckets := advice.Recommend(universe, 20_000_000, 0, 15)
		(&advice.Filler{Fetch: source.fetch, Stored: source.keep}).Enrich(buckets, 15)
		document := Document{Universe: universe, Advice: buckets, Generated: "21/10/2026 09:00",
			Swaps: advice.ReadySwaps(universe, buckets, 20_000_000)}
		players := []map[string]any{}
		for _, player := range rows(universe["players"]) {
			players = append(players, player)
		}
		price := 5_000_000.0
		armed := map[string]policies.Policy{"sk": {ID: "sk", AlwaysList: true, MinPrice: &price,
			AutoSell: true}, "rv": {ID: "rv", Raid: true}}
		_, views := document.Render()
		return output{advice: viewJSON(t, buckets), swaps: viewJSON(t, document.Swaps),
			cards: viewJSON(t, document.decisionCards(now)),
			plan: viewJSON(t, policies.Plan(players, armed, now)) +
				viewJSON(t, policies.RaidPlan(players, armed, 20_000_000, nil)),
			views: views}
	}
	was := FFPages
	t.Cleanup(func() { FFPages = was })
	without, with := run(false), run(true)

	for name, pair := range map[string][2]string{"advice": {with.advice, without.advice},
		"swaps": {with.swaps, without.swaps}, "cards": {with.cards, without.cards},
		"standing instructions": {with.plan, without.plan}} {
		if pair[0] != pair[1] {
			t.Errorf("%s differ with the extras:\n%s\nwithout:\n%s", name, pair[0], pair[1])
		}
	}
	if !strings.Contains(without.cards, `"Kind":"swap"`) || !strings.Contains(without.cards, `"Kind":"offer"`) {
		t.Fatalf("the fixture should decide a sale and a signing: %s", without.cards)
	}
	decide := viewJSON(t, with.views["decidir"])
	for _, want := range []string{"cerca de su máximo", "cerca de su mínimo", "futbolfantasy: lesionado",
		"ff: lesionado"} {
		if !strings.Contains(decide, want) {
			t.Errorf("Decidir should carry %q: %s", want, decide)
		}
	}
	if squad := viewJSON(t, with.views["squad"]); !strings.Contains(squad, "ff: lesionado") {
		t.Errorf("the squad should carry the badge: %s", squad)
	}
	for name := range without.views {
		if got, want := stripped(t, with.views[name]), stripped(t, without.views[name]); got != want {
			t.Errorf("%s differs beyond the tags:\n%s\nwant:\n%s", name, got, want)
		}
	}
}
