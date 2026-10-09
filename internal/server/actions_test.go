package server

import "testing"

// One filled button: the best offer worth taking, and nothing else even when a clause is
// recommended too.
func TestMarkPrimaryPrefersTheBestOfferTaken(t *testing.T) {
	actions := []map[string]any{
		{"op": "accept_offer", "offer_id": "1", "amount": 9.0, "why": "x", "take": false},
		{"op": "decline_offer", "offer_id": "1"},
		{"op": "accept_offer", "offer_id": "2", "amount": 5.0, "why": "y", "take": true},
		{"op": "decline_offer", "offer_id": "2"},
		{"op": "raise_clause", "recommended": true},
	}
	groupActions(actions, true)
	markPrimary(actions, false)
	primaries := 0
	for _, action := range actions {
		if truthy(action["primary"]) {
			primaries++
		}
	}
	if primaries != 1 || !truthy(actions[2]["primary"]) {
		t.Errorf("one primary, the offer taken: %v", actions)
	}
	if actions[0]["group"] != "oferta" || actions[4]["group"] != "clausula" {
		t.Errorf("groups: %v", actions)
	}
}

// No offer worth taking: the refusal of the best one is the recommendation, with its reason.
func TestMarkPrimaryRecommendsDecliningTheBestOffer(t *testing.T) {
	actions := []map[string]any{
		{"op": "accept_offer", "offer_id": "1", "amount": 9.0, "why": "no compensa"},
		{"op": "decline_offer", "offer_id": "1"},
	}
	markPrimary(actions, false)
	if !truthy(actions[1]["primary"]) || actions[1]["why"] != "no compensa" {
		t.Errorf("decline filled with the reason: %v", actions)
	}
}

// A rival's player: the bid when buying him is recommended, else the clause flagged by the
// advice, and never a blocked button.
func TestMarkPrimaryForRivals(t *testing.T) {
	actions := []map[string]any{
		{"op": "bid"}, {"op": "pay_clause", "recommended": true, "blocked": true},
		{"op": "raid", "recommended": true},
	}
	markPrimary(actions, false)
	if truthy(actions[0]["primary"]) || truthy(actions[1]["primary"]) ||
		!truthy(actions[2]["primary"]) {
		t.Errorf("the raid: %v", actions)
	}
	groupActions(actions, false)
	if actions[0]["group"] != "fichar" {
		t.Errorf("group: %v", actions)
	}
}
