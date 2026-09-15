package server

import (
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/writes"
)

func withOffers(ids ...string) *model.Universe {
	offers := []map[string]any{}
	for _, id := range ids {
		offers = append(offers, map[string]any{"id": id})
	}
	return &model.Universe{Players: []model.Player{{ID: "2384", Offers: offers}}}
}

// The page sends the token and nothing else, so the offer that was just refused can only be
// named by the token's own record. Read from the request body instead, it was always empty and
// the row stayed on screen until the rebuild caught up.
func TestPatchDropsTheOfferThatWasAnswered(t *testing.T) {
	for _, operation := range []string{"accept_offer", "decline_offer"} {
		universe := withOffers("aa", "bb", "cc")
		if !patch(universe, operation, writes.Args{OfferID: "bb"}) {
			t.Fatalf("%s: answering an offer changes the world", operation)
		}
		left := universe.Players[0].Offers
		if len(left) != 2 || text(left[0]["id"]) != "aa" || text(left[1]["id"]) != "cc" {
			t.Errorf("%s: only the answered one goes: %v", operation, left)
		}
	}
}

func TestPatchWithoutAnOfferLeavesTheWorldAlone(t *testing.T) {
	universe := withOffers("aa", "bb")
	if patch(universe, "decline_offer", writes.Args{}) {
		t.Error("nothing is known to have happened, so nothing is patched")
	}
	if len(universe.Players[0].Offers) != 2 {
		t.Errorf("the offers are untouched: %v", universe.Players[0].Offers)
	}
}

func TestPatchCancelsTheBidOnItsOwnListing(t *testing.T) {
	bid, bidID := 12.0, "b1"
	universe := &model.Universe{Players: []model.Player{
		{ID: "1", MarketEntry: &model.Listing{MarketID: "m1", MyBid: &bid, MyBidID: &bidID}},
		{ID: "2", MarketEntry: &model.Listing{MarketID: "m2", MyBid: &bid, MyBidID: &bidID}},
	}}
	if !patch(universe, "cancel_bid", writes.Args{MarketID: "m1"}) {
		t.Fatal("cancelling a bid changes the world")
	}
	if universe.Players[0].MarketEntry.MyBid != nil {
		t.Error("the cancelled bid is gone")
	}
	if universe.Players[1].MarketEntry.MyBid == nil {
		t.Error("the other listing keeps its bid")
	}
}
