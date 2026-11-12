package workshop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Project struct {
	Downloads   int64    `json:"downloads"`
	Kind        string   `json:"kind"`
	Preview     string   `json:"preview"`
	Tags        []string `json:"tags"`
	ID          string   `json:"id"`
	Provider    string   `json:"provider"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug,omitempty"`
	CreatedAt   string   `json:"createdAt,omitempty"`
	UpdatedAt   string   `json:"updatedAt,omitempty"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Author      string   `json:"author"`
	URL         string   `json:"url"`
	Games       []string `json:"games"`
	Loaders     []string `json:"loaders"`
}
type Dependency struct {
	Project string `json:"project"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}
type Version struct {
	Kind         string       `json:"kind"`
	Release      string       `json:"release"`
	ID           string       `json:"id"`
	Project      string       `json:"project"`
	Name         string       `json:"name"`
	Filename     string       `json:"filename"`
	URL          string       `json:"-"`
	URLs         []string     `json:"-"`
	Hash         string       `json:"-"`
	Algorithm    string       `json:"-"`
	Size         int64        `json:"size"`
	Games        []string     `json:"games"`
	Loaders      []string     `json:"loaders"`
	Dependencies []Dependency `json:"dependencies"`
}
type SearchResult struct {
	NextOffset int       `json:"nextOffset"`
	Items      []Project `json:"items"`
	Total      int       `json:"total"`
	Next       bool      `json:"next"`
}
type Client struct {
	translationMu                     sync.Mutex
	translationBlockedUntil           time.Time
	categoryMu                        sync.Mutex
	categoryIDs                       map[string]int
	HTTP                              *http.Client
	Modrinth, CurseForge, Sintet, Key string
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
var gameVersion = regexp.MustCompile(`^(?:[0-9]|[ab][0-9])[A-Za-z0-9_.+\-]{1,63}$`)

func New(key string) *Client {
	native := strings.TrimRight(os.Getenv("SINTET_ID_URL"), "/")
	if native == "" {
		native = "https://sintet.xyz"
	}
	return &Client{HTTP: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, Sintet: native + "/api/catalog", Modrinth: "https://api.modrinth.com/v2", CurseForge: "https://api.curseforge.com/v1", Key: key}
}
func (c *Client) get(ctx context.Context, provider, path string, out any) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err = c.getOnce(ctx, provider, path, out)
		var temporary *transferError
		if !errors.As(err, &temporary) || attempt == 2 {
			return err
		}
		if err = waitTransferRetry(ctx, attempt, err); err != nil {
			return err
		}
	}
	return err
}

func (c *Client) getOnce(ctx context.Context, provider, path string, out any) error {
	base := c.Modrinth
	if provider == "curseforge" {
		if c.Key == "" {
			return errors.New("ключ CurseForge не настроен")
		}
		base = c.CurseForge
	} else if provider == "sintedmods" {
		base = c.Sintet
	} else if provider != "modrinth" {
		return errors.New("неизвестный каталог")
	}
	request, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "Sintet/0.9 (sintet.xyz)")
	request.Header.Set("Accept", "application/json")
	if provider == "curseforge" {
		request.Header.Set("x-api-key", c.Key)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return &transferError{err: fmt.Errorf("%s временно недоступен: %w", provider, err)}
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		err := fmt.Errorf("%s: ответ %d", provider, response.StatusCode)
		if transientStatus(response.StatusCode) {
			return &transferError{err: err, delay: retryDelay(response.Header.Get("Retry-After"))}
		}
		return err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (12<<20)+1))
	if err != nil {
		return &transferError{err: err}
	}
	if len(data) > 12<<20 {
		return errors.New("ответ каталога превышает допустимый размер")
	}
	if json.Unmarshal(data, out) != nil {
		return errors.New("неверный ответ каталога")
	}
	return nil
}
func Loaders(values []string) []string {
	result := []string{}
	for _, name := range []string{"fabric", "forge", "neoforge", "quilt"} {
		for _, value := range values {
			if strings.EqualFold(name, value) {
				result = append(result, name)
				break
			}
		}
	}
	return result
}
func Games(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, v := range values {
		if gameVersion.MatchString(v) && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return naturalLess(result[i], result[j]) })
	return result
}
func naturalLess(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, ex := strconv.Atoi(pa[i])
		y, ey := strconv.Atoi(pb[i])
		if ex == nil && ey == nil {
			if x != y {
				return x < y
			}
		} else if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return len(pa) < len(pb)
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (v Version) Compatible(game, loader string) bool {
	if isZipPack(v.Kind) {
		return contains(v.Games, game)
	}
	return contains(v.Games, game) && contains(v.Loaders, loader)
}

type mrProject struct {
	Downloads  int64  `json:"downloads"`
	Type       string `json:"project_type"`
	Categories []string
	Gallery    []struct {
		URL      string
		Featured bool
	}
	ID, Title, Description, Slug, Body string
	IconURL                            string   `json:"icon_url"`
	GameVersions                       []string `json:"game_versions"`
	Loaders                            []string
}

func (p mrProject) project() Project {
	kind, route := "mods", "mod"
	if p.Type == "modpack" {
		kind, route = "modpacks", "modpack"
	}
	if p.Type == "resourcepack" {
		kind, route = "resources", "resourcepack"
	}
	if p.Type == "shader" {
		kind, route = "shaders", "shader"
	}
	preview := p.IconURL
	for i, g := range p.Gallery {
		if i == 0 || g.Featured {
			preview = g.URL
			if g.Featured {
				break
			}
		}
	}
	return Project{Downloads: p.Downloads, Kind: kind, Preview: preview, Tags: p.Categories, ID: p.ID, Provider: "modrinth", Slug: p.Slug, Name: p.Title, Description: p.Description, Icon: p.IconURL, URL: "https://modrinth.com/" + route + "/" + p.Slug, Games: Games(p.GameVersions), Loaders: Loaders(p.Loaders)}
}

type cfFile struct {
	ReleaseType  int `json:"releaseType"`
	ID           int
	ModID        int      `json:"modId"`
	DisplayName  string   `json:"displayName"`
	FileName     string   `json:"fileName"`
	DownloadURL  string   `json:"downloadUrl"`
	FileLength   int64    `json:"fileLength"`
	GameVersions []string `json:"gameVersions"`
	Hashes       []struct {
		Value string
		Algo  int
	}
	Dependencies []struct {
		ModID int `json:"modId"`
		Type  int `json:"relationType"`
	}
}

func (f cfFile) version() Version {
	v := Version{ID: strconv.Itoa(f.ID), Project: strconv.Itoa(f.ModID), Name: f.DisplayName, Filename: f.FileName, URL: f.DownloadURL, Size: f.FileLength, Games: Games(f.GameVersions), Loaders: Loaders(f.GameVersions), Dependencies: []Dependency{}}
	v.Release = map[int]string{1: "release", 2: "beta", 3: "alpha"}[f.ReleaseType]
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			v.Hash = h.Value
			v.Algorithm = "sha1"
			break
		}
		if h.Algo == 2 {
			v.Hash = h.Value
			v.Algorithm = "md5"
		}
	}
	for _, d := range f.Dependencies {
		kind := "optional"
		if d.Type == 3 {
			kind = "required"
		}
		if d.Type == 5 {
			kind = "incompatible"
		}
		v.Dependencies = append(v.Dependencies, Dependency{Project: strconv.Itoa(d.ModID), Kind: kind})
	}
	return v
}

type cfProject struct {
	Downloads  float64 `json:"downloadCount"`
	ClassID    int     `json:"classId"`
	Categories []struct {
		ID   int
		Slug string
	}
	Screenshots []struct {
		URL       string
		Thumbnail string `json:"thumbnailUrl"`
	}
	ID                        int
	Name, Summary, Slug       string
	DateCreated, DateModified string
	Logo                      struct{ URL string }
	Authors                   []struct{ Name string }
	Links                     struct {
		WebsiteURL string `json:"websiteUrl"`
	}
	Indexes []struct {
		GameVersion string `json:"gameVersion"`
		Loader      int    `json:"modLoader"`
	} `json:"latestFilesIndexes"`
	Latest []cfFile `json:"latestFiles"`
}

func (p cfProject) project() Project {
	games, loaders := []string{}, []string{}
	for _, i := range p.Indexes {
		games = append(games, i.GameVersion)
		loaders = append(loaders, map[int]string{1: "forge", 4: "fabric", 5: "quilt", 6: "neoforge"}[i.Loader])
	}
	for _, f := range p.Latest {
		games = append(games, f.GameVersions...)
		loaders = append(loaders, f.GameVersions...)
	}
	authors := []string{}
	for _, a := range p.Authors {
		authors = append(authors, a.Name)
	}
	kind := "mods"
	if p.ClassID == 4471 {
		kind = "modpacks"
	}
	if p.ClassID == 12 {
		kind = "resources"
	}
	if p.ClassID == 6552 {
		kind = "shaders"
	}
	preview := p.Logo.URL
	if len(p.Screenshots) > 0 {
		preview = p.Screenshots[0].Thumbnail
		if preview == "" {
			preview = p.Screenshots[0].URL
		}
	}
	tags := []string{}
	for _, category := range p.Categories {
		tags = append(tags, category.Slug)
	}
	return Project{Downloads: int64(p.Downloads), Kind: kind, Preview: preview, Tags: tags, ID: strconv.Itoa(p.ID), Provider: "curseforge", Slug: p.Slug, CreatedAt: p.DateCreated, UpdatedAt: p.DateModified, Name: p.Name, Description: p.Summary, Icon: p.Logo.URL, Author: strings.Join(authors, ", "), URL: p.Links.WebsiteURL, Games: Games(games), Loaders: Loaders(loaders)}
}
func (c *Client) Search(ctx context.Context, provider, query string, offset int) (SearchResult, error) {
	return c.SearchFiltered(ctx, provider, query, offset, SearchFilters{})
}
func (c *Client) SearchFiltered(ctx context.Context, provider, query string, offset int, filters SearchFilters) (SearchResult, error) {
	result := SearchResult{Items: []Project{}}
	if err := filters.validate(); err != nil {
		return result, err
	}
	if provider == "sintedmods" {
		q := url.Values{"kind": {filters.Kind}, "q": {query}, "offset": {strconv.Itoa(offset)}, "game": {filters.Game}, "loader": {filters.Loader}, "topic": {filters.Topic}, "sort": {filters.Sort}, "resolution": {filters.Resolution}}
		if filters.Kind == "" {
			q.Set("kind", "mods")
		}
		if offset < 0 || offset > 9980 || len(query) > 200 {
			return result, errors.New("неверный поиск")
		}
		err := c.get(ctx, provider, "/projects?"+q.Encode(), &result)
		return result, err
	}
	topic := filters.topic()
	topicQuery := topic.Query
	if provider == "curseforge" && topic.CurseQuery != "" {
		topicQuery = topic.CurseQuery
	}
	if topicQuery != "" {
		query = strings.TrimSpace(query + " " + topicQuery)
	}
	if offset < 0 || offset > 9980 || len(query) > 200 {
		return result, errors.New("неверный запрос поиска")
	}
	q := url.Values{"query": {query}, "limit": {"20"}, "offset": {strconv.Itoa(offset)}, "facets": {`[["project_type:mod"]]`}, "index": {"downloads"}}
	if provider == "modrinth" {
		facets := [][]string{{"project_type:mod"}}
		if filters.Kind == "modpacks" {
			facets = [][]string{{"project_type:modpack"}}
		}
		if filters.Kind == "resources" {
			facets = [][]string{{"project_type:resourcepack"}}
		}
		if filters.Kind == "shaders" {
			facets = [][]string{{"project_type:shader"}}
		}
		if filters.Game != "" {
			facets = append(facets, []string{"versions:" + filters.Game})
		}
		if filters.Loader != "" {
			facets = append(facets, []string{"categories:" + filters.Loader})
		}
		if topic.Modrinth != "" {
			facets = append(facets, []string{"categories:" + topic.Modrinth})
		}
		if filters.Resolution != "" {
			facets = append(facets, []string{"categories:" + filters.Resolution})
		}
		encoded, _ := json.Marshal(facets)
		q.Set("facets", string(encoded))
		if filters.Sort != "" {
			q.Set("index", map[string]string{"relevance": "relevance", "downloads": "downloads", "updated": "updated", "newest": "newest"}[filters.Sort])
		}
		var response struct {
			Hits []struct {
				Downloads                        int64  `json:"downloads"`
				ProjectID                        string `json:"project_id"`
				Title, Description, Author, Slug string
				Icon                             string `json:"icon_url"`
				CreatedAt                        string `json:"date_created"`
				UpdatedAt                        string `json:"date_modified"`
				Versions, Categories             []string
				Gallery                          []string
			}
			Total int `json:"total_hits"`
		}
		if err := c.get(ctx, provider, "/search?"+q.Encode(), &response); err != nil {
			return result, err
		}
		for _, hit := range response.Hits {
			kind, route := "mods", "mod"
			if filters.Kind == "modpacks" {
				kind, route = "modpacks", "modpack"
			}
			if filters.Kind == "resources" {
				kind, route = "resources", "resourcepack"
			}
			if filters.Kind == "shaders" {
				kind, route = "shaders", "shader"
			}
			preview := hit.Icon
			if len(hit.Gallery) > 0 {
				preview = hit.Gallery[0]
			}
			result.Items = append(result.Items, Project{Downloads: hit.Downloads, Kind: kind, Tags: hit.Categories, Preview: preview, ID: hit.ProjectID, Provider: provider, Slug: hit.Slug, CreatedAt: hit.CreatedAt, UpdatedAt: hit.UpdatedAt, Name: hit.Title, Description: hit.Description, Author: hit.Author, Icon: hit.Icon, Games: Games(hit.Versions), Loaders: Loaders(hit.Categories), URL: "https://modrinth.com/" + route + "/" + hit.Slug})
		}
		result.Total = response.Total
		result.NextOffset = offset + len(response.Hits)
	} else {
		q = url.Values{"gameId": {"432"}, "classId": {"6"}, "searchFilter": {query}, "index": {strconv.Itoa(offset)}, "pageSize": {"20"}, "sortField": {"6"}, "sortOrder": {"desc"}}
		class := "6"
		if filters.Kind == "modpacks" {
			class = "4471"
			q.Set("classId", class)
		}
		if filters.Kind == "resources" {
			class = "12"
			q.Set("classId", class)
		}
		if filters.Kind == "shaders" {
			class = "6552"
			q.Set("classId", class)
		}
		if filters.Game != "" {
			q.Set("gameVersion", filters.Game)
		}
		if filters.Loader != "" {
			q.Set("modLoaderType", map[string]string{"forge": "1", "fabric": "4", "quilt": "5", "neoforge": "6"}[filters.Loader])
		}
		categoryIDs := []int{}
		if topic.CurseForge != "" {
			id, err := c.curseCategoryFor(ctx, topic.CurseForge, class)
			if err != nil {
				return result, err
			}
			q.Set("categoryId", strconv.Itoa(id))
			categoryIDs = append(categoryIDs, id)
		}
		if filters.Resolution != "" {
			id, err := c.curseCategoryFor(ctx, curseResolutions[filters.Resolution], class)
			if err != nil {
				return result, err
			}
			categoryIDs = append(categoryIDs, id)
			q.Set("categoryId", strconv.Itoa(id))
			if len(categoryIDs) > 1 {
				encoded, _ := json.Marshal(categoryIDs)
				q.Set("categoryIds", string(encoded))
			}
		}
		if filters.Sort != "" {
			q.Set("sortField", map[string]string{"relevance": "1", "downloads": "6", "updated": "3", "newest": "11"}[filters.Sort])
		}
		var response struct {
			Data       []cfProject
			Pagination struct {
				Total int `json:"totalCount"`
			}
		}
		if err := c.get(ctx, provider, "/mods/search?"+q.Encode(), &response); err != nil {
			return result, err
		}
		for _, p := range response.Data {
			if filters.Kind == "resources" {
				skip := false
				for _, cat := range p.Categories {
					if cat.ID == 5193 {
						skip = true
					}
				}
				for _, id := range categoryIDs {
					found := false
					for _, cat := range p.Categories {
						if cat.ID == id {
							found = true
						}
					}
					if !found {
						skip = true
					}
				}
				if skip {
					continue
				}
			}
			item := p.project()
			if filters.Game != "" && !contains(item.Games, filters.Game) {
				item.Games = Games(append(item.Games, filters.Game))
			}
			if filters.Loader != "" && !contains(item.Loaders, filters.Loader) {
				item.Loaders = Loaders(append(item.Loaders, filters.Loader))
			}
			result.Items = append(result.Items, item)
		}
		result.Total = response.Pagination.Total
		result.NextOffset = offset + len(response.Data)
	}
	result.Next = result.NextOffset > offset && result.NextOffset < result.Total && result.NextOffset < 10000
	return result, nil
}
func (c *Client) Project(ctx context.Context, provider, id string) (Project, error) {
	if provider == "sintedmods" {
		if !safeID.MatchString(id) {
			return Project{}, errors.New("неверный ID проекта")
		}
		var item Project
		err := c.get(ctx, provider, "/projects/"+id, &item)
		return item, err
	}
	if !safeID.MatchString(id) {
		return Project{}, errors.New("неверный ID мода")
	}
	if provider == "modrinth" {
		var p mrProject
		err := c.get(ctx, provider, "/project/"+id, &p)
		return p.project(), err
	}
	var response struct{ Data cfProject }
	err := c.get(ctx, provider, "/mods/"+id, &response)
	return response.Data.project(), err
}

type mrVersion struct {
	Release      string `json:"version_type"`
	ID           string
	ProjectID    string `json:"project_id"`
	Name         string
	Games        []string `json:"game_versions"`
	Loaders      []string
	Dependencies []struct {
		Project string `json:"project_id"`
		Version string `json:"version_id"`
		Kind    string `json:"dependency_type"`
	}
	Files []struct {
		URL, Filename string
		Primary       bool
		Size          int64
		Hashes        map[string]string
	}
}

func (v mrVersion) version() Version {
	result := Version{Release: v.Release, ID: v.ID, Project: v.ProjectID, Name: v.Name, Games: Games(v.Games), Loaders: Loaders(v.Loaders), Dependencies: []Dependency{}}
	for _, f := range v.Files {
		if result.Filename == "" || f.Primary {
			result.Filename = f.Filename
			result.URL = f.URL
			result.Size = f.Size
			result.Algorithm = "sha512"
			result.Hash = f.Hashes["sha512"]
			if result.Hash == "" {
				result.Algorithm = "sha1"
				result.Hash = f.Hashes["sha1"]
			}
			if f.Primary {
				break
			}
		}
	}
	for _, d := range v.Dependencies {
		result.Dependencies = append(result.Dependencies, Dependency{Project: d.Project, Version: d.Version, Kind: d.Kind})
	}
	return result
}
func (c *Client) Versions(ctx context.Context, provider, id, game, loader string) ([]Version, error) {
	if provider == "sintedmods" {
		return c.nativeVersions(ctx, id, game, loader)
	}
	result := []Version{}
	if !safeID.MatchString(id) {
		return result, errors.New("неверный ID мода")
	}
	q := url.Values{}
	if provider == "modrinth" {
		if game != "" {
			data, _ := json.Marshal([]string{game})
			q.Set("game_versions", string(data))
		}
		if loader != "" {
			data, _ := json.Marshal([]string{loader})
			q.Set("loaders", string(data))
		}
		var rows []mrVersion
		if err := c.get(ctx, provider, "/project/"+id+"/version?"+q.Encode(), &rows); err != nil {
			return result, err
		}
		for _, v := range rows {
			result = append(result, v.version())
		}
		return result, nil
	}
	q.Set("pageSize", "50")
	if game != "" {
		q.Set("gameVersion", game)
	}
	if loader != "" {
		code := map[string]string{"forge": "1", "fabric": "4", "quilt": "5", "neoforge": "6"}[loader]
		if code == "" {
			return result, errors.New("неизвестный загрузчик")
		}
		q.Set("modLoaderType", code)
	}
	for offset := 0; offset < 10000; offset += 50 {
		q.Set("index", strconv.Itoa(offset))
		var response struct {
			Data       []cfFile
			Pagination struct {
				Total int `json:"totalCount"`
			}
		}
		if err := c.get(ctx, provider, "/mods/"+id+"/files?"+q.Encode(), &response); err != nil {
			return result, err
		}
		for _, v := range response.Data {
			result = append(result, v.version())
		}
		if len(response.Data) < 50 || offset+len(response.Data) >= response.Pagination.Total {
			break
		}
	}
	return result, nil
}
func (c *Client) Version(ctx context.Context, provider, project, id string) (Version, error) {
	if provider == "sintedmods" {
		if !safeID.MatchString(id) {
			return Version{}, errors.New("неверный ID файла")
		}
		var row nativeVersion
		err := c.get(ctx, provider, "/versions/"+id, &row)
		v := row.version()
		if project != "" && v.Project != project {
			return Version{}, errors.New("файл другого проекта")
		}
		return v, err
	}
	if !safeID.MatchString(id) {
		return Version{}, errors.New("неверный ID файла")
	}
	if provider == "modrinth" {
		var v mrVersion
		err := c.get(ctx, provider, "/version/"+id, &v)
		return v.version(), err
	}
	if !safeID.MatchString(project) {
		return Version{}, errors.New("неверный ID мода")
	}
	var response struct{ Data cfFile }
	err := c.get(ctx, provider, "/mods/"+project+"/files/"+id, &response)
	return response.Data.version(), err
}

type nativeVersion struct {
	Version
	DownloadURL string `json:"url"`
	Digest      string `json:"hash"`
	HashType    string `json:"algorithm"`
}

func (v nativeVersion) version() Version {
	result := v.Version
	result.URL = v.DownloadURL
	result.Hash = v.Digest
	result.Algorithm = v.HashType
	return result
}
func (c *Client) nativeVersions(ctx context.Context, id, game, loader string) ([]Version, error) {
	if !safeID.MatchString(id) {
		return nil, errors.New("неверный ID проекта")
	}
	var rows []nativeVersion
	q := url.Values{"game": {game}, "loader": {loader}}
	err := c.get(ctx, "sintedmods", "/projects/"+id+"/versions?"+q.Encode(), &rows)
	items := []Version{}
	for _, row := range rows {
		items = append(items, row.version())
	}
	return items, err
}
