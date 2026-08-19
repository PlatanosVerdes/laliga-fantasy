package main

import (
	"testing"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/policies"
)

// The whole point of waking early is firing on the other side of the instant, so the wait has to
// exist when the clause is seconds away and not otherwise.
func TestUntilNextRaid(t *testing.T) {
	soon := time.Now().Add(2 * time.Second).Format(time.RFC3339Nano)
	later := time.Now().Add(90 * time.Second).Format(time.RFC3339Nano)
	past := time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	armed := map[string]policies.Policy{
		"a": {Raid: true}, "b": {Raid: true}, "c": {Raid: true}, "d": {},
	}

	cases := []struct {
		name string
		rows []map[string]any
		want bool
	}{
		{"a clause seconds away is worth waiting for",
			[]map[string]any{{"id": "a", "clause_locked_until": soon}}, true},
		{"one that opens in a minute and a half is not",
			[]map[string]any{{"id": "b", "clause_locked_until": later}}, false},
		{"one already open needs no wait",
			[]map[string]any{{"id": "c", "clause_locked_until": past}}, false},
		{"a player with no raid armed is never waited for",
			[]map[string]any{{"id": "d", "clause_locked_until": soon}}, false},
	}
	for _, test := range cases {
		got := untilNextRaid(test.rows, armed)
		if (got > 0) != test.want {
			t.Errorf("%s: espera %v, esperaba que %v", test.name, got, test.want)
		}
		if got > RaidWait {
			t.Errorf("%s: espera %v, por encima del tope %v", test.name, got, RaidWait)
		}
	}

	// The nearest one wins, and the grace period lands after the instant, not before.
	rows := []map[string]any{
		{"id": "a", "clause_locked_until": soon},
		{"id": "b", "clause_locked_until": time.Now().Add(5 * time.Second).Format(time.RFC3339Nano)},
	}
	wait := untilNextRaid(rows, armed)
	if wait < 2*time.Second || wait > 2*time.Second+RaidGrace+50*time.Millisecond {
		t.Errorf("espera %v, esperaba unos 2s mas la gracia", wait)
	}
}
