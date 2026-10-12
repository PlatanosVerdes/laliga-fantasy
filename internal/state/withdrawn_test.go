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
		Players: []model.Player{{ID: "7", Name: "Kike Salas"}},
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

	world.Withdrawn("52857370", "7")
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
	world.Withdrawn("52857370", "7")
	universe := worldWithMyOffer()
	world.hideWithdrawn(universe, time.Now().Add(WithdrawGrace+time.Second))
	if universe.Market[0].MyBidID == nil {
		t.Error("past the grace the market should win")
	}
}
