package cache

import (
	"strings"
	"unicode"

	"powerhour/internal/tools"
)

type NormalizationConfig struct {
	ArtistAliases map[string]string
}

type NormalizationInput struct {
	Title    string
	Artist   string
	Track    string
	Album    string
	Uploader string
	Channel  string
}

type NormalizationResult struct {
	Title          string
	Artist         string
	Confidence     string
	Reasons        []string
	AppliedAlias   bool
	AliasSource    string
	AliasCandidate string
}

func LoadNormalizationConfig() NormalizationConfig {
	meta := tools.LoadMetadataNormalizationConfig()
	return NormalizationConfig{ArtistAliases: meta.ArtistAliases}
}

func SaveArtistAlias(raw, canonical string) error {
	meta := tools.LoadMetadataNormalizationConfig()
	if meta.ArtistAliases == nil {
		meta.ArtistAliases = map[string]string{}
	}
	key := aliasKey(raw)
	if key != "" && strings.TrimSpace(canonical) != "" {
		meta.ArtistAliases[key] = strings.TrimSpace(canonical)
	}
	return tools.SaveMetadataNormalizationConfig(meta)
}

func NormalizeMetadata(cfg NormalizationConfig, in NormalizationInput) NormalizationResult {
	title := strings.TrimSpace(in.Title)
	artist := strings.TrimSpace(in.Artist)
	track := strings.TrimSpace(in.Track)
	uploader := strings.TrimSpace(in.Uploader)
	channel := strings.TrimSpace(in.Channel)

	res := NormalizationResult{
		Title:      title,
		Artist:     artist,
		Confidence: "low",
	}

	if track != "" {
		if res.Title == "" || looksDecoratedTitle(res.Title) {
			res.Title = track
			res.Reasons = append(res.Reasons, "used track as title")
			res.Confidence = "high"
		}
	}

	if alias, src := lookupArtistAlias(cfg, artist, uploader, channel); alias != "" {
		res.Artist = alias
		res.AppliedAlias = true
		res.AliasSource = src
		res.Reasons = append(res.Reasons, "applied artist alias")
		res.Confidence = "high"
	}

	if res.Artist == "" {
		if splitArtist, splitTitle, ok := splitArtistTitle(res.Title, cfg); ok {
			res.Artist = splitArtist
			res.Title = splitTitle
			res.Reasons = append(res.Reasons, "split artist/title from title field")
			res.Confidence = "high"
		}
	}

	if res.Artist == "" {
		if alias, src := lookupArtistAlias(cfg, uploader, channel); alias != "" {
			res.Artist = alias
			res.AppliedAlias = true
			res.AliasSource = src
			res.Reasons = append(res.Reasons, "mapped uploader/channel to artist alias")
			res.Confidence = "high"
		}
	}

	if cleaned := cleanVideoTitle(res.Title); cleaned != res.Title {
		res.Title = cleaned
		res.Reasons = append(res.Reasons, "removed video suffix noise")
		if res.Confidence == "low" {
			res.Confidence = "medium"
		}
	}

	if splitArtist, splitTitle, ok := splitArtistTitle(res.Title, cfg); ok && aliasKey(splitArtist) == aliasKey(res.Artist) {
		res.Title = splitTitle
		res.Reasons = append(res.Reasons, "removed repeated artist from title")
		if res.Confidence == "low" {
			res.Confidence = "medium"
		}
	}

	if res.Artist == "" {
		switch {
		case uploader != "" && !looksHandleLike(uploader) && !looksTitleLike(uploader):
			res.Artist = uploader
			res.Reasons = append(res.Reasons, "fell back to uploader")
			res.Confidence = "medium"
		case channel != "" && !looksHandleLike(channel) && !looksTitleLike(channel):
			res.Artist = channel
			res.Reasons = append(res.Reasons, "fell back to channel")
			res.Confidence = "medium"
		}
	}

	if res.Artist == "" {
		if candidate := bestAliasCandidate(artist, uploader, channel); candidate != "" {
			res.AliasCandidate = candidate
		}
	}

	res.Title = strings.TrimSpace(res.Title)
	res.Artist = strings.TrimSpace(res.Artist)
	return res
}

func lookupArtistAlias(cfg NormalizationConfig, values ...string) (string, string) {
	for _, value := range values {
		key := aliasKey(value)
		if key == "" {
			continue
		}
		if alias := strings.TrimSpace(cfg.ArtistAliases[key]); alias != "" {
			return alias, strings.TrimSpace(value)
		}
	}
	return "", ""
}

func aliasKey(value string) string {
	if value == "" {
		return ""
	}
	fields := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(value)), func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
	return strings.Join(fields, "")
}

func fingerprintArtist(value string) string {
	return aliasKey(value)
}

func splitArtistTitle(title string, cfg NormalizationConfig) (string, string, bool) {
	for _, sep := range []string{" - ", " – ", " — "} {
		if !strings.Contains(title, sep) {
			continue
		}
		parts := strings.SplitN(title, sep, 2)
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		if left == "" || right == "" {
			continue
		}
		if alias, _ := lookupArtistAlias(cfg, left); alias != "" {
			return alias, cleanVideoTitle(right), true
		}
		if !looksHandleLike(left) && !looksTitleLike(left) {
			return left, cleanVideoTitle(right), true
		}
	}
	return "", "", false
}

func cleanVideoTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	suffixes := []string{
		"(official video)",
		"[official video]",
		"(official music video)",
		"[official music video]",
		"(music video)",
		"[music video]",
		"(official lyric video)",
		"[official lyric video]",
		"(lyric video)",
		"[lyric video]",
		"(lyrics)",
		"[lyrics]",
		"(visualizer)",
		"[visualizer]",
		"(official visualizer)",
		"[official visualizer]",
		"(audio)",
		"[audio]",
		"(explicit)",
		"[explicit]",
		"(hd)",
		"[hd]",
	}
	for {
		lower := strings.ToLower(strings.TrimSpace(title))
		changed := false
		for _, suffix := range suffixes {
			if strings.HasSuffix(lower, suffix) {
				title = strings.TrimSpace(title[:len(title)-len(suffix)])
				changed = true
				break
			}
		}
		if !changed {
			break
		}
	}
	return strings.TrimSpace(title)
}

func looksDecoratedTitle(title string) bool {
	lower := strings.ToLower(title)
	return strings.Contains(lower, "official") || strings.Contains(lower, "visualizer") || strings.Contains(lower, "lyrics")
}

func looksHandleLike(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "@") || strings.Contains(lower, "vevo") || strings.Contains(lower, "topic")
}

func looksTitleLike(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(lower, "official video") || strings.Contains(lower, "lyrics") || strings.Contains(lower, " - ")
}

func bestAliasCandidate(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
