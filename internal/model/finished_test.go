package model

import (
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/api"
)

// A past matchday earns the week-long cache only when the copy says every match is over.
func TestAllFinished(t *testing.T) {
	cases := []struct {
		states []int
		want   bool
	}{
		{[]int{7, 7, 7}, true},
		{[]int{7, 1, 7}, false},
		{nil, false},
	}
	for _, c := range cases {
		matches := []api.Match{}
		for _, state := range c.states {
			matches = append(matches, api.Match{MatchState: state})
		}
		if got := AllFinished(matches); got != c.want {
			t.Fatalf("states %v: got %v", c.states, got)
		}
	}
}
