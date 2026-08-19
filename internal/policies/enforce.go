package policies

// The half of the standing instructions that actually acts.
//
// Everything else in this package decides; this executes, and it is the only code in the project
// that spends money without a person confirming it. That is the whole point of arming an
// instruction — a clause opens at 03:00 and an offer expires while you sleep — but it means the
// limits have to be somebody else's decision, taken in advance and in writing:
//
//   - it only ever performs the three actions the plan marks as ready: list a player you told it
//     to keep listed, accept an offer above the floor you set, pay a clause under the cap you set;
//   - `avisar`, `esperando`, `bloqueada`, `cancelada` and `sin_saldo` are never acted on, they are
//     the plan saying no;
//   - a cap per cycle, so a bug costs a bounded amount rather than a squad;
//   - every action is logged before and after, with its reason, because an operation nobody
//     watched has to be explainable afterwards.

import (
	"fmt"
	"log/slog"
)

// PerCycle is how many automatic operations one pass may perform. Three covers a busy morning —
// a sale, a listing and a clause — and bounds the damage if the plan ever computes nonsense.
const PerCycle = 3

// Doable are the actions Enforce is allowed to perform, and their operation names. Anything not
// in here is a notice.
var Doable = map[string]string{
	"poner_en_venta":  "sell_to_market",
	"aceptar_oferta":  "accept_offer",
	"pagar_clausula":  "pay_clause",
}

// Runner performs one operation and reports what happened. Injected so the enforcement can be
// exercised without a session and so a test cannot spend anything.
type Runner func(operation string, action Row) error

// Enforce walks the plan and performs what is ready, in order, up to the cap. It returns what it
// did, for the log and for the page.
func Enforce(plan []Row, raids []Row, run Runner) []Row {
	done := []Row{}
	for _, action := range append(append([]Row{}, plan...), raids...) {
		if len(done) >= PerCycle {
			slog.Warn("automatic actions capped for this cycle", "cap", PerCycle,
				"pending", "se hara en el siguiente ciclo")
			break
		}
		operation, doable := Doable[text(action["action"])]
		if !doable {
			continue
		}
		slog.Info("automatic action starting", "operation", operation,
			"player", text(action["name"]), "amount", int64(number(action["amount"])),
			"why", text(action["why"]))

		err := run(operation, action)
		result := merge(action, Row{"operation": operation, "ok": err == nil})
		if err != nil {
			result["error"] = err.Error()
			slog.Error("automatic action failed", "operation", operation,
				"player", text(action["name"]), "reason", err.Error())
		} else {
			slog.Info("automatic action done", "operation", operation,
				"player", text(action["name"]), "amount", int64(number(action["amount"])),
				"why", text(action["why"]))
		}
		done = append(done, result)
	}
	if len(done) > 0 {
		slog.Info("automatic actions finished", "count", len(done))
	}
	return done
}

// Describe is one line per action for a person reading afterwards.
func Describe(action Row) string {
	state := "hecho"
	if !truthy(action["ok"]) {
		state = "fallo: " + text(action["error"])
	}
	return fmt.Sprintf("%s · %s · %s (%s)", text(action["name"]), text(action["operation"]),
		state, text(action["why"]))
}
