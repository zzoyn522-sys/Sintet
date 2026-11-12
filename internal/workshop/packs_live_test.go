package workshop

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sintet/internal/identity"
)

// Opt-in verification of an actual catalog archive, without touching a user's
// instances or launching Minecraft. All downloaded content is temporary.
func TestLiveCatalogPackArchiveInstallation(t *testing.T) {
	archive := os.Getenv("SINTET_PACK_VERIFY_ARCHIVE")
	if archive == "" {
		t.Skip("actual archive installation is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	folder := t.TempDir()
	source, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(folder, "source.zip"))
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(output, source)
	source.Close()
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal("archive copy", copyErr, closeErr)
	}
	manager := NewManager(New(""))
	plan := PackPlan{ID: "actual-pack", Project: Project{Provider: "modrinth"}, Created: time.Now(), folder: folder}
	if err := manager.parsePack(ctx, filepath.Join(folder, "source.zip"), &plan); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SINTET_PACK_VERIFY_ONLY") == "1" {
		t.Logf("Validated actual archive: %d files, %s %s", plan.Files, plan.Loader, plan.LoaderVersion)
		return
	}
	manager.packPlans[plan.ID] = plan
	target := t.TempDir()
	t.Logf("Installing actual archive: %d files, %d declared bytes, %s %s", plan.Files, plan.Bytes, plan.Loader, plan.LoaderVersion)
	if err := manager.StartPack(ctx, plan.ID, Target{Directory: target, Game: plan.Game, Loader: plan.Loader}, nil); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for manager.State().Busy {
		select {
		case <-ctx.Done():
			manager.Cancel()
			t.Fatal(ctx.Err())
		case <-ticker.C:
			state := manager.State()
			t.Logf("%d/%d files, %.1f%%: %s", state.Done, state.Total, state.Percent, state.Stage)
		case <-time.After(100 * time.Millisecond):
		}
	}
	if state := manager.State(); state.Error != "" {
		t.Fatal(state.Error)
	}
	mods, err := Scan(target, "mods")
	if err != nil || len(mods) < 20 {
		t.Fatal("actual mods missing", len(mods), err)
	}
	t.Logf("Installed and verified actual archive: %d mod JARs", len(mods))
}

func TestLiveCatalogPackReliabilityAudit(t *testing.T) {
	if os.Getenv("SINTET_PACK_CATALOG_AUDIT") != "1" {
		t.Skip("real catalog audit is opt-in")
	}
	key, err := identity.ReadServiceSecret(os.Getenv("SINTET_CURSEFORGE_TEST_KEY_FILE"))
	if err != nil {
		t.Fatal("could not read local encrypted CurseForge key")
	}
	for _, provider := range []string{"modrinth", "curseforge"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			manager := NewManager(New(key))
			project := "fabulously-optimized"
			if provider == "curseforge" {
				results, err := manager.Client.SearchFiltered(ctx, provider, "Fabulously Optimized", 0, SearchFilters{Kind: "modpacks"})
				if err != nil {
					t.Fatal(err)
				}
				project = ""
				for _, item := range results.Items {
					if item.Name == "Fabulously Optimized" {
						project = item.ID
						break
					}
				}
				if project == "" {
					t.Fatal("actual CurseForge pack not found")
				}
			}
			versions, err := manager.Client.Versions(ctx, provider, project, "1.21.1", "fabric")
			if err != nil || len(versions) == 0 {
				t.Fatal("actual versions", err)
			}
			plan, err := manager.PreparePack(ctx, provider, project, versions[0].ID, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("Resolved %s: %d files, %d bytes", plan.Project.Name, plan.Files, plan.Bytes)
			target := t.TempDir()
			if err := manager.StartPack(ctx, plan.ID, Target{Directory: target, Game: plan.Game, Loader: plan.Loader}, nil); err != nil {
				t.Fatal(err)
			}
			for manager.State().Busy {
				select {
				case <-ctx.Done():
					manager.Cancel()
					t.Fatal(ctx.Err())
				case <-time.After(100 * time.Millisecond):
				}
			}
			if state := manager.State(); state.Error != "" {
				t.Fatal(state.Error)
			}
			mods, err := Scan(target, "mods")
			if err != nil || len(mods) < 20 {
				t.Fatal("actual pack did not install", len(mods), err)
			}
			t.Logf("Installed actual %s pack: %d verified JARs", provider, len(mods))
		})
	}
}
