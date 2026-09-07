package advice

import (
	"testing"
	"time"
)

func moneyWorld() Row {
	return Row{
		"fixtures": []any{
			// Played already, and still to be played.
			Row{"kickoff": "2026-09-05T18:30:00+02:00", "local_id": "10", "visitor_id": "11"},
			Row{"kickoff": "2026-09-07T21:30:00+02:00", "local_id": "20", "visitor_id": "21"},
		},
		"players": []any{
			Row{"id": "1", "name": "Jorge Salinas", "is_mine": true, "team_id": "11",
				"value": 7_050_000.0, "xpts": 2.79, "projected_gain": -50_000.0,
				"offers": []any{Row{"id": "o1", "money": 7_300_000.0}}},
			Row{"id": "2", "name": "C. Soler", "is_mine": true, "team_id": "21",
				"value": 30_520_000.0, "xpts": 4.21, "projected_gain": -1_150_000.0,
				"offers": []any{Row{"id": "o2", "money": 30_000_000.0}}},
			Row{"id": "3", "name": "Roberto", "owner": "cristian1206", "value": 23_130_000.0,
				"market": Row{"market_id": "m3", "min_bid": 21_170_000.0}},
			// Listed, but signed three days ago: the league's hold rule binds his owner too.
			Row{"id": "4", "name": "Aramburu", "owner": "cristian1206", "value": 17_860_000.0,
				"sale_locked": true,
				"market":      Row{"market_id": "m4", "min_bid": 16_940_000.0}},
			// A clause under his value, and one over it.
			Row{"id": "5", "name": "Iker Muñoz", "owner": "La Agustineta 96",
				"value": 1_760_000.0, "clause": 1_500_000.0},
			Row{"id": "6", "name": "Nacho Perez", "owner": "Villaone",
				"value": 1_560_000.0, "clause": 6_520_000.0},
		},
	}
}

// Selling a player whose match has not kicked off hands over this matchday's points with him.
// Nothing on the page said so, and it is the difference between free money and a bad trade.
func TestMoneySaysWhichSalesCostPoints(t *testing.T) {
	money := Money(moneyWorld(), 10_900_000, time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC))
	sell := rowsOf(money["sell"])
	if len(sell) != 1 {
		t.Fatalf("solo una oferta paga por encima del valor, hay %d: %v", len(sell), sell)
	}
	if text(sell[0]["name"]) != "Jorge Salinas" {
		t.Errorf("la oferta buena es la de Salinas, dijo %q", text(sell[0]["name"]))
	}
	if number(sell[0]["over_value"]) != 250_000 {
		t.Errorf("de mas = %v, want 250.000", sell[0]["over_value"])
	}
	if truthy(sell[0]["match_pending"]) {
		t.Error("su partido ya se jugo: no regala puntos")
	}
	if got := number(money["cash_if_sold"]); got != 18_200_000 {
		t.Errorf("caja si vendes = %v, want 10.9M + 7.3M", got)
	}
}

// The offer under value is not a sale, and the player bleeding a million a week is the leak
// nobody is asking about.
func TestMoneyRanksWhatHoldingCosts(t *testing.T) {
	money := Money(moneyWorld(), 10_900_000, time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC))
	fading := rowsOf(money["fading"])
	if len(fading) != 1 || text(fading[0]["name"]) != "C. Soler" {
		t.Fatalf("el que pierde de verdad es Soler, dijo %v", fading)
	}
	if number(fading[0]["loses"]) != 1_150_000 {
		t.Errorf("pierde = %v, want 1.15M", fading[0]["loses"])
	}
	// Salinas drops 50k, which is drift and not news.
	if number(money["projected_7d"]) != -1_200_000 {
		t.Errorf("proyeccion = %v, want la suma de los dos", money["projected_7d"])
	}
}

func TestMoneyBargainsByEurosAndByRoute(t *testing.T) {
	money := Money(moneyWorld(), 10_900_000, time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC))
	found := rowsOf(money["bargains"])
	if len(found) != 2 {
		t.Fatalf("Roberto y la clausula de Iker, no mas: %v", names(found))
	}
	if text(found[0]["name"]) != "Roberto" || number(found[0]["gap"]) != 1_960_000 {
		t.Errorf("el primero es el hueco mas grande: %v", found[0])
	}
	if truthy(found[0]["affordable"]) {
		t.Error("21.17M no caben en 10.9M")
	}
	if text(found[1]["route"]) != "clausula" || number(found[1]["gap"]) != 260_000 {
		t.Errorf("la clausula de Iker esta 260k por debajo de su valor: %v", found[1])
	}
	for _, row := range found {
		if text(row["name"]) == "Aramburu" {
			t.Error("un fichaje reciente de un rival no es una oportunidad: la norma le ata")
		}
		if text(row["name"]) == "Nacho Perez" {
			t.Error("su clausula esta por encima de su valor, no es un chollo")
		}
	}
}

func names(rows []Row) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, text(row["name"]))
	}
	return out
}

// A signing arrives with a clause of its own, and what it lands at was measured rather than
// assumed: max(price, value), floored at a million. Buying below value therefore means entering
// at 1.00x, which is the most exposed clause there is, so the row has to say it.
func TestMoneyBargainSaysWhatTheClauseBecomes(t *testing.T) {
	money := Money(moneyWorld(), 100_000_000, time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC))
	for _, row := range rowsOf(money["bargains"]) {
		want := number(row["value"])
		if cost := number(row["entry_cost"]); cost > want {
			want = cost
		}
		if want < ClauseFloor {
			want = ClauseFloor
		}
		if got := number(row["clause_after"]); got != want {
			t.Errorf("%s: clausula al fichar %v, want %v", text(row["name"]), got, want)
		}
		if margin := number(row["margin_after"]); margin < 1 {
			t.Errorf("%s: entrar por debajo de su valor deja la clausula a %.2fx",
				text(row["name"]), margin)
		}
	}
}

// A signing that does not get into the eleven is decoration, however big the discount is.
func TestMoneyBargainSaysWhatThePitchGains(t *testing.T) {
	money := Money(moneyWorld(), 100_000_000, time.Date(2026, 9, 7, 11, 0, 0, 0, time.UTC))
	found := rowsOf(money["bargains"])
	if len(found) == 0 {
		t.Fatal("sin chollos no hay nada que medir")
	}
	for _, row := range found {
		if _, ok := row["xi_gain"]; !ok {
			t.Errorf("%s no dice lo que suma al once", text(row["name"]))
		}
	}
}
