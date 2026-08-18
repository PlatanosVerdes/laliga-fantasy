// Package cli is the terminal output: tables that line up, money in millions, and colour
// that never carries meaning on its own. A port of the presentation half of fantasy/cli.py.
//
// Every status is also written: an unavailable player gets a "!" after his name and a doubtful
// one a "?", so the red and the yellow are a second reading rather than the only one. The same
// rule the page follows.
package cli

import (
	"fmt"
	"os"
	"strings"
)

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
)

// Colour is disabled when stdout is not a terminal, so a redirected run stays greppable.
// Checked with a stat rather than golang.org/x/term: one syscall against a dependency, and the
// project has none in either language.
var colour = isTerminal() && os.Getenv("NO_COLOR") == ""

func isTerminal() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func Paint(text, code string) string {
	if !colour {
		return text
	}
	return code + text + reset
}

func Red(text string) string    { return Paint(text, red) }
func Green(text string) string  { return Paint(text, green) }
func Yellow(text string) string { return Paint(text, yellow) }
func Cyan(text string) string   { return Paint(text, cyan) }
func Dim(text string) string    { return Paint(text, dim) }

// Money is the terminal's shorter format: millions with two decimals, thousands with none.
func Money(value any) string {
	amount, ok := asFloat(value)
	if !ok {
		return "-"
	}
	sign := ""
	if amount < 0 {
		sign = "-"
	}
	if amount < 0 {
		amount = -amount
	}
	switch {
	// 999,500 upwards is millions: rounding it to "1000K" would be a thousand-fold lie in the
	// unit.
	case amount >= 999_500:
		return fmt.Sprintf("%s%.2fM", sign, amount/1e6)
	case amount >= 1e3:
		return fmt.Sprintf("%s%.0fK", sign, amount/1e3)
	default:
		return fmt.Sprintf("%s%.0f", sign, amount)
	}
}

func Heading(text string) {
	fmt.Println()
	fmt.Println(Paint(text, bold))
}

// Table lines the columns up on display width rather than byte length, because a name with an
// accent is one column wide and two bytes long, and getting that wrong tilts the whole table.
func Table(headers []string, rows [][]string, right map[int]bool) string {
	if len(rows) == 0 {
		return Dim("  (sin resultados)")
	}
	widths := make([]int, len(headers))
	for index, header := range headers {
		widths[index] = width(header)
	}
	for _, row := range rows {
		for index, cell := range row {
			if index < len(widths) && width(cell) > widths[index] {
				widths[index] = width(cell)
			}
		}
	}

	line := func(values []string, strong bool) string {
		parts := make([]string, 0, len(values))
		for index, value := range values {
			if index >= len(widths) {
				continue
			}
			padding := widths[index] - width(value)
			if padding < 0 {
				padding = 0
			}
			if right[index] {
				parts = append(parts, strings.Repeat(" ", padding)+value)
			} else {
				parts = append(parts, value+strings.Repeat(" ", padding))
			}
		}
		text := strings.TrimRight(strings.Join(parts, "  "), " ")
		if strong {
			return Paint(text, bold)
		}
		return text
	}

	rules := make([]string, len(widths))
	for index, size := range widths {
		rules[index] = strings.Repeat("─", size)
	}
	out := []string{line(headers, true), Dim(strings.Join(rules, "  "))}
	for _, row := range rows {
		out = append(out, line(row, false))
	}
	for index := range out {
		out[index] = "  " + out[index]
	}
	return strings.Join(out, "\n")
}

// width is the display width, ignoring the escape sequences colour adds and counting a rune as
// one column.
func width(text string) int {
	stripped := text
	for {
		start := strings.Index(stripped, "\033[")
		if start < 0 {
			break
		}
		end := strings.Index(stripped[start:], "m")
		if end < 0 {
			break
		}
		stripped = stripped[:start] + stripped[start+end+1:]
	}
	return len([]rune(stripped))
}

// PlayerRows is the shared player table: identity, price, the two model numbers, how likely he
// is to start, where his value is going, and who he plays next.
func PlayerRows(players []map[string]any, costKey string,
	extra *Column) ([]string, [][]string, map[int]bool) {
	headers := []string{"#", "Jugador", "Pos", "Equipo", "Valor"}
	if costKey != "" {
		headers = append(headers, "Coste")
	}
	headers = append(headers, "xPts", "Pts/M", "Tit%", "7d%", "Rival", "Score")
	if extra != nil {
		headers = append(headers, extra.Header)
	}

	rows := make([][]string, 0, len(players))
	for index, player := range players {
		rival := "-"
		if name := text(player["next_rival"]); name != "" {
			where := "(F)"
			if truthy(player["next_home"]) {
				where = "(C)"
			}
			rival = trim(name, 12) + where
		}

		name := trim(text(player["name"]), 22)
		if !truthy(player["available"]) {
			name = Red(name + "!")
		} else if text(player["status"]) == "doubtful" {
			name = Yellow(name + "?")
		}

		row := []string{
			fmt.Sprintf("%d", index+1), name, text(player["position"]),
			fallback(text(player["team_short"]), "?"), Money(player["value"]),
		}
		if costKey != "" {
			row = append(row, Money(player[costKey]))
		}
		starts := "-"
		if probability, ok := asFloat(player["start_probability"]); ok {
			starts = fmt.Sprintf("%.0f", probability)
		}
		row = append(row,
			fmt.Sprintf("%.2f", number(player["xpts"])),
			fmt.Sprintf("%.2f", number(player["points_value"])),
			starts,
			fmt.Sprintf("%+.1f", number(player["projected_pct"])),
			rival,
			fmt.Sprintf("%+.2f", number(player["score"])),
		)
		if extra != nil {
			row = append(row, extra.Read(player))
		}
		rows = append(rows, row)
	}

	// Numbers right-aligned: a column of prices only reads as a column when the digits line
	// up. Jorge asked for it after seeing both.
	right := map[int]bool{0: true, 4: true}
	offset := 5
	if costKey != "" {
		right[5] = true
		offset = 6
	}
	for index := offset; index < offset+4; index++ {
		right[index] = true
	}
	right[offset+5] = true
	return headers, rows, right
}

// Column is an extra column a command adds to the shared table.
type Column struct {
	Header string
	Read   func(map[string]any) string
}

func trim(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func fallback(value, other string) string {
	if value != "" {
		return value
	}
	return other
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
		return fmt.Sprintf("%v", typed)
	}
	return fmt.Sprint(value)
}

func asFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case *float64:
		if typed == nil {
			return 0, false
		}
		return *typed, true
	case int:
		return float64(typed), true
	case *int:
		if typed == nil {
			return 0, false
		}
		return float64(*typed), true
	case string:
		if typed == "" {
			return 0, false
		}
	}
	return 0, false
}

func number(value any) float64 {
	amount, _ := asFloat(value)
	return amount
}

func truthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case *bool:
		return typed != nil && *typed
	case float64:
		return typed != 0
	}
	return false
}
