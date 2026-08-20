package server

import (
	"fmt"
	"regexp"
	"strings"
)

// sectionRE matches the page's own `<section id=...>` blocks; the renderer emits nothing else
// with that shape, so pulling them out is enough to serve the page in pieces.
// Ids can carry digits and dashes (one section per rival is `rival-1234`) and sections can
// carry attributes, so neither is assumed away: a section this misses is a section the live
// refresh silently stops updating.
var sectionRE = regexp.MustCompile(`(?s)<section id="([a-z][a-z0-9-]*)"[^>]*>(.*?)</section>`)

// Sections pulls each section out of a built page. The server re-renders the whole page (cheap,
// all in memory) and then serves the pieces, so there is exactly one renderer and the live page
// and the static file can never drift apart.
func Sections(page string) map[string]string {
	out := map[string]string{}
	for _, match := range sectionRE.FindAllStringSubmatch(page, -1) {
		out[match[1]] = match[2]
	}
	return out
}

func text(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case *string:
		if typed == nil {
			return ""
		}
		return *typed
	case float64:
		// Ids arrive as JSON numbers often enough that %v's scientific notation would turn
		// one into a string nothing matches.
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%v", typed)
	}
	return fmt.Sprint(value)
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case *float64:
		if typed == nil {
			return 0
		}
		return *typed
	case int:
		return float64(typed)
	// The scrapers return *int for a parsed integer, and reading one as zero is a silent
	// wrong answer: it turned Berenguer's 30% into 0%.
	case *int:
		if typed == nil {
			return 0
		}
		return float64(*typed)
	case int64:
		return float64(typed)
	case *int64:
		if typed == nil {
			return 0
		}
		return float64(*typed)
	}
	return 0
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case *bool:
		return typed != nil && *typed
	case float64:
		return typed != 0
	case string:
		return typed != "" && typed != "false"
	}
	return false
}

func mapOf(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func listOf(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
		return out
	}
	return nil
}

func fallback(value, other string) string {
	if value != "" {
		return value
	}
	return other
}

// thousands writes an amount the way the page does: dots every three digits.
func thousands(amount int64) string {
	digits := fmt.Sprintf("%d", amount)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var parts []string
	for len(digits) > 3 {
		parts = append([]string{digits[len(digits)-3:]}, parts...)
		digits = digits[:len(digits)-3]
	}
	parts = append([]string{digits}, parts...)
	return sign + strings.Join(parts, ".")
}
