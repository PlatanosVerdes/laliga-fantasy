// Package favourites is the starred-players file: a flat id -> {id, name, note} map that both
// the CLI and the page write.
package favourites

import (
	"encoding/json"
	"os"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

type Entry = map[string]any

func Load() (map[string]Entry, error) {
	out := map[string]Entry{}
	body, err := os.ReadFile(config.FavouritesFile)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	// A corrupt file is not worth losing the session over: treat it as empty.
	if err := json.Unmarshal(body, &out); err != nil {
		return map[string]Entry{}, nil
	}
	return out, nil
}

func Save(favourites map[string]Entry) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	blob, err := json.MarshalIndent(favourites, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(config.FavouritesFile, blob, 0o600)
}

// Toggle flips the star and reports where it landed.
func Toggle(id string, name any) (bool, error) {
	favourites, err := Load()
	if err != nil {
		return false, err
	}
	if _, ok := favourites[id]; ok {
		delete(favourites, id)
		return false, Save(favourites)
	}
	favourites[id] = Entry{"id": id, "name": name, "note": nil}
	return true, Save(favourites)
}
