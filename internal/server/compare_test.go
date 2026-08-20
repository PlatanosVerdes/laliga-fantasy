package server

import "testing"

func rowsForSearch() []map[string]any {
	return []map[string]any{
		{"id": "1", "name": "Álvaro García", "score": 1.0},
		{"id": "2", "name": "García", "score": 0.5},
		{"id": "3", "name": "Guruzeta", "score": 2.0},
		{"id": "4", "name": "A. Abqar", "score": 0.1},
		{"id": "5", "name": "", "score": 9.0},
	}
}

func TestSearchIgnoresAccentsAndCase(t *testing.T) {
	found := search(rowsForSearch(), "GARCIA")
	if len(found) != 2 {
		t.Fatalf("garcia deberia encontrar a los dos: %v", found)
	}
	// El que empieza por lo que escribes va primero, no el de mas score.
	if text(found[0]["id"]) != "2" {
		t.Errorf("el primero deberia ser el que empieza por García, salio %v", found[0]["id"])
	}
}

func TestSearchMatchesAnyWord(t *testing.T) {
	found := search(rowsForSearch(), "alvaro")
	if len(found) != 1 || text(found[0]["id"]) != "1" {
		t.Errorf("por nombre de pila deberia salir Álvaro García: %v", found)
	}
}

func TestSearchNeedsTwoLetters(t *testing.T) {
	if found := search(rowsForSearch(), "g"); len(found) != 0 {
		t.Errorf("una sola letra devuelve media liga: %v", found)
	}
	if found := search(rowsForSearch(), "  "); len(found) != 0 {
		t.Errorf("en blanco no busca nada: %v", found)
	}
}

func TestSearchCapsResults(t *testing.T) {
	rows := make([]map[string]any, 0, searchMax+8)
	for index := 0; index < searchMax+8; index++ {
		rows = append(rows, map[string]any{"id": "x", "name": "Garcia", "score": float64(index)})
	}
	if found := search(rows, "garcia"); len(found) != searchMax {
		t.Errorf("la lista tiene que caber en la bandeja: %d", len(found))
	}
}
