package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
)

// FFPages is the stored futbolfantasy page of a player, filled by the caller like Crests. What
// it holds beside the profitable ceiling (the season's high and low, the open injury) is only
// ever drawn: no card, order, score, plan or standing instruction reads it.
var FFPages func(ffID string) (futbolfantasy.Detail, bool)

func ffPage(player map[string]any) (futbolfantasy.Detail, bool) {
	ffID := text(player["ff_id"])
	if FFPages == nil || ffID == "" {
		return futbolfantasy.Detail{}, false
	}
	return FFPages(ffID)
}

// NearBand is how close to his season high or low a value has to be for Decidir to say so:
// within a tenth of it. The two bands must not overlap, or a flat season would be near both.
const NearBand = 0.10

func nearHigh(value float64, page futbolfantasy.Detail) bool {
	return value > 0 && distinctBands(page) && value >= page.MaxValue*(1-NearBand)
}

func nearLow(value float64, page futbolfantasy.Detail) bool {
	return value > 0 && distinctBands(page) && value <= page.MinValue*(1+NearBand)
}

func distinctBands(page futbolfantasy.Detail) bool {
	return page.MinValue > 0 && page.MaxValue*(1-NearBand) > page.MinValue*(1+NearBand)
}

// ffDay is the page's "09 ago." or "15 sept." as the panel writes a day: "9 ago", "15 sep".
func ffDay(raw string) string {
	day, month, found := strings.Cut(strings.TrimSpace(raw), " ")
	number, err := strconv.Atoi(day)
	if !found || err != nil {
		return strings.TrimSuffix(raw, ".")
	}
	month = strings.TrimSuffix(strings.ToLower(month), ".")
	if runes := []rune(month); len(runes) > 3 {
		month = string(runes[:3])
	}
	return fmt.Sprintf("%d %s", number, month)
}

// slashDay is the chart's "07/10" as "7 oct".
func slashDay(raw string) string {
	day, month, found := strings.Cut(raw, "/")
	dayNumber, dayErr := strconv.Atoi(day)
	monthNumber, monthErr := strconv.Atoi(month)
	if !found || dayErr != nil || monthErr != nil || monthNumber < 1 || monthNumber > 12 {
		return raw
	}
	return fmt.Sprintf("%d %s", dayNumber, months[monthNumber])
}

func withDay(amount float64, day string) string {
	if day == "" {
		return esMoney(amount)
	}
	return fmt.Sprintf("%s (%s)", esMoney(amount), ffDay(day))
}

// ValueRange is the line the player's card draws for his season on futbolfantasy: "Máx 52.1M
// (9 ago) · Mín 23.7M (21 oct) · ahora al 45 % de su máximo", and the page's age when it is
// being read again. Empty when the page has no high to tell.
func ValueRange(page futbolfantasy.Detail, value float64, now time.Time) string {
	if page.MaxValue <= 0 {
		return ""
	}
	parts := []string{"Máx " + withDay(page.MaxValue, page.MaxDate)}
	if page.MinValue > 0 {
		parts = append(parts, "Mín "+withDay(page.MinValue, page.MinDate))
	}
	if value > 0 {
		if share := math.Round(value / page.MaxValue * 100); share >= 100 {
			parts = append(parts, "ahora en su máximo")
		} else {
			parts = append(parts, fmt.Sprintf("ahora al %.0f %% de su máximo", share))
		}
	}
	if !page.Fresh(now) {
		parts = append(parts, "ficha de "+page.Age(now))
	}
	return strings.Join(parts, " · ")
}

// rangeTag is Decidir's word on where a sale or a signing sits in his season, or nil when it
// sits nowhere near either end. Named when the card is about two players.
func rangeTag(player map[string]any, selling, named bool, now time.Time) *Seg {
	page, ok := ffPage(player)
	if !ok {
		return nil
	}
	value := number(player["value"])
	label, end, day := "cerca de su máximo", page.MaxValue, page.MaxDate
	if selling && !nearHigh(value, page) {
		return nil
	}
	if !selling {
		if !nearLow(value, page) {
			return nil
		}
		label, end, day = "cerca de su mínimo", page.MinValue, page.MinDate
	}
	tip := fmt.Sprintf("Según futbolfantasy, %s de la temporada fue %s; ahora vale %s.",
		strings.TrimPrefix(label, "cerca de su "), withDay(end, day), esMoney(value))
	if !page.Fresh(now) {
		label += " · " + page.Age(now)
	}
	if named {
		label = text(player["name"]) + " " + label
	}
	return &Seg{T: label, C: "ff-tag", Tip: tip + " Solo informativo."}
}

// secondOpinion is futbolfantasy's word on his fitness, only where it differs from the status the
// panel goes by. The page speaks of injuries only, so an available verdict says nothing against a
// sanction; an injury still open there against a fit player here, or the other way round, does.
// Nil when they agree, when the page has not been read or when what it says is not understood.
func secondOpinion(player map[string]any, named bool, now time.Time) *Seg {
	page, ok := ffPage(player)
	if !ok || page.Mark == nil {
		return nil
	}
	status := strings.ToLower(text(player["status"]))
	if status == "out_of_league" || status == "unknown" {
		return nil
	}
	ours, theirs := statusLabels[status], page.Mark.State
	if ours == theirs || (theirs == "" && ours == "sancionado") {
		return nil
	}
	word := theirs
	if word == "" {
		word = "disponible"
	}
	label := "futbolfantasy: " + word
	tip := "Su ficha de futbolfantasy lo da " + word
	if page.Mark.Since != "" {
		tip += " desde el " + slashDay(page.Mark.Since)
	}
	if page.Mark.Reason != "" {
		tip += " (" + page.Mark.Reason + ")"
	}
	if ours == "" {
		ours = "disponible"
	}
	tip += "; LaLiga, " + ours + ". Solo informativo: no cambia ninguna decisión."
	if !page.Fresh(now) {
		label += " · " + page.Age(now)
	}
	if named {
		label = text(player["name"]) + " · " + label
	}
	return &Seg{T: label, C: "ff-tag ff-2nd", Tip: tip}
}

// pitchOpinion is secondOpinion short enough for a shirt on the eleven or a squad row, where the
// long label was cut; the tooltip keeps the whole story.
func pitchOpinion(player map[string]any) *Seg {
	seg := secondOpinion(player, false, time.Now())
	if seg != nil {
		seg.T = strings.Replace(seg.T, "futbolfantasy:", "ff:", 1)
	}
	return seg
}

// cardTags are the informative tags of a decision card: where each player's value sits in his
// season, on the side of the deal he is on, and futbolfantasy's word on his fitness where it
// differs. They are added to the card drawn, never to the card judged.
func cardTags(card Card, now time.Time) []Seg {
	var out []Seg
	add := func(seg *Seg) {
		if seg != nil {
			out = append(out, *seg)
		}
	}
	two := card.In != nil
	switch card.Kind {
	case "offer":
		add(rangeTag(card.Player, true, false, now))
	case "swap":
		add(rangeTag(card.Player, two, two, now))
		if two {
			add(rangeTag(card.In, false, true, now))
		}
	case "sign":
		add(rangeTag(card.Player, false, false, now))
	}
	add(secondOpinion(card.Player, two, now))
	if two {
		add(secondOpinion(card.In, true, now))
	}
	return out
}
