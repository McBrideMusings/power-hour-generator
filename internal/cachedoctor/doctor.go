package cachedoctor

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"powerhour/internal/cache"
)

type Finding struct {
	Identifier     string   `json:"identifier"`
	File           string   `json:"file"`
	Source         string   `json:"source"`
	CurrentTitle   string   `json:"current_title"`
	CurrentArtist  string   `json:"current_artist"`
	ProposedTitle  string   `json:"proposed_title"`
	ProposedArtist string   `json:"proposed_artist"`
	Confidence     string   `json:"confidence"`
	Reasons        []string `json:"reasons,omitempty"`
	AliasSource    string   `json:"alias_source,omitempty"`
	AliasCandidate string   `json:"alias_candidate,omitempty"`
	SimilarArtist  string   `json:"similar_artist,omitempty"`
	NeedsAttention bool     `json:"needs_attention"`
}

func InspectEntry(ctx context.Context, svc interface {
	QueryRemoteID(context.Context, string) (cache.RemoteIDInfo, error)
}, normCfg cache.NormalizationConfig, knownArtists []string, entry cache.Entry, requery bool) (Finding, bool, error) {
	input := cache.NormalizationInput{
		Title:    entry.Title,
		Artist:   entry.Artist,
		Track:    entry.Track,
		Album:    entry.Album,
		Uploader: entry.Uploader,
		Channel:  entry.Channel,
	}

	if requery && svc != nil && entry.SourceType == cache.SourceTypeURL {
		link := entry.Source
		if link == "" && len(entry.Links) > 0 {
			link = entry.Links[0]
		}
		if link != "" {
			info, err := svc.QueryRemoteID(ctx, link)
			if err != nil {
				return Finding{}, false, fmt.Errorf("requery %s: %w", link, err)
			}
			input = cache.NormalizationInput{
				Title:    firstNonEmpty(info.Title, entry.Title),
				Artist:   firstNonEmpty(info.Artist, entry.Artist),
				Track:    firstNonEmpty(info.Track, entry.Track),
				Album:    firstNonEmpty(info.Album, entry.Album),
				Uploader: firstNonEmpty(info.Uploader, entry.Uploader),
				Channel:  firstNonEmpty(info.Channel, entry.Channel),
			}
		}
	}

	result := cache.NormalizeMetadata(normCfg, input)
	similar := closestKnownArtist(result.Artist, knownArtists)
	needsAttention := strings.TrimSpace(entry.Title) == "" ||
		strings.TrimSpace(entry.Artist) == "" ||
		result.Title != strings.TrimSpace(entry.Title) ||
		result.Artist != strings.TrimSpace(entry.Artist) ||
		(similar != "" && !strings.EqualFold(similar, result.Artist))

	if !needsAttention {
		return Finding{}, false, nil
	}

	return Finding{
		Identifier:     entry.Identifier,
		File:           filepath.Base(entry.CachedPath),
		Source:         entry.Source,
		CurrentTitle:   entry.Title,
		CurrentArtist:  entry.Artist,
		ProposedTitle:  result.Title,
		ProposedArtist: result.Artist,
		Confidence:     result.Confidence,
		Reasons:        append([]string(nil), result.Reasons...),
		AliasSource:    result.AliasSource,
		AliasCandidate: firstNonEmpty(result.AliasCandidate, result.AliasSource, entry.Artist, entry.Uploader, entry.Channel),
		SimilarArtist:  similar,
		NeedsAttention: needsAttention,
	}, true, nil
}

func ApplyFinding(idx *cache.Index, finding Finding) error {
	entry, ok := idx.GetByIdentifier(finding.Identifier)
	if !ok {
		return fmt.Errorf("cache entry not found: %s", finding.Identifier)
	}
	entry.Title = strings.TrimSpace(finding.ProposedTitle)
	entry.Artist = strings.TrimSpace(finding.ProposedArtist)
	idx.SetEntry(entry)
	return nil
}

func ApplyAliasAcrossIndex(idx *cache.Index, normCfg cache.NormalizationConfig, aliasCandidate string) {
	matchKey := normalizedAliasMatch(aliasCandidate)
	for _, entry := range SortedEntries(idx) {
		values := []string{entry.Artist, entry.Uploader, entry.Channel}
		matches := false
		for _, value := range values {
			if normalizedAliasMatch(value) == matchKey {
				matches = true
				break
			}
		}
		if !matches {
			continue
		}
		result := cache.NormalizeMetadata(normCfg, cache.NormalizationInput{
			Title:    entry.Title,
			Artist:   entry.Artist,
			Track:    entry.Track,
			Album:    entry.Album,
			Uploader: entry.Uploader,
			Channel:  entry.Channel,
		})
		entry.Title = result.Title
		entry.Artist = result.Artist
		idx.SetEntry(entry)
	}
}

func BuildKnownArtists(idx *cache.Index, normCfg cache.NormalizationConfig) []string {
	seen := map[string]bool{}
	var artists []string
	for _, entry := range idx.Entries {
		if artist := strings.TrimSpace(entry.Artist); artist != "" && !seen[artist] {
			artists = append(artists, artist)
			seen[artist] = true
		}
	}
	for _, artist := range normCfg.ArtistAliases {
		artist = strings.TrimSpace(artist)
		if artist != "" && !seen[artist] {
			artists = append(artists, artist)
			seen[artist] = true
		}
	}
	slices.Sort(artists)
	return artists
}

func SortedEntries(idx *cache.Index) []cache.Entry {
	entries := make([]cache.Entry, 0, len(idx.Entries))
	for _, entry := range idx.Entries {
		entries = append(entries, entry)
	}
	slices.SortFunc(entries, func(a, b cache.Entry) int {
		return strings.Compare(a.Identifier, b.Identifier)
	})
	return entries
}

func MatchesArtistFilter(finding Finding, filter string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	return strings.Contains(strings.ToLower(finding.CurrentArtist), filter) ||
		strings.Contains(strings.ToLower(finding.ProposedArtist), filter)
}

func DisplayBlank(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func closestKnownArtist(candidate string, known []string) string {
	key := normalizedAliasMatch(candidate)
	if key == "" {
		return ""
	}
	for _, artist := range known {
		artistKey := normalizedAliasMatch(artist)
		if artistKey == key {
			return artist
		}
		if strings.Contains(artistKey, key) || strings.Contains(key, artistKey) {
			return artist
		}
	}
	return ""
}

func normalizedAliasMatch(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
