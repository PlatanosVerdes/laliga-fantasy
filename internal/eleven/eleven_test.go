package eleven

import "testing"

// The squad that started this: eleven players, 1-5-3-2, which fields a 5-3-2 and nothing else
// with four midfielders. Legal, but with nobody to spare.
var mine = map[int]int{Keeper: 1, Defender: 5, Midfielder: 3, Striker: 2}

func TestFitsTheSquadThatHasExactlyEleven(t *testing.T) {
	fits := Fits(mine)
	if len(fits) != 1 || fits[0].Name != "5-3-2" {
		t.Fatalf("fits = %v, want only 5-3-2", names(fits))
	}
	if len(Missing(mine)) != 0 {
		t.Fatalf("Missing = %v, want nothing: 5-3-2 fields all eleven", Missing(mine))
	}
}

// The bug this package exists for: the old floor per position said two defenders could go,
// because five is two above the minimum of three. Selling one leaves ten players, and ten
// players field nothing.
func TestNobodyIsSpareAtElevenPlayers(t *testing.T) {
	for _, position := range []int{Keeper, Defender, Midfielder, Striker} {
		if room := Room(mine, position); room != 0 {
			t.Errorf("Room(%d) = %d, want 0: selling anybody leaves ten", position, room)
		}
	}
}

func TestRoomAppearsWithASubstitute(t *testing.T) {
	twelve := map[int]int{Keeper: 1, Defender: 6, Midfielder: 3, Striker: 2}
	if room := Room(twelve, Defender); room != 1 {
		t.Errorf("Room(defender) = %d, want 1", room)
	}
	// The keeper is still nobody's spare, however deep the rest of the squad is.
	if room := Room(twelve, Keeper); room != 0 {
		t.Errorf("Room(keeper) = %d, want 0", room)
	}
}

// Ten players, and three different signings each fix it: a defender completes the 5-3-2, a
// midfielder the 4-4-2, a striker the 4-3-3. A second keeper fixes nothing, because no
// formation asks for one.
func TestMissingNamesEveryPositionThatWouldFixIt(t *testing.T) {
	ten := map[int]int{Keeper: 1, Defender: 4, Midfielder: 3, Striker: 2}
	if Any(ten) {
		t.Fatal("ten players should field no legal eleven")
	}
	missing := Missing(ten)
	for _, position := range []int{Defender, Midfielder, Striker} {
		if missing[position] != 1 {
			t.Errorf("Missing[%d] = %d, want 1", position, missing[position])
		}
	}
	if missing[Keeper] != 0 {
		t.Errorf("Missing[keeper] = %d, want 0: nobody fields two", missing[Keeper])
	}
}

// Two short: one signing is not enough, and the count says so instead of stopping at one.
func TestMissingCountsMoreThanOne(t *testing.T) {
	nine := map[int]int{Keeper: 1, Defender: 3, Midfielder: 3, Striker: 2}
	if short := Missing(nine)[Defender]; short != 2 {
		t.Fatalf("Missing[defender] = %d, want 2", short)
	}
}

// Eleven players and still nothing to field: every free shape wants a striker.
func TestElevenPlayersCanStillBeIllegal(t *testing.T) {
	strikerless := map[int]int{Keeper: 1, Defender: 5, Midfielder: 5, Striker: 0}
	if Any(strikerless) {
		t.Fatal("no free shape fields a squad with no strikers")
	}
	if Missing(strikerless)[Striker] != 1 {
		t.Fatalf("Missing = %v, want one striker", Missing(strikerless))
	}
	if room := Room(strikerless, Defender); room != 0 {
		t.Errorf("Room = %d, want 0: there is no eleven to protect yet", room)
	}
}

func names(shapes []Shape) []string {
	var out []string
	for _, shape := range shapes {
		out = append(out, shape.Name)
	}
	return out
}
