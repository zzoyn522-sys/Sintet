package workshop

import (
	"context"
	"errors"
	"strings"
)

type SearchFilters struct {
	Kind       string `json:"kind"`
	Resolution string `json:"resolution"`
	Game       string `json:"game"`
	Loader     string `json:"loader"`
	Topic      string `json:"topic"`
	Sort       string `json:"sort"`
}
type Topic struct {
	CurseQuery string `json:"-"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Modrinth   string `json:"-"`
	CurseForge string `json:"-"`
	Query      string `json:"-"`
}

var Topics = []Topic{
	{ID: "pvp", Name: "PvP", Query: "pvp"},
	{ID: "survival", Name: "Выживание", Query: "survival"},
	{ID: "optimization", Name: "Оптимизация", Modrinth: "optimization", CurseForge: "performance"},
	{ID: "adventure", Name: "Приключения", Modrinth: "adventure", CurseForge: "adventure-rpg"},
	{ID: "technology", Name: "Технологии", Modrinth: "technology", CurseForge: "technology"},
	{ID: "magic", Name: "Магия", Modrinth: "magic", CurseForge: "magic"},
	{ID: "worldgen", Name: "Генерация мира", Modrinth: "worldgen", CurseForge: "world-gen"},
	{ID: "mobs", Name: "Мобы", Modrinth: "mobs", CurseForge: "world-mobs"},
	{ID: "equipment", Name: "Оружие и броня", Modrinth: "equipment", CurseForge: "armor-weapons-tools"},
	{ID: "decoration", Name: "Строительство", Modrinth: "decoration", CurseForge: "cosmetic"},
	{ID: "food", Name: "Еда и фермы", Modrinth: "food", CurseForge: "food"},
	{ID: "storage", Name: "Хранение", Modrinth: "storage", CurseForge: "storage"},
	{ID: "transportation", Name: "Транспорт", Modrinth: "transportation", CurseForge: "technology-player-transport"},
	{ID: "utility", Name: "Удобство", Modrinth: "utility", CurseForge: "utility-qol"},
	{ID: "library", Name: "Библиотеки", Modrinth: "library", CurseForge: "library-api"},
	{ID: "maps", Name: "Карты и навигация", Modrinth: "utility", CurseForge: "map-information"},
	{ID: "farming", Name: "Фермерство", Modrinth: "food", CurseForge: "technology-farming"},
	{ID: "automation", Name: "Автоматизация", Modrinth: "technology", CurseForge: "technology-automation"},
	{ID: "dimensions", Name: "Измерения", Modrinth: "worldgen", CurseForge: "world-dimensions"},
	{ID: "horror", Name: "Хоррор", Query: "horror"},
	{ID: "skyblock", Name: "Skyblock", Query: "skyblock"},
}

func (f SearchFilters) validate() error {
	if f.Kind != "" && f.Kind != "mods" && !isZipPack(f.Kind) && f.Kind != "modpacks" {
		return errors.New("неизвестный каталог")
	}
	if f.Resolution != "" && (f.Kind != "resources" || !contains([]string{"16x", "32x", "64x", "128x", "256x", "512x+"}, f.Resolution)) {
		return errors.New("неверное разрешение текстур")
	}
	if f.Game != "" && !gameVersion.MatchString(f.Game) {
		return errors.New("неверная версия Minecraft")
	}
	if f.Loader != "" && !contains([]string{"fabric", "forge", "neoforge", "quilt"}, f.Loader) {
		return errors.New("неверный загрузчик")
	}
	if !contains([]string{"", "relevance", "downloads", "updated", "newest"}, f.Sort) {
		return errors.New("неверная сортировка")
	}
	if f.Topic != "" {
		for _, t := range f.topics() {
			if t.ID == f.Topic {
				return nil
			}
		}
		return errors.New("неизвестная тематика")
	}
	return nil
}
func (f SearchFilters) topic() Topic {
	for _, topic := range f.topics() {
		if topic.ID == f.Topic {
			return topic
		}
	}
	return Topic{}
}
func (c *Client) curseCategory(ctx context.Context, slug string) (int, error) {
	return c.curseCategoryFor(ctx, slug, "6")
}
func (c *Client) curseCategoryFor(ctx context.Context, slug, class string) (int, error) {
	c.categoryMu.Lock()
	defer c.categoryMu.Unlock()
	if c.categoryIDs != nil && c.categoryIDs["@"+class] == 1 {
		if id, ok := c.categoryIDs[class+":"+slug]; ok {
			return id, nil
		}
		return 0, errors.New("эта тематика пока недоступна в CurseForge")
	}
	var response struct {
		Data []struct {
			ID         int
			Name, Slug string
		}
	}
	if err := c.get(ctx, "curseforge", "/categories?gameId=432&classId="+class, &response); err != nil {
		return 0, err
	}
	if c.categoryIDs == nil {
		c.categoryIDs = map[string]int{}
	}
	c.categoryIDs["@"+class] = 1
	for _, category := range response.Data {
		c.categoryIDs[class+":"+category.Slug] = category.ID
		c.categoryIDs[class+":"+strings.TrimPrefix(category.Slug, "mc-")] = category.ID
	}
	if id, ok := c.categoryIDs[class+":"+slug]; ok {
		return id, nil
	}
	return 0, errors.New("эта тематика пока недоступна в CurseForge")
}

var ResourceTopics = []Topic{
	{ID: "pvp", Name: "PvP", Modrinth: "combat", CurseQuery: "pvp"},
	{ID: "vanilla", Name: "В стиле Vanilla", Modrinth: "vanilla-like", CurseForge: "traditional"},
	{ID: "realistic", Name: "Реалистичные", Modrinth: "realistic", CurseForge: "photo-realistic"},
	{ID: "simplistic", Name: "Минималистичные", Modrinth: "simplistic", CurseQuery: "simple"},
	{ID: "medieval", Name: "Средневековые", Query: "medieval", CurseForge: "medieval"},
	{ID: "modern", Name: "Современные", Query: "modern", CurseForge: "modern"},
	{ID: "animated", Name: "Анимированные", Query: "animated", CurseForge: "animated"},
	{ID: "gui", Name: "Интерфейс", Modrinth: "gui", CurseQuery: "gui"},
	{ID: "fonts", Name: "Шрифты", Modrinth: "fonts", CurseForge: "font-packs"},
	{ID: "audio", Name: "Звуки", Modrinth: "audio", CurseQuery: "sound"},
	{ID: "modded", Name: "Для модов", Modrinth: "modded", CurseForge: "mod-support"},
	{ID: "models", Name: "3D-модели", Modrinth: "models", CurseQuery: "models"},
}

var PackTopics = []Topic{
	{ID: "adventure", Name: "Приключения", Modrinth: "adventure", CurseQuery: "adventure"},
	{ID: "optimization", Name: "Оптимизация", Modrinth: "optimization", CurseQuery: "performance"},
	{ID: "technology", Name: "Технологии", Modrinth: "technology", CurseQuery: "tech"},
	{ID: "magic", Name: "Магия", Modrinth: "magic", CurseQuery: "magic"},
	{ID: "quests", Name: "Квесты", Modrinth: "quests", CurseQuery: "quests"},
	{ID: "lightweight", Name: "Лёгкие сборки", Modrinth: "lightweight", CurseQuery: "lightweight"},
	{ID: "multiplayer", Name: "Для друзей", Modrinth: "multiplayer", CurseQuery: "multiplayer"},
	{ID: "skyblock", Name: "Skyblock", Query: "skyblock"},
	{ID: "horror", Name: "Хоррор", Query: "horror"},
	{ID: "survival", Name: "Выживание", Query: "survival"},
}

func (f SearchFilters) topics() []Topic {
	if f.Kind == "shaders" {
		return nil
	}
	if f.Kind == "modpacks" {
		return PackTopics
	}
	if f.Kind == "resources" {
		return ResourceTopics
	}
	return Topics
}

var curseResolutions = map[string]string{"16x": "sixteen-x", "32x": "thirty-two-x", "64x": "sixty-four-x", "128x": "one-twenty-eight-x", "256x": "two-fifty-six-x", "512x+": "five-twelve-x-and-beyond"}
