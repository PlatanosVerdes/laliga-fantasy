package state

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/model"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/outcomes"
)

// The market answers with my offer for a minute or two after the cancel went through.
func worldWithMyOffer() *model.Universe {
	offerID, amount, seller := "164412914", 14110001.0, "cristian1206"
	universe := &model.Universe{
		Market: []model.Listing{{MarketID: "52857370", PlayerID: "7", Kind: "venta",
			Seller: &seller, MyBidID: &offerID, MyBid: &amount}},
		Players:         []model.Player{{ID: "7", Name: "Kike Salas"}},
		OwnershipLoaded: true,
	}
	universe.Players[0].MarketEntry = &universe.Market[0]
	return universe
}

func TestAWithdrawnOfferStaysGoneAndEndsAsWithdrawn(t *testing.T) {
	config.StateDir = t.TempDir()
	config.PolicyFile = filepath.Join(t.TempDir(), "policies.json")
	world := New(func() (*model.Universe, error) { return worldWithMyOffer(), nil })
	if err := world.Refresh("start"); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	world.Withdrawn("52857370", "164412914", "7")
	if err := world.Refresh("cancel_offer"); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	if listing := world.Universe().Players[0].MarketEntry; listing.MyBidID != nil {
		t.Error("the rebuild brought back an offer that was withdrawn")
	}
	endings := outcomes.Load()
	if len(endings) != 1 || endings[0].Outcome != "retirada" {
		t.Fatalf("endings = %+v, want one retirada", endings)
	}
}

func TestAWithdrawalThatNeverLandedShowsUpAgain(t *testing.T) {
	world := &State{}
	world.Withdrawn("52857370", "164412914", "7")
	universe := worldWithMyOffer()
	world.hideWithdrawn(universe, time.Now().Add(WithdrawGrace+time.Second))
	if universe.Market[0].MyBidID == nil {
		t.Error("past the grace the market should win")
	}
}

// A new offer on the same listing right after withdrawing the old one is real: hiding it left the
// page offering "Ofrecer" and the API refusing with "Team has pending offer in this player".
func TestANewOfferAfterAWithdrawalShows(t *testing.T) {
	world := &State{}
	world.Withdrawn("52857370", "164412914", "7")
	universe := worldWithMyOffer()
	fresh := "166182455"
	universe.Market[0].MyBidID = &fresh
	world.hideWithdrawn(universe, time.Now())
	if universe.Market[0].MyBidID == nil || *universe.Market[0].MyBidID != fresh {
		t.Error("the new offer was hidden as if it were the withdrawn one")
	}
}

// The API keeps listing an offer for my player for a while after I declined it, and a rebuild in
// that window put the row back with its Rechazar button.
func TestADeclinedOfferStaysGone(t *testing.T) {
	world := &State{}
	world.Answered("37547860")
	universe := &model.Universe{Players: []model.Player{{ID: "7", IsMine: true,
		Offers: []map[string]any{{"id": 37547860.0}, {"id": "37547861"}}}}}
	world.hideWithdrawn(universe, time.Now())
	if offers := universe.Players[0].Offers; len(offers) != 1 || offers[0]["id"] != "37547861" {
		t.Errorf("offers = %v, want only the one still open", offers)
	}

	universe.Players[0].Offers = []map[string]any{{"id": 37547860.0}}
	world.hideWithdrawn(universe, time.Now().Add(WithdrawGrace+time.Second))
	if len(universe.Players[0].Offers) != 1 {
		t.Error("past the grace the market should win")
	}
}
