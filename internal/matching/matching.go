// Package matching resolves the same player across two sources that disagree about
// everything: LaLiga uses team id 3 for Athletic where futbolfantasy uses it for Barcelona,
// and LaLiga publishes short nicknames ("f de jong", "aitor fdez") where futbolfantasy
// writes full names. So teams are matched by normalized name and players by a series of
// passes from strict to loose.
//
// A port of fantasy/matching.py. The rule that makes it safe is not any single pass but the
// commit condition: a pass only takes a pair when it is unambiguous in *both* directions,
// which is what stops "raul" from stealing the slot of "raul moro".
package matching

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var ffPositions = map[string]int{
	"portero": 1, "defensa": 2, "centrocampista": 3, "mediocampista": 3, "medio": 3,
	"delantero": 4, "entrenador": 5,
}

// Words that carry no identity: every other Spanish club has one.
var noise = regexp.MustCompile(`\b(fc|cf|ud|cd|rc|rcd|ca|sd|club|de|del|real)\b`)

var (
	nonAlphanumeric = regexp.MustCompile(`[^a-z0-9 ]+`)
	whitespaceRun   = regexp.MustCompile(`\s+`)
)

var folded = strings.NewReplacer(
	"á", "a", "à", "a", "ä", "a", "â", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ë", "e", "ê", "e",
	"í", "i", "ì", "i", "ï", "i", "î", "i",
	"ó", "o", "ò", "o", "ö", "o", "ô", "o", "õ", "o", "ø", "o",
	"ú", "u", "ù", "u", "ü", "u", "û", "u",
	"ç", "c", "ć", "c", "č", "c", "ñ", "n", "ń", "n",
	"ý", "y", "ÿ", "y", "š", "s", "ś", "s", "ž", "z", "ź", "z",
	"ł", "l", "đ", "d", "ř", "r", "ť", "t", "ğ", "g", "ı", "i",
)

// FFTeamAliases maps futbolfantasy's short team names onto the official ones.
var FFTeamAliases = map[string]string{
	"alaves": "deportivo alaves", "athletic": "athletic club",
	"atletico": "atletico de madrid", "barcelona": "fc barcelona",
	"betis": "real betis", "celta": "celta", "deportivo": "rc deportivo",
	"elche": "elche cf", "espanyol": "rcd espanyol", "getafe": "getafe cf",
	"girona": "girona fc", "levante": "levante ud", "malaga": "malaga cf",
	"mallorca": "rcd mallorca", "osasuna": "ca osasuna", "oviedo": "real oviedo",
	"racing": "r racing club", "rayo": "rayo vallecano", "real madrid": "real madrid",
	"real sociedad": "real sociedad", "sevilla": "sevilla fc",
	"valencia": "valencia cf", "villarreal": "villarreal cf",
}

func Normalize(text string) string {
	if text == "" {
		return ""
	}
	lowered := folded.Replace(strings.ToLower(text))
	lowered = nonAlphanumeric.ReplaceAllString(lowered, " ")
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(lowered, " "))
}

func NormalizeTeam(name string) string {
	base := Normalize(name)
	if alias, ok := FFTeamAliases[base]; ok {
		base = alias
	}
	base = noise.ReplaceAllString(base, " ")
	// Single letters go, so "C.A. Osasuna" and "R. Racing Club" reduce like their short
	// forms do.
	tokens := make([]string, 0, 4)
	for _, token := range strings.Fields(base) {
		if len(token) > 1 {
			tokens = append(tokens, token)
		}
	}
	return strings.Join(tokens, " ")
}

func FFPositionID(position string) int {
	return ffPositions[Normalize(position)]
}

func Surname(name string) string {
	tokens := strings.Fields(Normalize(name))
	if len(tokens) == 0 {
		return ""
	}
	return tokens[len(tokens)-1]
}

// BuildTeamIndex is normalized team name -> LaLiga team record. First wins, and the key
// order is the same as Python's, because the feed carries historic clubs and a collision
// resolved the other way picks a different team.
func BuildTeamIndex(teams []map[string]any) map[string]map[string]any {
	index := map[string]map[string]any{}
	for _, team := range teams {
		for _, key := range []string{"name", "slug", "shortName"} {
			normalized := NormalizeTeam(text(team[key]))
			if normalized == "" {
				continue
			}
			if _, taken := index[normalized]; !taken {
				index[normalized] = team
			}
		}
	}
	return index
}

var tokenAliases = map[string]string{
	"jr": "junior", "fdez": "fernandez", "fdz": "fernandez", "glez": "gonzalez",
	"gzlez": "gonzalez", "hdez": "hernandez", "mtnez": "martinez",
	"dguez": "dominguez", "rgez": "rodriguez", "rodz": "rodriguez",
}

// isAbbrev: "fdez" -> "fernandez". Same initial and the letters in order.
func isAbbrev(short, long string) bool {
	if tokenAliases[short] == long {
		return true
	}
	if len(short) < 3 || len(short) >= len(long) || short[0] != long[0] {
		return false
	}
	position := 0
	for _, letter := range short {
		found := strings.IndexRune(long[position:], letter)
		if found < 0 {
			return false
		}
		position += found + 1
	}
	return true
}

func tokenMatch(short, long string) bool {
	return short == long || strings.HasPrefix(long, short) || isAbbrev(short, long)
}

// prefixSubset: every LaLiga token maps to a distinct futbolfantasy token.
func prefixSubset(mine, theirs []string) bool {
	if len(mine) == 0 || len(mine) > len(theirs) {
		return false
	}
	used := map[int]bool{}
	for _, token := range mine {
		found := -1
		for index, other := range theirs {
			if !used[index] && tokenMatch(token, other) {
				found = index
				break
			}
		}
		if found < 0 {
			return false
		}
		used[found] = true
	}
	return true
}

// nicknameVariant: "alex balde" against "alejandro balde" — same surname, and the first
// names share a stem of at least three letters.
func nicknameVariant(mine, theirs []string) bool {
	if len(mine) < 2 || len(theirs) < 2 || mine[len(mine)-1] != theirs[len(theirs)-1] {
		return false
	}
	shared := 0
	one, other := mine[0], theirs[0]
	for index := 0; index < len(one) && index < len(other); index++ {
		if one[index] != other[index] {
			break
		}
		shared++
	}
	return shared >= 3
}

// fuzzy is difflib.SequenceMatcher's ratio, which is 2*M/T where M is the number of matching
// characters found by its recursive longest-match walk. Reimplemented rather than
// approximated with an edit distance: the threshold of 0.82 was tuned against this measure,
// and a different similarity would move which pairs commit.
func fuzzy(mine, theirs string) bool {
	return ratio(mine, theirs) >= 0.82
}

func ratio(a, b string) float64 {
	total := len(a) + len(b)
	if total == 0 {
		return 1
	}
	return 2 * float64(matchingChars(a, b)) / float64(total)
}

// matchingChars mirrors difflib's get_matching_blocks: find the longest common block, then
// recurse on what is left of it and what is right of it.
func matchingChars(a, b string) int {
	if a == "" || b == "" {
		return 0
	}
	bestA, bestB, bestSize := longestMatch(a, b)
	if bestSize == 0 {
		return 0
	}
	return bestSize +
		matchingChars(a[:bestA], b[:bestB]) +
		matchingChars(a[bestA+bestSize:], b[bestB+bestSize:])
}

// longestMatch is difflib's: the earliest longest block, preferring the lowest index in a
// then in b, which is what makes the walk deterministic.
func longestMatch(a, b string) (int, int, int) {
	bestA, bestB, bestSize := 0, 0, 0
	// j2len[j] is the length of the longest block ending at a[i], b[j].
	j2len := map[int]int{}
	for i := 0; i < len(a); i++ {
		newJ2len := map[int]int{}
		for j := 0; j < len(b); j++ {
			if a[i] != b[j] {
				continue
			}
			size := j2len[j-1] + 1
			newJ2len[j] = size
			if size > bestSize {
				bestA, bestB, bestSize = i-size+1, j-size+1, size
			}
		}
		j2len = newJ2len
	}
	return bestA, bestB, bestSize
}

type preparedPlayer struct {
	Player    map[string]any
	ID        string
	Team      string
	Position  int
	Variants  []string
	TokenSets [][]string
}

type preparedRow struct {
	Row      map[string]any
	Norm     string
	Display  string
	Tokens   []string
	Team     string
	Position int
	live     bool
}

// pass is one matching rule.
type pass func(*preparedPlayer, *preparedRow) bool

func passes() []pass {
	return []pass{
		// Exact, on any spelling of the name.
		func(p *preparedPlayer, r *preparedRow) bool {
			for _, variant := range p.Variants {
				if variant == r.Norm || (r.Display != "" && variant == r.Display) {
					return true
				}
			}
			return false
		},
		// Same tokens in any order.
		func(p *preparedPlayer, r *preparedRow) bool {
			for _, tokens := range p.TokenSets {
				if sameSet(tokens, r.Tokens) {
					return true
				}
			}
			return false
		},
		func(p *preparedPlayer, r *preparedRow) bool {
			for _, tokens := range p.TokenSets {
				if prefixSubset(tokens, r.Tokens) {
					return true
				}
			}
			return false
		},
		func(p *preparedPlayer, r *preparedRow) bool {
			for _, tokens := range p.TokenSets {
				if nicknameVariant(tokens, r.Tokens) {
					return true
				}
			}
			return false
		},
		func(p *preparedPlayer, r *preparedRow) bool {
			for _, variant := range p.Variants {
				if fuzzy(variant, r.Norm) {
					return true
				}
			}
			return false
		},
	}
}

func sameSet(one, other []string) bool {
	if len(one) == 0 && len(other) == 0 {
		return true
	}
	left, right := map[string]bool{}, map[string]bool{}
	for _, token := range one {
		left[token] = true
	}
	for _, token := range other {
		right[token] = true
	}
	if len(left) != len(right) {
		return false
	}
	for token := range left {
		if !right[token] {
			return false
		}
	}
	return true
}

func nameVariants(player map[string]any) []string {
	var out []string
	for _, key := range []string{"nickname", "name", "slug"} {
		if value := text(player[key]); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// PlayerLabel is the display name: the nickname, or the full name, or the id.
func PlayerLabel(player map[string]any) string {
	for _, key := range []string{"nickname", "name", "id"} {
		if value := text(player[key]); value != "" {
			return value
		}
	}
	return ""
}

func SlugifyFF(name string) string {
	return strings.ReplaceAll(Normalize(name), " ", "-")
}

// MatchMarket returns (LaLiga player id -> futbolfantasy row, unmatched rows).
func MatchMarket(players []map[string]any, marketRows []map[string]any,
	teamIndex map[string]map[string]any) (map[string]map[string]any, []map[string]any) {

	ffTeamToLaLiga := map[string]string{}
	for _, row := range marketRows {
		if team, ok := teamIndex[NormalizeTeam(text(row["ff_team"]))]; ok {
			ffTeamToLaLiga[text(row["ff_team_id"])] = text(team["id"])
		}
	}

	prepared := make([]*preparedPlayer, 0, len(players))
	for _, player := range players {
		var variants []string
		for _, variant := range nameVariants(player) {
			variants = append(variants, Normalize(variant))
		}
		// "R.P. Bigas" also has to be tried as plain "bigas".
		for _, variant := range append([]string(nil), variants...) {
			var kept []string
			for _, token := range strings.Fields(variant) {
				if len(token) > 1 {
					kept = append(kept, token)
				}
			}
			trimmed := strings.Join(kept, " ")
			if trimmed != "" && trimmed != variant {
				variants = append(variants, trimmed)
			}
		}

		entry := &preparedPlayer{Player: player, ID: text(player["id"]),
			Team: text(player["teamId"]), Position: int(number(player["positionId"]))}
		for _, variant := range variants {
			if variant == "" {
				continue
			}
			entry.Variants = append(entry.Variants, variant)
			entry.TokenSets = append(entry.TokenSets, strings.Fields(variant))
		}
		prepared = append(prepared, entry)
	}

	rowsPrepared := make([]*preparedRow, 0, len(marketRows))
	for _, row := range marketRows {
		norm := Normalize(text(row["ff_name"]))
		rowsPrepared = append(rowsPrepared, &preparedRow{
			Row: row, Norm: norm, Display: Normalize(text(row["display_name"])),
			Tokens: strings.Fields(norm),
			Team:   ffTeamToLaLiga[text(row["ff_team_id"])],
			Position: FFPositionID(text(row["position"])), live: true,
		})
	}

	matched := map[string]map[string]any{}
	openPlayers := map[string]*preparedPlayer{}
	order := make([]string, 0, len(prepared))
	for _, entry := range prepared {
		openPlayers[entry.ID] = entry
		order = append(order, entry.ID)
	}

	// Second round ignores the team, so a mid-window transfer — a player futbolfantasy still
	// lists at his old club — still resolves.
	for _, sameTeam := range []bool{true, false} {
		for _, predicate := range passes() {
			for progress := true; progress; {
				progress = false
				type pair struct {
					row    *preparedRow
					player *preparedPlayer
				}
				var pairs []pair
				for _, row := range rowsPrepared {
					if !row.live {
						continue
					}
					var hits []*preparedPlayer
					// Iterated in the players' original order so a tie is broken the same
					// way on both sides.
					for _, id := range order {
						player, open := openPlayers[id]
						if !open {
							continue
						}
						if sameTeam && row.Team != "" && player.Team != row.Team {
							continue
						}
						if predicate(player, row) {
							hits = append(hits, player)
						}
					}
					if len(hits) > 1 && row.Position != 0 {
						var narrowed []*preparedPlayer
						for _, player := range hits {
							if player.Position == row.Position {
								narrowed = append(narrowed, player)
							}
						}
						if len(narrowed) > 0 {
							hits = narrowed
						}
					}
					if len(hits) == 1 {
						pairs = append(pairs, pair{row, hits[0]})
					}
				}

				// Only commit what is unambiguous in both directions: a player claimed by
				// two rows is a coin toss, and a coin toss here is a wrong player.
				claimed := map[string]int{}
				for _, item := range pairs {
					claimed[item.player.ID]++
				}
				for _, item := range pairs {
					if claimed[item.player.ID] != 1 {
						continue
					}
					if _, open := openPlayers[item.player.ID]; !open {
						continue
					}
					matched[item.player.ID] = item.row.Row
					delete(openPlayers, item.player.ID)
					item.row.live = false
					progress = true
				}
			}
		}
	}

	var unmatched []map[string]any
	for _, row := range rowsPrepared {
		if row.live {
			unmatched = append(unmatched, row.Row)
		}
	}
	return matched, unmatched
}

// SortedKeys keeps output stable for comparison.
func SortedKeys(source map[string]map[string]any) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func text(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		// Ids arrive as numbers when they come from JSON: "1300", never "1300.0".
		if typed == math.Trunc(typed) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	}
	return ""
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	}
	return 0
}
