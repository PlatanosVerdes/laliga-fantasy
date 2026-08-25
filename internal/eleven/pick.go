package eleven

import "sort"

// Player is the little a lineup decision needs. ID is the playerTeamId, which is what the
// lineup call travels with: the API lists by squad slot, not by player.
type Player struct {
	ID        string
	Name      string
	Position  int
	XPts      float64
	Available bool
}

// Choice is a lineup: the shape and who fills each line.
type Choice struct {
	Shape   Shape
	Keeper  string
	Defence []string
	Middle  []string
	Attack  []string
}

// Starters is how many of the eleven slots this choice fills.
func (c Choice) Starters() int {
	count := len(c.Defence) + len(c.Middle) + len(c.Attack)
	if c.Keeper != "" {
		count++
	}
	return count
}

// IDs are the starters, in the order the lineup call wants them.
func (c Choice) IDs() []string {
	out := make([]string, 0, 11)
	if c.Keeper != "" {
		out = append(out, c.Keeper)
	}
	out = append(out, c.Defence...)
	out = append(out, c.Middle...)
	return append(out, c.Attack...)
}

func (c *Choice) add(positionID int, id string) {
	switch positionID {
	case Keeper:
		c.Keeper = id
	case Defender:
		c.Defence = append(c.Defence, id)
	case Midfielder:
		c.Middle = append(c.Middle, id)
	case Striker:
		c.Attack = append(c.Attack, id)
	}
}

// Best is the highest-scoring legal eleven of players who can actually play, and whether it
// fills all eleven slots.
//
// Unavailable players are left out rather than ranked last: an injured or a sanctioned name on
// the pitch is an empty slot with a shirt on, and the slot might be fillable by somebody who
// plays. Doubtful is not one of those -- he is discounted in his points, not written off, the
// same as everywhere else in the engine.
//
// The shape that seats the most players wins and points break the tie, so a formation that
// fields eleven always beats a richer one that fields ten.
func Best(squad []Player) (Choice, bool) {
	byPosition := map[int][]Player{}
	for _, player := range squad {
		if !player.Available {
			continue
		}
		byPosition[player.Position] = append(byPosition[player.Position], player)
	}
	for _, players := range byPosition {
		sort.SliceStable(players, func(one, two int) bool {
			return players[one].XPts > players[two].XPts
		})
	}

	var best Choice
	bestStarters, bestPoints := -1, 0.0
	for _, shape := range Shapes {
		choice := Choice{Shape: shape}
		points := 0.0
		for _, positionID := range []int{Keeper, Defender, Midfielder, Striker} {
			players := byPosition[positionID]
			for index := 0; index < shape.Need[positionID] && index < len(players); index++ {
				choice.add(positionID, players[index].ID)
				points += players[index].XPts
			}
		}
		starters := choice.Starters()
		if starters > bestStarters || (starters == bestStarters && points > bestPoints) {
			best, bestStarters, bestPoints = choice, starters, points
		}
	}
	return best, bestStarters == 11
}

// Playable is how many of these starters can score: the saved lineup's own count, measured the
// same way Best measures its own, so the two are comparable.
func Playable(starters []string, squad []Player) int {
	available := map[string]bool{}
	for _, player := range squad {
		available[player.ID] = player.Available
	}
	count := 0
	for _, id := range starters {
		if id != "" && available[id] {
			count++
		}
	}
	return count
}
