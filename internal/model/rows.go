package model

import "encoding/json"

// playerRow is a Player as the generic rows the advice layer and the page read. One
// conversion, through JSON, so the field names can only be the struct tags.
func playerRow(player Player) map[string]any {
	blob, err := json.Marshal(player)
	if err != nil {
		return map[string]any{}
	}
	var row map[string]any
	if err := json.Unmarshal(blob, &row); err != nil {
		return map[string]any{}
	}
	return row
}
