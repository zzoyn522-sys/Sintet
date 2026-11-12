package workshop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sintet/internal/identity"
	"strings"
	"testing"
)

func TestLiveFilterTaxonomy(t *testing.T) {
	if os.Getenv("SINTET_WORKSHOP_INTEGRATION") != "1" {
		t.Skip("live category metadata")
	}
	key, err := identity.ReadServiceSecret(os.Getenv("SINTET_CURSEFORGE_TEST_KEY_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	c := New(key)
	for _, topic := range Topics {
		if topic.CurseForge != "" {
			if _, err = c.curseCategory(context.Background(), topic.CurseForge); err != nil {
				t.Fatalf("%s: %v", topic.ID, err)
			}
		}
	}
	for _, provider := range []string{"modrinth", "curseforge"} {
		for _, topic := range []string{"pvp", "survival", "optimization"} {
			t.Run(provider+"/"+topic, func(t *testing.T) {
				result, err := c.SearchFiltered(context.Background(), provider, "", 0, SearchFilters{Game: "1.21.1", Loader: "fabric", Topic: topic, Sort: "relevance"})
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Items) == 0 {
					t.Fatal("expected matching mods for the combined filters")
				}
				for _, project := range result.Items[:min(2, len(result.Items))] {
					files, err := c.Versions(context.Background(), provider, project.ID, "1.21.1", "fabric")
					if err != nil || len(files) == 0 {
						t.Fatal("filtered result has no matching file", err)
					}
				}
				t.Logf("%d results returned for combined game, loader and topic filters", len(result.Items))
			})
		}
	}
}

func TestSearchFiltersAreSentToCatalogsAndResetCompletely(t *testing.T) {
	categoryCalls := 0
	var last *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = r
		if r.URL.Path == "/categories" {
			categoryCalls++
			w.Write([]byte(`{"data":[{"id":6814,"slug":"performance"}]}`))
			return
		}
		if r.URL.Path == "/search" {
			w.Write([]byte(`{"hits":[],"total_hits":0}`))
			return
		}
		w.Write([]byte(`{"data":[],"pagination":{"totalCount":0}}`))
	}))
	defer server.Close()
	c := New("test-key")
	c.Modrinth = server.URL
	c.CurseForge = server.URL
	filters := SearchFilters{Game: "1.21.1", Loader: "fabric", Topic: "optimization", Sort: "updated"}
	if _, err := c.SearchFiltered(context.Background(), "modrinth", "render", 0, filters); err != nil {
		t.Fatal(err)
	}
	var facets [][]string
	if err := json.Unmarshal([]byte(last.URL.Query().Get("facets")), &facets); err != nil {
		t.Fatal(err)
	}
	expected := [][]string{{"project_type:mod"}, {"versions:1.21.1"}, {"categories:fabric"}, {"categories:optimization"}}
	data, _ := json.Marshal(facets)
	want, _ := json.Marshal(expected)
	if string(data) != string(want) {
		t.Fatal("filters must be separate AND facet groups")
	}
	if last.URL.Query().Get("index") != "updated" || last.URL.Query().Get("query") != "render" {
		t.Fatal("sort or text query lost")
	}
	for i := 0; i < 2; i++ {
		if _, err := c.SearchFiltered(context.Background(), "curseforge", "render", 0, filters); err != nil {
			t.Fatal(err)
		}
	}
	query := last.URL.Query()
	if query.Get("gameVersion") != "1.21.1" || query.Get("modLoaderType") != "4" || query.Get("categoryId") != "6814" || query.Get("sortField") != "3" {
		t.Fatal("CurseForge filters not translated correctly")
	}
	if categoryCalls != 1 {
		t.Fatal("categories were not cached")
	}
	if _, err := c.SearchFiltered(context.Background(), "curseforge", "hud", 0, SearchFilters{Topic: "pvp"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(last.URL.Query().Get("searchFilter"), "pvp") || !strings.Contains(last.URL.Query().Get("searchFilter"), "hud") {
		t.Fatal("PvP search discarded the user's query")
	}
	if _, err := c.SearchFiltered(context.Background(), "curseforge", "", 0, SearchFilters{}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"gameVersion", "modLoaderType", "categoryId"} {
		if last.URL.Query().Has(key) {
			t.Fatal("reset left a filter active")
		}
	}
	if _, err := c.SearchFiltered(context.Background(), "modrinth", "", 0, SearchFilters{Loader: "bad-loader"}); err == nil {
		t.Fatal("invalid filter accepted")
	}
}
