package model

import (
	"regexp"
	"strings"
)

// Team names differ between the two sources, and the model resolves a fixture's rival by
// name. This is a port of matching.normalize / normalize_team, kept byte-compatible with
// it: the harness compares fixture_factor, which depends on the rival resolving the same
// way on both sides.

var (
	nonAlphanumeric = regexp.MustCompile(`[^a-z0-9 ]+`)
	whitespaceRun   = regexp.MustCompile(`\s+`)
	// Words that carry no identity: every other Spanish club has one.
	teamNoise = regexp.MustCompile(`\b(fc|cf|ud|cd|rc|rcd|ca|sd|club|de|del|real)\b`)
)

// ffTeamAliases mirrors config.FF_TEAM_ALIASES: futbolfantasy's short names mapped onto
// the official ones.
var ffTeamAliases = map[string]string{
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

// folded strips the accents Python removes via NFKD. Spelled out rather than pulled in
// from golang.org/x/text: this is the only place decomposition is needed, and the project
// is worth keeping dependency-free at both ends. Verified against the Python
// normalization for every team and player name in the feed.
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

func normalizeName(text string) string {
	if text == "" {
		return ""
	}
	lowered := folded.Replace(strings.ToLower(text))
	lowered = nonAlphanumeric.ReplaceAllString(lowered, " ")
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(lowered, " "))
}

func normalizeTeam(name string) string {
	base := normalizeName(name)
	if alias, ok := ffTeamAliases[base]; ok {
		base = alias
	}
	base = teamNoise.ReplaceAllString(base, " ")
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
