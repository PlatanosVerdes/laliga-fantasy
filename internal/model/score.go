package model

import (
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/futbolfantasy"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/matching"
)

// Constants lifted verbatim from fantasy/analysis.py. Changing one here without changing
// it there would make the harness red for a reason that is not a bug.
const (
	WeeksInSeason            = 38
	BaselineStartProbability = 0.85
	AvailabilityFloor        = 0.20
	AvailabilityCeiling      = 1.10
	NoTrendConfidence        = 0.75
	PriorConfidence          = 0.9
	DoubtfulXPts             = 0.55
	DoubtfulScorePenalty     = 0.4
	UnavailableScorePenalty  = 1.5
	ProjectionDamping        = 0.55
	ProjectionCap            = 12.0
	PriorBuckets             = 8
)

var scoreWeights = []struct {
	Key      string
	Weight   float64
	Winsor   float64
}{
	// Winsorising is not decoration: a handful of near-free players would otherwise
	// flatten the whole points-per-million distribution.
	{"points_value", 0.30, 0.03},
	{"xpts", 0.35, 0.01},
	{"projected_pct", 0.20, 0},
	{"start_probability", 0.15, 0},
}

// Trend is what futbolfantasy contributes, already matched to a LaLiga player id by the
// Python bridge. Only the fields the model reads are named.
type Trend struct {
	FFID             string   `json:"ff_id"`
	FFName           string   `json:"ff_name"`
	Value            *float64 `json:"value"`
	StartProbability *float64 `json:"start_probability"`
	NextWeek         *int     `json:"next_week"`
	NextRival        *string  `json:"next_rival"`
	NextHome         *bool    `json:"next_home"`
	TrendLabel       *string  `json:"trend_label"`
	StreakDays       *int     `json:"streak_days"`
	StreakDir        *string  `json:"streak_dir"`
	Acceleration     *float64 `json:"acceleration"`
	Pct1d            *float64 `json:"pct_1d"`
	Pct3d            *float64 `json:"pct_3d"`
	Pct7d            *float64 `json:"pct_7d"`
	Pct30d           *float64 `json:"pct_30d"`
}

// Bridge is the Python side's answer: everything derived from futbolfantasy, keyed by
// LaLiga player id, plus the absences.
type Bridge struct {
	Trends       map[string]Trend          `json:"trends"`
	Absences     map[string]map[string]any `json:"absences"`
	Unmatched    []any                     `json:"unmatched"`
	MatchedCount int                       `json:"matched_count"`
}

// TeamStrength is a 0..1 proxy per LaLiga team.
//
// Squad market value is the only signal that covers promoted clubs: their players carry
// zero last-season fantasy points because those points only exist for LaLiga, so a
// points-based proxy would rate a promoted side at exactly zero.
func TeamStrength(players []Player) map[string]float64 {
	points := map[string][]float64{}
	values := map[string][]float64{}
	for _, player := range players {
		if player.PositionID == PositionCoach {
			continue
		}
		points[player.TeamID] = append(points[player.TeamID], player.LastSeason)
		values[player.TeamID] = append(values[player.TeamID], player.Value)
	}

	topFourteen := func(items []float64) float64 {
		sorted := append([]float64(nil), items...)
		sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
		if len(sorted) > 14 {
			sorted = sorted[:14]
		}
		total := 0.0
		for _, item := range sorted {
			total += item
		}
		return total
	}

	pointsTotals := map[string]float64{}
	valueTotals := map[string]float64{}
	for teamID, items := range points {
		pointsTotals[teamID] = topFourteen(items)
	}
	for teamID, items := range values {
		valueTotals[teamID] = topFourteen(items)
	}

	pointsPct := rankPercentiles(pointsTotals)
	valuePct := rankPercentiles(valueTotals)

	strength := map[string]float64{}
	for teamID := range valueTotals {
		strength[teamID] = 0.45*percentileOr(pointsPct, teamID) + 0.55*percentileOr(valuePct, teamID)
	}
	return strength
}

func percentileOr(values map[string]float64, key string) float64 {
	if value, ok := values[key]; ok {
		return value
	}
	return 0.5
}

// rankPercentiles is 0..1 by rank, so three promoted clubs at zero do not squash the
// scale. Ties resolve by key to stay deterministic, matching Python's stable sort over
// an insertion-ordered dict.
func rankPercentiles(values map[string]float64) map[string]float64 {
	if len(values) == 0 {
		return map[string]float64{}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if values[keys[i]] != values[keys[j]] {
			return values[keys[i]] < values[keys[j]]
		}
		return keys[i] < keys[j]
	})
	span := float64(max(1, len(keys)-1))
	out := make(map[string]float64, len(keys))
	for index, key := range keys {
		out[key] = float64(index) / span
	}
	return out
}

// FixtureFactor: a harder opponent and an away trip both cost expected points.
func FixtureFactor(opponentStrength *float64, home *bool) float64 {
	factor := 1.0
	if opponentStrength != nil {
		factor *= 1.0 + 0.12*(1.0-2.0**opponentStrength)
	}
	if home != nil {
		if *home {
			factor *= 1.04
		} else {
			factor *= 0.96
		}
	}
	return factor
}

type curvePoint struct{ LogValue, PerWeek float64 }

// PricePrior is points-per-week implied by price, per position, as a log-price curve.
//
// Built only from players who do have last-season points, then used as the baseline for
// anyone who has none: promoted clubs, and the star records the game recreates each
// season with no history at all. LaLiga prices by expectation, so price is the best
// prior before kick-off.
func PricePrior(players []Player) map[int][]curvePoint {
	samples := map[int][]curvePoint{}
	for _, player := range players {
		if player.PositionID == PositionCoach {
			continue
		}
		if player.LastSeason <= 0 || player.Value <= 0 {
			continue
		}
		samples[player.PositionID] = append(samples[player.PositionID],
			curvePoint{math.Log10(player.Value), player.LastSeason / WeeksInSeason})
	}

	curves := map[int][]curvePoint{}
	for positionID, rows := range samples {
		// Python sorts tuples, so ties on log-value fall back to the second element.
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].LogValue != rows[j].LogValue {
				return rows[i].LogValue < rows[j].LogValue
			}
			return rows[i].PerWeek < rows[j].PerWeek
		})
		size := max(1, len(rows)/PriorBuckets)
		var curve []curvePoint
		for start := 0; start < len(rows); start += size {
			end := min(start+size, len(rows))
			chunk := rows[start:end]
			// A trailing stub of one or two players is noise, but it cannot be dropped
			// when it is all we have.
			if len(chunk) < 3 && len(curve) > 0 {
				continue
			}
			logs := make([]float64, 0, len(chunk))
			weeks := make([]float64, 0, len(chunk))
			for _, point := range chunk {
				logs = append(logs, point.LogValue)
				weeks = append(weeks, point.PerWeek)
			}
			curve = append(curve, curvePoint{mean(logs), median(weeks)})
		}
		curves[positionID] = curve
	}
	return curves
}

// PriorFor interpolates the curve, extrapolating past its ends with the edge slope.
func PriorFor(curves map[int][]curvePoint, positionID int, value float64) float64 {
	curve := curves[positionID]
	if len(curve) == 0 || value <= 0 {
		return 0
	}
	if len(curve) == 1 {
		return curve[0].PerWeek
	}

	x := math.Log10(value)
	var left, right curvePoint
	switch {
	case x <= curve[0].LogValue:
		left, right = curve[0], curve[1]
	case x >= curve[len(curve)-1].LogValue:
		left, right = curve[len(curve)-2], curve[len(curve)-1]
	default:
		left, right = curve[0], curve[1]
		for index := 0; index < len(curve)-1; index++ {
			if curve[index].LogValue <= x && x <= curve[index+1].LogValue {
				left, right = curve[index], curve[index+1]
				break
			}
		}
	}
	span := right.LogValue - left.LogValue
	if span == 0 {
		span = 1
	}
	slope := (right.PerWeek - left.PerWeek) / span
	return math.Max(0, left.PerWeek+slope*(x-left.LogValue))
}

// ProjectedPct is the expected value change over the next 7 days, in percent.
//
// Recent daily rates are extrapolated but damped: these streaks mean-revert, so a raw
// 7x of yesterday's move would peg almost everyone at the cap.
func ProjectedPct(trend *Trend) float64 {
	if trend == nil {
		return 0
	}
	windows := []struct {
		Pct    *float64
		Days   float64
		Weight float64
	}{
		{trend.Pct1d, 1, 0.5}, {trend.Pct3d, 3, 0.3}, {trend.Pct7d, 7, 0.2},
	}
	daily, weights := 0.0, 0.0
	for _, window := range windows {
		if window.Pct == nil {
			continue
		}
		daily += window.Weight * (*window.Pct / window.Days)
		weights += window.Weight
	}
	if weights == 0 {
		return 0
	}
	projected := (daily / weights) * 7 * ProjectionDamping
	return math.Max(-ProjectionCap, math.Min(ProjectionCap, projected))
}

// zscores are standard scores. `winsorize` clips the top and bottom quantile first.
func zscores(values []float64, winsorize float64) []float64 {
	clean := make([]float64, 0, len(values))
	for _, value := range values {
		if !math.IsInf(value, 0) && !math.IsNaN(value) {
			clean = append(clean, value)
		}
	}
	if len(clean) < 2 {
		return make([]float64, len(values))
	}
	sort.Float64s(clean)
	low, high := clean[0], clean[len(clean)-1]
	if winsorize > 0 {
		index := max(0, min(len(clean)-1, int(float64(len(clean))*winsorize)))
		low, high = clean[index], clean[len(clean)-1-index]
	}

	clipped := make([]float64, len(values))
	for position, value := range values {
		clipped[position] = math.Min(math.Max(value, low), high)
	}
	average := mean(clipped)
	deviation := populationStdev(clipped, average)
	if deviation == 0 {
		deviation = 1
	}
	out := make([]float64, len(values))
	for position, value := range clipped {
		out[position] = (value - average) / deviation
	}
	return out
}

// ApplyScores fills score, rank and position_rank.
func ApplyScores(players []Player) {
	if len(players) == 0 {
		return
	}
	columns := map[string][]float64{}
	for _, weight := range scoreWeights {
		values := make([]float64, len(players))
		for index, player := range players {
			values[index] = scoreInput(player, weight.Key)
		}
		columns[weight.Key] = zscores(values, weight.Winsor)
	}

	for index := range players {
		score := 0.0
		for _, weight := range scoreWeights {
			score += weight.Weight * columns[weight.Key][index]
		}
		if !players[index].Available {
			score -= UnavailableScorePenalty
		} else if players[index].Status == "doubtful" {
			score -= DoubtfulScorePenalty
		}
		players[index].Score = score
	}

	order := make([]int, len(players))
	for index := range order {
		order[index] = index
	}
	// Python's sorted() is stable, so equal scores keep their original order.
	sort.SliceStable(order, func(i, j int) bool {
		return players[order[i]].Score > players[order[j]].Score
	})
	for rank, index := range order {
		players[index].Rank = rank + 1
	}

	byPosition := map[int][]int{}
	for _, index := range order {
		positionID := players[index].PositionID
		byPosition[positionID] = append(byPosition[positionID], index)
	}
	for _, indexes := range byPosition {
		for rank, index := range indexes {
			players[index].PositionRank = rank + 1
		}
	}
}

func scoreInput(player Player, key string) float64 {
	switch key {
	case "points_value":
		return player.PointsValue
	case "xpts":
		return player.XPts
	case "projected_pct":
		return player.ProjectedPct
	case "start_probability":
		if player.StartProbability == nil {
			return 0
		}
		return *player.StartProbability
	}
	return 0
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
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

func populationStdev(values []float64, average float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, value := range values {
		total += (value - average) * (value - average)
	}
	return math.Sqrt(total / float64(len(values)))
}

// DeepEnrich replaces the price-derived baseline with real last-season output.
//
// The API ships no history for promoted clubs or for the star records it recreated this season,
// so for a shortlist we read futbolfantasy's player page and recompute the baseline from the
// matchdays he actually played. One request per player and the pages are heavy, hence opt-in
// and shortlist-only.
func DeepEnrich(players []*Player, limit int, ttl time.Duration) int {
	fixed := 0
	for index, player := range players {
		if index >= limit {
			break
		}
		if !player.PriorBased || player.FFID == nil {
			continue
		}
		name := player.Name
		if player.FFName != nil && *player.FFName != "" {
			name = *player.FFName
		}
		page, err := futbolfantasy.PlayerPage(matching.SlugifyFF(name), ttl)
		if err != nil {
			slog.Debug("deep enrich skipped", "player", player.Name, "reason", err.Error())
			continue
		}
		games := number(page["games_played"])
		if games == 0 {
			continue
		}
		total := number(page["total_points"])
		baseWeek := total / WeeksInSeason
		scale := 1.0
		if player.BaseWeek != 0 {
			scale = baseWeek / player.BaseWeek
		}
		player.BaseWeek = baseWeek
		player.LastSeason = total
		player.PriorBased = false
		player.XPts *= scale
		if player.Value != 0 {
			player.PointsValue = player.XPts / (player.Value / 1e6)
		} else {
			player.PointsValue = 0
		}
		fixed++
	}
	slog.Info("deep enrich done", "fixed", fixed)
	return fixed
}
