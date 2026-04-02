package dashboard

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"powerhour/internal/tools"
)

// probeMetadata runs yt-dlp --dump-json to extract video metadata for a URL.
// Returns a tea.Cmd that sends a metadataProbeMsg when complete.
func probeMetadata(url string, collectionIdx int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		ytPath, err := tools.Lookup("yt-dlp")
		if err != nil {
			return metadataProbeMsg{collectionIdx: collectionIdx, link: url, err: err}
		}

		cmd := exec.CommandContext(ctx, ytPath,
			"--no-playlist",
			"--no-progress",
			"--skip-download",
			"--dump-json",
			"--no-warnings",
			"--no-color",
			url,
		)

		out, err := cmd.Output()
		if err != nil {
			return metadataProbeMsg{collectionIdx: collectionIdx, link: url, err: err}
		}

		var payload struct {
			Title    string `json:"title"`
			Artist   string `json:"artist"`
			Track    string `json:"track"`
			Uploader string `json:"uploader"`
			Channel  string `json:"channel"`
		}
		if err := json.Unmarshal(out, &payload); err != nil {
			return metadataProbeMsg{collectionIdx: collectionIdx, link: url, err: err}
		}

		title := strings.TrimSpace(payload.Title)
		artist := strings.TrimSpace(payload.Artist)
		if artist == "" {
			artist = strings.TrimSpace(payload.Track)
		}
		// yt-dlp often puts "Artist - Title" in the title field.
		// If artist is empty but title contains " - ", try to split.
		if artist == "" && strings.Contains(title, " - ") {
			parts := strings.SplitN(title, " - ", 2)
			artist = strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])
		}

		return metadataProbeMsg{
			collectionIdx: collectionIdx,
			link:          url,
			title:         title,
			artist:        artist,
		}
	}
}
