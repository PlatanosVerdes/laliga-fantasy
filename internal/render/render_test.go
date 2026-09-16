package render

import (
	"strings"
	"testing"
)

// Your own listings read from your side. Unai López advertised at 14.13M on a 14.92M value is
// 0.95x: a bargain to whoever buys him, and the same section's note calls him under market
// value in the line above, which is what the column used to contradict.
func TestRatioBadgeReadsFromTheSideYouAreOn(t *testing.T) {
	ratio := 0.947
	cases := []struct {
		side Side
		want string
	}{
		{Paying, "chollo"},
		{Asking, "por debajo"},
		{Offered, "por debajo"},
	}
	for _, one := range cases {
		if got := RatioBadge(&ratio, one.side); !strings.Contains(got, one.want) {
			t.Errorf("0.95x from side %d: want %q, got %s", one.side, one.want, got)
		}
	}
}

// The step is at 1.00 on purpose: the note above the table warns about anything under it.
func TestAskingStepsAtValue(t *testing.T) {
	for _, one := range []struct {
		ratio float64
		want  string
	}{
		{1.30, "por encima"},
		{1.05, "con margen"},
		{1.00, "a valor"},
		{0.999, "por debajo"},
		{0.85, "lo regalas"},
	} {
		ratio := one.ratio
		if got := RatioBadge(&ratio, Asking); !strings.Contains(got, one.want) {
			t.Errorf("%.3fx asking: want %q, got %s", one.ratio, one.want, got)
		}
	}
}
