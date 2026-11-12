package workshop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sintet/internal/identity"
)

func TestLiveShaderWorkshop(t *testing.T) {
	if os.Getenv("SINTET_SHADER_LIVE") != "1" {
		t.Skip("live shader catalog check")
	}
	key, err := identity.ReadServiceSecret(os.Getenv("SINTET_CURSEFORGE_TEST_KEY_FILE"))
	if err != nil {
		t.Fatal("could not read local CurseForge key")
	}
	for _, provider := range []string{"modrinth", "curseforge"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			client := New(key)
			manager := NewManager(client)
			const game = "1.21.1"
			found, err := client.SearchFiltered(ctx, provider, "MakeUp", 0, SearchFilters{Kind: "shaders", Game: game})
			if err != nil || len(found.Items) == 0 {
				t.Fatal("shader search", err)
			}
			project := found.Items[0]
			files, err := client.ZipPackVersions(ctx, provider, project.ID, game, "shaders")
			if err != nil || len(files) == 0 || project.Kind != "shaders" {
				t.Fatal("shader versions", project.Name, err)
			}
			var selected Version
			for _, file := range files {
				if file.URL != "" && file.Compatible(game, "") {
					selected = file
					break
				}
			}
			if selected.ID == "" {
				t.Fatal("no public shader archive", project.Name)
			}
			target := Target{Game: game, Loader: "fabric", Directory: t.TempDir()}
			plan, err := manager.PrepareZipPack(ctx, provider, project.ID, selected.ID, "shaders", target)
			if err != nil {
				t.Fatal(err)
			}
			if err := manager.Start(ctx, plan.ID, true); err != nil {
				t.Fatal(err)
			}
			for manager.State().Busy && ctx.Err() == nil {
				time.Sleep(50 * time.Millisecond)
			}
			if state := manager.State(); state.Busy || state.Error != "" {
				t.Fatal("shader install", state)
			}
			items, err := Scan(target.Directory, "shaders")
			if err != nil || len(items) != 1 || !items[0].Enabled || items[0].Provider != provider {
				t.Fatal("shader list", items, err)
			}
			filename := items[0].Filename
			if err := ToggleLocal(target.Directory, "shaders", filename, false); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(target.Directory, "shaderpacks", filename+disabledSuffix)); err != nil {
				t.Fatal(err)
			}
			if err := ToggleLocal(target.Directory, "shaders", filename+disabledSuffix, true); err != nil {
				t.Fatal(err)
			}
			if err := RemoveLocal(target.Directory, "shaders", filename); err != nil {
				t.Fatal(err)
			}
			items, err = Scan(target.Directory, "shaders")
			if err != nil || len(items) != 0 {
				t.Fatal("removed shader remains", items, err)
			}
			t.Logf("Verified search, download, toggle and removal for %s", project.Name)
		})
	}
}

func TestLiveWorkshopTranslation(t *testing.T) {
	if os.Getenv("SINTET_SHADER_LIVE") != "1" {
		t.Skip("live translation check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	const description = "Adds dynamic lighting to Minecraft."
	translated, err := New("").TranslateDescription(ctx, description, "ru")
	if err != nil || !strings.Contains(strings.ToLower(translated), "освещени") {
		t.Fatal(translated, err)
	}
}
