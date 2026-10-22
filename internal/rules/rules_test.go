package rules

import (
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

func TestALeaguesRulesStayInItsProfile(t *testing.T) {
	root := t.TempDir()
	a, b := Open(config.ProfileIn(root, "uno", "L")), Open(config.ProfileIn(root, "dos", "L"))
	if err := a.Save(map[string]League{"L": {HoldDays: 7}}); err != nil {
		t.Fatal(err)
	}
	if a.For("L").HoldDays != 7 || b.For("L").HoldDays != 0 {
		t.Errorf("hold days: a %d, b %d", a.For("L").HoldDays, b.For("L").HoldDays)
	}
}
