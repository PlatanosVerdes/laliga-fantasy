package render

import (
	"strings"
)

// The view model: what a tab shows, decided and worded here and drawn by the browser
// (assets/ui/view.js). Figures arrive already written, so each is formatted in one place; the
// client only adds the markup and the parts that move on their own (countdowns).

// Seg is a run of text, with the class it is drawn in, its tooltip, a sprite icon or a link.
type Seg struct {
	T    string `json:"t,omitempty"`
	C    string `json:"c,omitempty"`
	Tip  string `json:"tip,omitempty"`
	Icon string `json:"icon,omitempty"`
	Href string `json:"href,omitempty"`
	// Pid makes it open that player's card; Team, that manager's squad.
	Pid  string `json:"pid,omitempty"`
	Team string `json:"team,omitempty"`
}

// Chip is a row's pill: a live countdown to Until, or fixed text.
type Chip struct {
	Until string `json:"until,omitempty"`
	Label string `json:"label,omitempty"`
	T     string `json:"t,omitempty"`
	C     string `json:"c,omitempty"`
	Tip   string `json:"tip,omitempty"`
}

// Act is a button: its classes (the look), what it runs (Do) and with what. Text makes it a
// plain label beside the buttons instead.
type Act struct {
	Label string         `json:"label"`
	Class string         `json:"class,omitempty"`
	Do    string         `json:"do,omitempty"`
	Args  map[string]any `json:"args,omitempty"`
	Tip   string         `json:"tip,omitempty"`
	Off   bool           `json:"off,omitempty"`
	Text  bool           `json:"text,omitempty"`
	// Wrap is a tooltip on a span around a disabled button, which shows none of its own.
	Wrap string `json:"wrap,omitempty"`
}

// Find is what the filter bar reads off a row.
type Find struct {
	Pos   string  `json:"pos"`
	Price float64 `json:"price"`
	Name  string  `json:"name"`
	Text  string  `json:"text"`
}

// Row is ListRow as data. A player row carries the player (face, name, tags); any other has a
// Lead where the face would be and its own Name and lines. Head makes it a list's sub-heading.
type Row struct {
	Player map[string]any `json:"player,omitempty"`
	Lead   []Seg          `json:"lead,omitempty"`
	LeadC  string         `json:"lead_c,omitempty"`
	Name   string         `json:"name,omitempty"`
	Meta   []Seg          `json:"meta,omitempty"`
	Sub    []Seg          `json:"sub,omitempty"`
	Tags   []Seg          `json:"tags,omitempty"`
	Value  string         `json:"value,omitempty"`
	Note   []Seg          `json:"note,omitempty"`
	Why    string         `json:"why,omitempty"`
	Chips  []Chip         `json:"chips,omitempty"`
	Acts   []Act          `json:"acts,omitempty"`
	Tone   string         `json:"tone,omitempty"`
	Team   string         `json:"team,omitempty"`
	Pid    string         `json:"pid,omitempty"`
	Find   *Find          `json:"find,omitempty"`
	Head   string         `json:"head,omitempty"`
	HeadC  string         `json:"head_c,omitempty"`
}

// Block is a titled box. Kind names a component of its own for what is not a list of rows, and
// Data is what that component reads.
type Block struct {
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
	Count  *int   `json:"count,omitempty"`
	Sub    string `json:"sub,omitempty"`
	Rows   []Row  `json:"rows,omitempty"`
	Empty  string `json:"empty,omitempty"`
	Scroll int    `json:"scroll,omitempty"`
	Note   string `json:"note,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Data   any    `json:"data,omitempty"`
}

// View is a tab's screen: a main column, an aside, and optionally a second row under both.
type View struct {
	Filters bool    `json:"filters,omitempty"`
	Main    []Block `json:"main"`
	Aside   []Block `json:"aside,omitempty"`
	Row2    []Block `json:"row2,omitempty"`
}

func count(n int) *int { return &n }

// metaSegs is a second line written as "a · b · c", one segment per item.
func metaSegs(line string) []Seg {
	var out []Seg
	for _, part := range strings.Split(line, " · ") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, Seg{T: part})
		}
	}
	return out
}

func clockChip(stamp, label string) []Chip {
	if stamp == "" {
		return nil
	}
	return []Chip{{Until: stamp, Label: label}}
}

// act is a row's button with the classes button() gave it.
func act(label, kind, class, do string, args map[string]any) Act {
	return Act{Label: label, Class: strings.TrimSpace("mb mb-" + kind + " " + class), Do: do,
		Args: args}
}

// playerLine is a player's row with nothing else on it yet.
func playerLine(player map[string]any) Row {
	return Row{Player: RowPlayer(player)}
}

func findOf(player map[string]any, price float64) *Find {
	words := []string{text(player["name"]), text(player["team_short"]), text(player["team"]),
		text(player["owner"]), text(mapOf(player["market"])["seller"])}
	return &Find{Pos: text(player["position"]), Price: price,
		Name: strings.ToLower(text(player["name"])),
		Text: strings.ToLower(strings.Join(filterEmpty(words), " "))}
}

// outcomeSeg is the mark of how something ended, with its words as the tooltip.
func outcomeSeg(outcome, why string) Seg {
	mark := outcomeMarkOf(outcome, why)
	return Seg{C: "oi oi-" + mark.Tone, Tip: mark.Label, Icon: mark.Symbol}
}

// bidAct is the bid or offer button openBid reads, and what is on it when the bid is mine.
func bidActs(row map[string]any, class, label string) []Act {
	listing := mapOf(row["market"])
	marketID := text(listing["market_id"])
	if marketID == "" {
		return nil
	}
	operation := "bid"
	if text(listing["kind"]) == "venta" {
		operation = "buy_offer"
	}
	if label == "" {
		label = map[string]string{"bid": "Pujar", "buy_offer": "Ofertar"}[operation]
	}
	args := map[string]any{"market": marketID, "operation": operation,
		"bids": int64(number(listing["bids"])), "expires": text(listing["expires"]),
		"player": text(row["id"]), "name": text(row["name"]),
		"min": int64(number(listing["min_bid"])), "ideal": int64(number(row["ideal_bid"])),
		"value": int64(number(row["value"]))}
	if existing := text(listing["my_bid_id"]); existing != "" {
		amount := asFloat(listing["my_bid"])
		if operation == "buy_offer" {
			return []Act{{Label: "Ofreciste " + Money(amount), Class: "mine-bid", Text: true},
				{Label: "Retirar", Class: "danger", Do: "op", Args: map[string]any{
					"op": "cancel_offer", "market_id": marketID, "offer_id": existing,
					"player_id": text(row["id"]), "name": text(row["name"])}}}
		}
		args["bid"] = existing
		label, class = "Tu puja", "bid"
		if amount != nil {
			label = "Tu puja " + Money(amount)
		}
	}
	return []Act{{Label: label, Class: class, Do: "bid", Args: args}}
}
