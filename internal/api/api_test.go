package api

import "testing"

func TestDailyLimit(t *testing.T) {
	cases := []struct {
		name      string
		catalogue []DailyReward
		want      int
	}{
		{"private league's own limit", []DailyReward{
			{LeagueType: "public", DailyLimit: 3},
			{LeagueType: "private", DailyLimit: 2},
			{LeagueType: "premium", DailyLimit: 5},
		}, 2},
		{"no private line", []DailyReward{{LeagueType: "public", DailyLimit: 2}}, 1},
		{"zero limit", []DailyReward{{LeagueType: "private", DailyLimit: 0}}, 1},
		{"empty catalogue", nil, 1},
	}
	for _, c := range cases {
		if got := DailyLimit(c.catalogue); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
