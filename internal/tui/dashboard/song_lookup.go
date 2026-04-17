package dashboard

import (
	"fmt"
	"sort"
	"strings"

	"powerhour/internal/cache"
	"powerhour/internal/config"
	"powerhour/internal/project"
	"powerhour/pkg/csvplan"
)

type songSuggestion struct {
	Title  string
	Artist string
	Link   string
	score  int
}

func cacheFieldValues(entry cache.Entry, field string) []string {
	switch strings.TrimSpace(field) {
	case "title":
		return []string{entry.Title}
	case "artist":
		return []string{entry.Artist}
	case "album":
		return []string{entry.Album}
	case "track":
		return []string{entry.Track}
	case "uploader":
		return []string{entry.Uploader}
	case "channel":
		return []string{entry.Channel}
	case "upload_date":
		return []string{entry.UploadDate}
	case "description":
		return []string{entry.Description}
	case "source":
		return []string{entry.Source}
	case "links":
		return append([]string(nil), entry.Links...)
	case "identifier":
		return []string{entry.Identifier}
	case "id":
		return []string{entry.ID}
	case "extractor":
		return []string{entry.Extractor}
	case "cached_path":
		return []string{entry.CachedPath}
	default:
		return nil
	}
}

func firstConfiguredCacheValue(entry cache.Entry, fields []string) string {
	for _, field := range fields {
		for _, value := range cacheFieldValues(entry, field) {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func collectionHasField(coll project.Collection, field string) bool {
	field = strings.TrimSpace(field)
	if field == "" {
		return false
	}
	for _, header := range coll.Headers {
		if strings.TrimSpace(header) == field {
			return true
		}
	}
	_, ok := coll.Defaults[field]
	return ok
}

func findCachedEntryByLink(idx *cache.Index, link string) (cache.Entry, bool) {
	if idx == nil {
		return cache.Entry{}, false
	}
	identifier, ok := idx.LookupLink(link)
	if !ok {
		return cache.Entry{}, false
	}
	entry, ok := idx.GetByIdentifier(identifier)
	return entry, ok
}

func findCachedSongByLink(idx *cache.Index, link string, profile config.CacheSearchProfile) (songSuggestion, bool) {
	entry, ok := findCachedEntryByLink(idx, link)
	if !ok {
		return songSuggestion{}, false
	}
	return suggestionFromEntry(entry, profile)
}

func searchCachedSongs(idx *cache.Index, query string, profile config.CacheSearchProfile, limit int) []songSuggestion {
	if idx == nil || limit <= 0 {
		return nil
	}
	query = normalizeSearchText(query)
	if query == "" {
		return nil
	}

	results := make([]songSuggestion, 0, limit)
	for _, entry := range idx.Entries {
		suggestion, ok := suggestionFromEntry(entry, profile)
		if !ok {
			continue
		}
		score := scoreSuggestion(query, entry, profile, suggestion)
		if score <= 0 {
			continue
		}
		suggestion.score = score
		results = append(results, suggestion)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		if results[i].Title != results[j].Title {
			return results[i].Title < results[j].Title
		}
		if results[i].Artist != results[j].Artist {
			return results[i].Artist < results[j].Artist
		}
		return results[i].Link < results[j].Link
	})

	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func suggestionFromEntry(entry cache.Entry, profile config.CacheSearchProfile) (songSuggestion, bool) {
	title := firstConfiguredCacheValue(entry, profile.Fill.TitleFields)
	artist := firstConfiguredCacheValue(entry, profile.Fill.ArtistFields)
	link := firstConfiguredCacheValue(entry, profile.Fill.LinkFields)
	if link == "" {
		link = firstConfiguredCacheValue(entry, []string{"source", "links"})
	}
	if title == "" && artist == "" {
		return songSuggestion{}, false
	}
	return songSuggestion{
		Title:  strings.TrimSpace(title),
		Artist: strings.TrimSpace(artist),
		Link:   strings.TrimSpace(link),
	}, true
}

func scoreSuggestion(query string, entry cache.Entry, profile config.CacheSearchProfile, suggestion songSuggestion) int {
	score := 0
	combined := normalizeSearchText(strings.TrimSpace(suggestion.Title + " " + suggestion.Artist))
	for _, field := range profile.SearchFields {
		for _, raw := range cacheFieldValues(entry, field) {
			value := normalizeSearchText(raw)
			if value == "" {
				continue
			}
			switch {
			case value == query:
				score += 140
			case strings.HasPrefix(value, query):
				score += 90
			case strings.Contains(value, query):
				score += 60
			}
			if value != "" && fuzzyContains(value, query) {
				score += 20
			}
		}
	}
	if combined != "" {
		if strings.Contains(combined, query) {
			score += 45
		}
		allTokens := true
		for _, token := range strings.Fields(query) {
			if !strings.Contains(combined, token) {
				allTokens = false
				break
			}
		}
		if allTokens {
			score += 35
		}
	}
	return score
}

func fuzzyContains(text, query string) bool {
	if query == "" {
		return false
	}
	tr := []rune(text)
	ti := 0
	for _, qr := range query {
		found := false
		for ti < len(tr) {
			if tr[ti] == qr {
				found = true
				ti++
				break
			}
			ti++
		}
		if !found {
			return false
		}
	}
	return true
}

func normalizeSearchText(value string) string {
	var b strings.Builder
	lastSpace := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func collectionLinkHeader(coll project.Collection) string {
	header := strings.TrimSpace(coll.Config.LinkHeader)
	if header == "" {
		header = "link"
	}
	return header
}

func applySuggestionToRow(coll project.Collection, row *csvplan.CollectionRow, suggestion songSuggestion) {
	if row == nil {
		return
	}
	if row.CustomFields == nil {
		row.CustomFields = make(map[string]string)
	}
	if suggestion.Title != "" && collectionHasField(coll, "title") {
		row.CustomFields["title"] = suggestion.Title
	}
	if suggestion.Artist != "" && collectionHasField(coll, "artist") {
		row.CustomFields["artist"] = suggestion.Artist
	}
	if suggestion.Link != "" {
		row.Link = suggestion.Link
		row.CustomFields[collectionLinkHeader(coll)] = suggestion.Link
	}
}

func formatSuggestionHint(prefix string, suggestions []songSuggestion) string {
	if len(suggestions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(suggestions))
	for _, suggestion := range suggestions {
		label := strings.TrimSpace(strings.TrimSpace(suggestion.Title) + " - " + strings.TrimSpace(suggestion.Artist))
		label = strings.Trim(label, " -")
		if label == "" {
			label = suggestion.Link
		}
		parts = append(parts, label)
	}
	return fmt.Sprintf("%s %s", prefix, strings.Join(parts, " | "))
}

func bestCachedSongMatch(idx *cache.Index, query string, profile config.CacheSearchProfile) (songSuggestion, bool) {
	suggestions := searchCachedSongs(idx, query, profile, 1)
	if len(suggestions) == 0 {
		return songSuggestion{}, false
	}
	return suggestions[0], true
}
