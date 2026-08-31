package outlook

import (
	"math"
	"strconv"
	"strings"
)

// Rows reach this package two ways: parsed from JSON, where every value is a plain float64 or
// string, and straight from the model, where absence is a nil pointer. Both have to read the
// same, because the same page is rendered from both.
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
		if typed == math.Trunc(typed) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	}
	return ""
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
	case *int:
		if typed == nil {
			return 0
		}
		return float64(*typed)
	case int:
		return float64(typed)
	case bool:
		if typed {
			return 1
		}
		return 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0
		}
		return parsed
	}
	return 0
}

// optional keeps "no data" apart from zero. A player with no published odds of starting is not
// a player with no chance of starting, and reading one as the other silences a whole squad.
func optional(value any) *float64 {
	switch typed := value.(type) {
	case nil:
		return nil
	case float64:
		return &typed
	case *float64:
		return typed
	case *int:
		if typed == nil {
			return nil
		}
		converted := float64(*typed)
		return &converted
	case int:
		converted := float64(typed)
		return &converted
	}
	return nil
}

func mapOf(value any) Row {
	if row, ok := value.(Row); ok {
		return row
	}
	return nil
}

func rowsOf(value any) []Row {
	switch typed := value.(type) {
	case []Row:
		return typed
	case []any:
		out := make([]Row, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(Row); ok {
				out = append(out, row)
			}
		}
		return out
	}
	return nil
}

func asWords(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, text(item))
		}
		return out
	}
	return nil
}
