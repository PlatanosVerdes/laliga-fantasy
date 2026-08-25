package server

import "testing"

// What to pay so the clause lands where the advice stops calling it a risk, remembering the
// clause rises by twice what you pay. Half his market value, which is what this used to suggest,
// was a number with nothing behind it.
func TestRaiseToSafe(t *testing.T) {
	cases := []struct {
		name          string
		value, clause float64
		want          int64
	}{
		// T. Martínez: 28.05M on a 27.71M value is 1.01x, the one genuinely worth protecting.
		// 1.6 × 27.709.943 − 28.050.000 = 16.285.908, and half of that is what you pay.
		{"below the line", 27_709_943, 28_050_000, 8_142_954},
		// Ferran Jutglà at 1.63x: already above it, so there is nothing to buy.
		{"already above", 9_313_248, 15_192_896, 0},
		// Tárrega at 2.60x: far above.
		{"far above", 9_658_067, 23_815_596, 0},
		// No value to measure against, so no number to propose.
		{"no value", 0, 10_000_000, 0},
	}
	for _, one := range cases {
		if got := raiseToSafe(one.value, one.clause); got != one.want {
			t.Errorf("%s: raiseToSafe(%.0f, %.0f) = %d, want %d",
				one.name, one.value, one.clause, got, one.want)
		}
	}
}

// The point of the number: pay it and the clause lands on the line, not short of it.
func TestRaiseToSafeLandsOnTheLine(t *testing.T) {
	value, clause := 27_709_943.0, 28_050_000.0
	pay := float64(raiseToSafe(value, clause))
	landed := (clause + 2*pay) / value
	if landed < 1.599 || landed > 1.601 {
		t.Fatalf("paying %.0f leaves the clause at %.3fx, want 1.600x", pay, landed)
	}
}
