// Package eleven is what fielding a legal starting eleven asks of a squad.
//
// The engine used to reason with a floor per position — a keeper, three defenders, three
// midfielders, a striker — which is eight players. So an automatic sale could leave ten and
// still be called legal, because no formation was ever consulted, and the plan could report
// the points of "the best eleven" for a squad that cannot line up eleven at all. Formations
// are the real rule, so they are the rule here.
package eleven

import "fmt"

// Position ids as the API numbers them.
const (
	Keeper     = 1
	Defender   = 2
	Midfielder = 3
	Striker    = 4
)

// Shape is one formation: how many starters it asks for per position.
type Shape struct {
	Name string
	Need map[int]int
}

// Shapes are the formations LaLiga Fantasy offers without premium, in the order the app lists
// them. Read from /v4/teams/lineup/formations?option=free.
//
// The five premium shapes (5-2-3, 4-6-0, 4-2-4, 3-6-1, 3-3-4) are deliberately left out.
// Without premium nobody can line one up, and assuming they exist would be wrong in the
// expensive direction: it would clear an automatic sale that leaves a squad unable to field
// anybody. Leaving them out only ever refuses a sale or asks for a signing that is legal
// anyway.
var Shapes = []Shape{
	shape(5, 4, 1), shape(5, 3, 2), shape(4, 5, 1), shape(4, 4, 2),
	shape(4, 3, 3), shape(3, 5, 2), shape(3, 4, 3),
}

func shape(defenders, midfielders, strikers int) Shape {
	return Shape{
		Name: fmt.Sprintf("%d-%d-%d", defenders, midfielders, strikers),
		Need: map[int]int{Keeper: 1, Defender: defenders,
			Midfielder: midfielders, Striker: strikers},
	}
}

// Numbers is the shape as the lineup call wants it: defenders, midfielders, strikers, with the
// keeper left implied, which is how the API writes tacticalFormation too.
func (s Shape) Numbers() []int {
	return []int{s.Need[Defender], s.Need[Midfielder], s.Need[Striker]}
}

// Fillable reports whether a squad with these counts can put this shape on the pitch.
func (s Shape) Fillable(counts map[int]int) bool {
	for position, need := range s.Need {
		if counts[position] < need {
			return false
		}
	}
	return true
}

// Fits are the shapes this squad can field, in the app's order.
func Fits(counts map[int]int) []Shape {
	var out []Shape
	for _, shape := range Shapes {
		if shape.Fillable(counts) {
			out = append(out, shape)
		}
	}
	return out
}

// Any reports whether there is a legal eleven at all.
func Any(counts map[int]int) bool {
	for _, shape := range Shapes {
		if shape.Fillable(counts) {
			return true
		}
	}
	return false
}

// Missing is, per position, how many players of that position alone it would take to reach a
// legal eleven, and nothing at all when there already is one.
//
// Per position rather than one cheapest shopping list, because usually several answers are
// right: with 1-4-3-2 a defender completes a 5-3-2, a midfielder a 4-4-2 and a striker a
// 4-3-3, so all three are worth one, and a keeper is worth nothing because no formation asks
// for a second one. Whoever reads this can then pick by who is actually for sale.
func Missing(counts map[int]int) map[int]int {
	short := map[int]int{}
	if Any(counts) {
		return short
	}
	trial := make(map[int]int, len(counts))
	for position, count := range counts {
		trial[position] = count
	}
	for _, position := range []int{Keeper, Defender, Midfielder, Striker} {
		start := trial[position]
		for added := 1; added <= 11; added++ {
			trial[position] = start + added
			if Any(trial) {
				short[position] = added
				break
			}
		}
		trial[position] = start
	}
	return short
}

// Room is how many of that position can leave with a legal eleven still standing. Zero when
// there is no legal eleven to begin with: a squad that cannot line up is not one to sell from.
func Room(counts map[int]int, positionID int) int {
	if !Any(counts) {
		return 0
	}
	trial := make(map[int]int, len(counts))
	for position, count := range counts {
		trial[position] = count
	}
	room := 0
	for trial[positionID] > 0 {
		trial[positionID]--
		if !Any(trial) {
			break
		}
		room++
	}
	return room
}
