package eleven

import "testing"

func fit(id string, position int, xpts float64) Player {
	return Player{ID: id, Name: id, Position: position, XPts: xpts, Available: true}
}

func out(id string, position int, xpts float64) Player {
	player := fit(id, position, xpts)
	player.Available = false
	return player
}

// The squad this came from: eleven players, three midfielders, and a 4-4-2 saved. The 4-4-2
// seats ten of them, so the pick has to be the formation that seats all eleven.
func TestBestPicksTheShapeThatSeatsEverybody(t *testing.T) {
	squad := []Player{
		fit("gk", Keeper, 3),
		fit("d1", Defender, 4), fit("d2", Defender, 3.9), fit("d3", Defender, 3.8),
		fit("d4", Defender, 3.7), fit("d5", Defender, 3.6),
		fit("m1", Midfielder, 2.5), fit("m2", Midfielder, 2.4), fit("m3", Midfielder, 2.3),
		fit("s1", Striker, 3.1), fit("s2", Striker, 3),
	}
	choice, complete := Best(squad)
	if !complete {
		t.Fatalf("eleven available players should fill a shape, got %d", choice.Starters())
	}
	if choice.Shape.Name != "5-3-2" {
		t.Errorf("shape = %q, want 5-3-2", choice.Shape.Name)
	}
	if len(choice.IDs()) != 11 {
		t.Errorf("%d starters, want 11", len(choice.IDs()))
	}
}

// An injured starter is an empty slot with a shirt on, so the eleven is picked around him and
// the substitute takes the place even though he scores a ninth of what the injured one would.
func TestBestLeavesOutWhoeverCannotPlay(t *testing.T) {
	squad := []Player{
		fit("gk", Keeper, 3),
		fit("d1", Defender, 4), fit("d2", Defender, 3.9), fit("d3", Defender, 3.8),
		out("d4-lesionado", Defender, 9), fit("d5", Defender, 1), fit("d6", Defender, 0.9),
		fit("m1", Midfielder, 2.5), fit("m2", Midfielder, 2.4), fit("m3", Midfielder, 2.3),
		fit("s1", Striker, 3.1), fit("s2", Striker, 3),
	}
	choice, complete := Best(squad)
	if !complete {
		t.Fatalf("the eleventh is available, got %d starters", choice.Starters())
	}
	for _, id := range choice.IDs() {
		if id == "d4-lesionado" {
			t.Fatal("an injured player was put on the pitch over an available one")
		}
	}
}

// Nobody to replace him: the slot stays empty rather than being handed to somebody who cannot
// play, and the caller is told the eleven is not complete.
func TestBestAdmitsWhenTheElevenCannotBeFilled(t *testing.T) {
	squad := []Player{
		fit("gk", Keeper, 3),
		fit("d1", Defender, 4), fit("d2", Defender, 3.9), fit("d3", Defender, 3.8),
		out("d4", Defender, 3.7), out("d5", Defender, 3.6),
		fit("m1", Midfielder, 2.5), fit("m2", Midfielder, 2.4), fit("m3", Midfielder, 2.3),
		fit("s1", Striker, 3.1), fit("s2", Striker, 3),
	}
	choice, complete := Best(squad)
	if complete {
		t.Fatal("nine available players cannot fill eleven slots")
	}
	if got := choice.Starters(); got != 9 {
		t.Errorf("%d starters, want the 9 who can play", got)
	}
}

// What decides whether to touch a saved lineup at all: how many of its starters can score.
func TestPlayableCountsOnlyWhoCanScore(t *testing.T) {
	squad := []Player{fit("a", Defender, 1), out("b", Defender, 1), fit("c", Striker, 1)}
	if got := Playable([]string{"a", "b", "c", "", "unknown"}, squad); got != 2 {
		t.Fatalf("Playable = %d, want 2", got)
	}
}

func TestNumbersLeaveTheKeeperImplied(t *testing.T) {
	for _, shape := range Shapes {
		if shape.Name != "4-3-3" {
			continue
		}
		numbers := shape.Numbers()
		if len(numbers) != 3 || numbers[0] != 4 || numbers[1] != 3 || numbers[2] != 3 {
			t.Fatalf("Numbers = %v, want [4 3 3]", numbers)
		}
	}
}
