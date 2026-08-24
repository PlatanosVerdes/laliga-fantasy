package server

import "testing"

func TestPadLinesOpensTheMissingSlot(t *testing.T) {
	lines := map[string][]map[string]any{
		"goalkeeper": {{"id": "1"}},
		"defender":   {{"id": "2"}, {"id": "3"}, {"id": "4"}, {"id": "5"}},
		"midfield":   {{"id": "6"}, {"id": "7"}, {"id": "8"}},
		"striker":    {{"id": "9"}, {"id": "10"}},
	}
	padLines(lines, []int{4, 4, 2})
	if len(lines["midfield"]) != 4 {
		t.Fatalf("un 4-4-2 con tres medios sigue pidiendo cuatro plazas: %d",
			len(lines["midfield"]))
	}
	if lines["midfield"][3] != nil {
		t.Errorf("la plaza que falta tiene que llegar vacia: %v", lines["midfield"][3])
	}
	if len(lines["defender"]) != 4 || len(lines["striker"]) != 2 {
		t.Errorf("las lineas completas no se tocan: %v", lines)
	}
}

func TestPadLinesAlwaysKeepsAKeeper(t *testing.T) {
	lines := map[string][]map[string]any{}
	padLines(lines, nil)
	if len(lines["goalkeeper"]) != 1 || lines["goalkeeper"][0] != nil {
		t.Errorf("sin formacion conocida, el portero sigue siendo una plaza: %v", lines)
	}
}
