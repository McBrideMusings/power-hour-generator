package project

import (
	"testing"

	"powerhour/internal/config"
	"powerhour/pkg/csvplan"
)

func TestRenderRowTemplateArbitraryColumn(t *testing.T) {
	row := csvplan.Row{
		Index: 3,
		Title: "Song Title",
		CustomFields: map[string]string{
			"label": "My Label",
		},
	}

	got := RenderRowTemplate("{label} ({index})", row)
	want := "My Label (3)"
	if got != want {
		t.Fatalf("RenderRowTemplate: got %q, want %q", got, want)
	}
}

func TestCollectionRowLabel(t *testing.T) {
	cases := []struct {
		name string
		cc   config.CollectionConfig
		row  csvplan.CollectionRow
		want string
	}{
		{
			name: "display template renders",
			cc:   config.CollectionConfig{Display: "{title} – {artist}"},
			row: csvplan.CollectionRow{
				Link: "https://youtu.be/x",
				CustomFields: map[string]string{
					"title":  "Song",
					"artist": "Artist",
				},
			},
			want: "Song – Artist",
		},
		{
			// A declared-but-blank column is absent from CustomFields, so the
			// token survives replacement unless it is stripped — and a literal
			// "{label}" is non-empty, which would suppress the fallback.
			name: "display whose only token is unset falls back",
			cc:   config.CollectionConfig{Display: "{label}"},
			row: csvplan.CollectionRow{
				Link: "/media/Spaced.(1999).S01E06.Epiphanies.WEBDL-1080p.x264.AAC.[EN].MuTT.mkv",
			},
			want: "Spaced (1999) S01E06 Epiphanies",
		},
		{
			name: "unset token drops its stranded separator",
			cc:   config.CollectionConfig{Display: "{title} – {artist}"},
			row: csvplan.CollectionRow{
				Link:         "https://youtu.be/x",
				CustomFields: map[string]string{"title": "Song"},
			},
			want: "Song",
		},
		{
			name: "unset display falls back to fallback label",
			cc:   config.CollectionConfig{},
			row: csvplan.CollectionRow{
				Link: "/media/Spaced.(1999).S01E06.Epiphanies.WEBDL-1080p.x264.AAC.[EN].MuTT.mkv",
			},
			want: "Spaced (1999) S01E06 Epiphanies",
		},
		{
			name: "display renders empty falls back",
			cc:   config.CollectionConfig{Display: "{label}"},
			row: csvplan.CollectionRow{
				Link:         "/media/Uncut.Gems.2019.Bluray-1080p.x265.EAC3.5.1.[EN].Radarr.mkv",
				CustomFields: map[string]string{"label": ""},
			},
			want: "Uncut Gems 2019",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CollectionRowLabel(tc.cc, tc.row)
			if got != tc.want {
				t.Fatalf("CollectionRowLabel: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFallbackLabel(t *testing.T) {
	cases := []struct {
		link string
		want string
	}{
		{
			link: "/Volumes/media/library/television/Girls (2012) {imdb-tt1723816}/Season 01/Girls.(2012).S01E07.Welcome.to.Bushwick.a.k.a.The.Crackcident.Bluray-1080p.x265.EAC3.[EN].iVy.mkv",
			want: "Girls (2012) S01E07 Welcome to Bushwick a k a The Crackcident",
		},
		{
			link: "/Volumes/media/library/movies/Megalopolis (2024) {imdb-tt10128846}/Megalopolis.2024.WEBDL-1080p.Proper.h265.AAC.2.0.[EN].FLUX.mkv",
			want: "Megalopolis 2024",
		},
		{
			link: "/Volumes/media/library/movies/Uncut Gems (2019) {imdb-tt5727208}/Uncut.Gems.2019.Bluray-1080p.x265.EAC3.5.1.[EN].Radarr.mkv",
			want: "Uncut Gems 2019",
		},
		{
			link: "https://youtu.be/x7dVCE3Kb3s",
			want: "x7dVCE3Kb3s",
		},
		{
			link: "",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.link, func(t *testing.T) {
			got := FallbackLabel(tc.link)
			if got != tc.want {
				t.Fatalf("FallbackLabel(%q): got %q, want %q", tc.link, got, tc.want)
			}
		})
	}
}

func TestTimelineEntryLabel(t *testing.T) {
	collections := map[string]Collection{
		"songs": {
			Config: config.CollectionConfig{Display: "{title} – {artist}"},
			Rows: []csvplan.CollectionRow{
				{Link: "https://youtu.be/a", CustomFields: map[string]string{"title": "Song", "artist": "Artist"}},
			},
		},
	}

	entry := TimelineEntry{Collection: "songs", Index: 1}
	if got, want := TimelineEntryLabel(entry, collections), "Song – Artist"; got != want {
		t.Fatalf("TimelineEntryLabel: got %q, want %q", got, want)
	}

	fileEntry := TimelineEntry{SourceFile: "/media/Spaced.(1999).S01E06.Epiphanies.WEBDL-1080p.x264.AAC.[EN].MuTT.mkv"}
	if got, want := TimelineEntryLabel(fileEntry, collections), "Spaced (1999) S01E06 Epiphanies"; got != want {
		t.Fatalf("TimelineEntryLabel (file): got %q, want %q", got, want)
	}

	outOfRange := TimelineEntry{Collection: "songs", Index: 5}
	if got, want := TimelineEntryLabel(outOfRange, collections), "songs"; got != want {
		t.Fatalf("TimelineEntryLabel (out of range): got %q, want %q", got, want)
	}
}
