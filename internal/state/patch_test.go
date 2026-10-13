package state

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
)

func patchOfferAndBid(universe *model.Universe) bool {
	player := &universe.Players[0]
	kept := player.Offers[:0]
	for _, offer := range player.Offers {
		if offer["id"] != "bb" {
			kept = append(kept, offer)
		}
	}
	player.Offers = kept
	player.Starred = true
	bid := 1.0
	player.MarketEntry.MyBid = &bid
	return true
}

func patchableWorld() *State {
	universe := worldWithMyOffer()
	universe.Players[0].Offers = []map[string]any{{"id": "aa"}, {"id": "bb"}, {"id": "cc"}}
	return &State{universe: universe, subs: map[chan []byte]bool{}}
}

// Readers marshal the world they were handed without the lock; a patch must not change it.
func TestAPatchLeavesThePublishedWorldAlone(t *testing.T) {
	world := patchableWorld()
	before := world.Universe()
	bidBefore := *before.Market[0].MyBid

	if !world.Patch("test", patchOfferAndBid) {
		t.Fatal("the patch changed something")
	}

	old := before.Players[0]
	if len(old.Offers) != 3 || old.Offers[1]["id"] != "bb" || old.Starred ||
		*before.Market[0].MyBid != bidBefore {
		t.Fatalf("the published world was mutated: %+v", old)
	}
	after := world.Universe()
	if after == before {
		t.Fatal("the patched world was not swapped in")
	}
	patched := after.Players[0]
	if len(patched.Offers) != 2 || !patched.Starred || *after.Market[0].MyBid != 1 {
		t.Fatalf("the patch did not land: %+v", patched)
	}
	if patched.MarketEntry != &after.Market[0] {
		t.Fatal("the listing does not point into the patched market")
	}
}

func TestAPatchThatChangesNothingKeepsTheWorld(t *testing.T) {
	world := patchableWorld()
	before := world.Universe()
	if world.Patch("noop", func(*model.Universe) bool { return false }) {
		t.Fatal("nothing changed")
	}
	if world.Universe() != before {
		t.Fatal("a patch that matched nothing replaced the world")
	}
}

// Run with -race: marshalling a handed-out world while patches land.
func TestPatchingWhileReadersMarshal(t *testing.T) {
	world := patchableWorld()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			if _, err := json.Marshal(world.Payload()); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for range 200 {
		world.Patch("test", patchOfferAndBid)
	}
	wg.Wait()
}

// The hooks are wired while the engine and the server already run; under -race this would flag
// an unguarded write.
func TestHooksCanBeWiredWhileRunning(t *testing.T) {
	world := patchableWorld()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		world.SetWarm(func() {})
		world.OnFirstWatcher(func(string) {})
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			channel := world.Subscribe()
			world.Patch("test", patchOfferAndBid)
			world.Unsubscribe(channel)
		}
	}()
	wg.Wait()
}
