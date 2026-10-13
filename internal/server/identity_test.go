package server

import "testing"

// Started without a session the ids are empty; once one is adopted the server has to see them.
func TestIdentityIsReadWhenAsked(t *testing.T) {
	league, team := "", ""
	server := New(nil, Options{Identity: func() (string, string) { return league, team }})
	if server.leagueID() != "" || server.myTeamID() != "" {
		t.Fatalf("ids before any session: %q %q", server.leagueID(), server.myTeamID())
	}
	league, team = "L1", "T1"
	if server.leagueID() != "L1" || server.myTeamID() != "T1" {
		t.Fatalf("adopted ids not seen: %q %q", server.leagueID(), server.myTeamID())
	}
}
