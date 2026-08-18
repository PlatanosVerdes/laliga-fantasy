package model

import (
	"strings"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/matching"
)

// MatchAbsences says which absence belongs to which player.
//
// The surname fallback exists because futbolfantasy writes full names where LaLiga writes
// nicknames ("Dani Vivian" vs "Vivian"). Applied blindly it gives a healthy player somebody
// else's injury whenever they share a surname: Pau Lopez had Diego Lopez's, Joan Garcia and
// Alvaro Garcia had Kike Garcia's, 24 players in all. So the fallback needs the surname to be
// unique among the absences *and* the first names to be compatible.
func MatchAbsences(players []map[string]any, rows []map[string]any) map[string]map[string]any {
	exact := map[string]map[string]any{}
	bySurname := map[string]map[string]any{}
	counts := map[string]int{}

	for _, row := range rows {
		name := text(row["name"])
		exact[matching.Normalize(name)] = row
		key := matching.Surname(name)
		if _, seen := bySurname[key]; !seen {
			bySurname[key] = row
		}
		counts[key]++
		if slug := text(row["slug"]); slug != "" {
			spaced := strings.ReplaceAll(slug, "-", " ")
			if _, seen := exact[spaced]; !seen {
				exact[spaced] = row
			}
		}
	}

	out := map[string]map[string]any{}
	for _, player := range players {
		for _, candidate := range []string{text(player["name"]), matching.PlayerLabel(player)} {
			if candidate == "" {
				continue
			}
			if found, ok := exact[matching.Normalize(candidate)]; ok {
				out[text(player["id"])] = found
				break
			}
			key := matching.Surname(candidate)
			found, ok := bySurname[key]
			if !ok || counts[key] > 1 {
				continue
			}
			if firstNamesAgree(candidate, text(found["name"])) {
				out[text(player["id"])] = found
				break
			}
		}
	}
	return out
}

// firstNamesAgree is whether two spellings can be the same person: true when the shorter one
// carries no first name to disagree with — a bare surname ("Vivian") or initials ("F. Calero",
// "R.P. Bigas") — or when the first initials match. False for "Pau Lopez" against "Diego
// Lopez", which is the whole point.
func firstNamesAgree(short, full string) bool {
	tokens := strings.Fields(matching.Normalize(short))
	if len(tokens) < 2 {
		return true
	}
	first := tokens[0]
	// An initial, however it was punctuated: "F.", "R.P.", "A".
	if len(first) <= 2 {
		return true
	}
	other := strings.Fields(matching.Normalize(full))
	return len(other) > 0 && other[0][:1] == first[:1]
}
