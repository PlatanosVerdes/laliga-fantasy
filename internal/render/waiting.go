package render

import (
	"fmt"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/advice"
)

// waiting is whether the row's futbolfantasy page is still on its way.
func waiting(row map[string]any) bool { return truthy(row[advice.PendingKey]) }

// ceilingAge is how old the stored ceiling the row was drawn with is, while it is read again.
func ceilingAge(row map[string]any) string { return text(row[advice.AgeKey]) }

// held is how many market candidates still wait for their page. Every judgement that compares
// candidates by their ceilings (the plan, the buy verdicts, the stand-in for a sale) waits for
// all of them: proposed from half, it would read a missing ceiling as futbolfantasy saying no.
func (d Document) held() int { return advice.Waiting(d.Advice, advice.CandidateBuckets...) }

func heldNote(missing int) string {
	if missing == 1 {
		return "Esperando 1 ficha de futbolfantasy: las decisiones comparan sus techos " +
			"rentables y salen en cuanto llegue."
	}
	return fmt.Sprintf("Esperando %d fichas de futbolfantasy: las decisiones comparan sus "+
		"techos rentables y salen en cuanto lleguen.", missing)
}
