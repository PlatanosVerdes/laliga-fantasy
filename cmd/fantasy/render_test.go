package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/render"
)

func TestTwoPagesRenderAtOnceWithoutRacingOnTheRenderGlobals(t *testing.T) {
	config.CacheRoot = t.TempDir()
	crests := `{"3":"data:image/png;base64,AAAA"}`
	if err := os.WriteFile(filepath.Join(config.CacheRoot, "crests.json"), []byte(crests),
		0o600); err != nil {
		t.Fatal(err)
	}
	pages := make([]string, 2)
	var group sync.WaitGroup
	for index := range pages {
		group.Go(func() {
			prepareRender()
			document := render.Document{Universe: map[string]any{"players": []any{}}}
			pages[index], _ = document.Render()
		})
	}
	group.Wait()
	for _, page := range pages {
		if !strings.Contains(page, ".crest-3{") || !strings.Contains(page, buildVersion) {
			t.Errorf("a page without the crests or the build: %.200s", page)
		}
	}
}
