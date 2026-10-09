package server

import (
	"fmt"
	"strings"
)

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

// short is an amount the way a button says it: 2,5M, 850K.
func short(amount float64) string {
	sign := ""
	if amount < 0 {
		sign, amount = "−", -amount
	}
	if amount >= 999_500 {
		return sign + strings.Replace(fmt.Sprintf("%.1f", amount/1e6), ".", ",", 1) + "M"
	}
	return sign + fmt.Sprintf("%.0fK", amount/1e3)
}
