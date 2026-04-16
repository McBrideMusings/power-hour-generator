package tools

import (
	"runtime"
	"sort"
)

var toolDefinitions = map[string]ToolDefinition{
	"ffmpeg": {
		Name:           "ffmpeg",
		Installable:    true,
		MinimumVersion: "6.0",
		DefaultVersion: "6.0",
		Binaries: []BinarySpec{
			{ID: "ffmpeg", Executable: executableName("ffmpeg"), VersionSwitch: "-version"},
			{ID: "ffprobe", Executable: executableName("ffprobe"), VersionSwitch: "-version"},
		},
	},
	"yt-dlp": {
		Name:           "yt-dlp",
		Installable:    true,
		MinimumVersion: "2023.01.01",
		DefaultVersion: "2024.07.16",
		Binaries: []BinarySpec{
			{ID: "yt-dlp", Executable: executableName("yt-dlp"), VersionSwitch: "--version"},
		},
	},
	"vlc": {
		Name:        "vlc",
		Optional:    true,
		Installable: false,
		Binaries: []BinarySpec{
			{ID: "vlc", Executable: executableName("vlc"), VersionSwitch: "--version"},
		},
	},
}

func executableName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// KnownTools returns the list of managed tool names.
func KnownTools() []string {
	names := make([]string, 0, len(toolDefinitions))
	for name := range toolDefinitions {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RequiredTools returns the tools needed for normal fetch/render workflows.
func RequiredTools() []string {
	var names []string
	for _, name := range KnownTools() {
		if toolDefinitions[name].Optional {
			continue
		}
		names = append(names, name)
	}
	return names
}

// InstallableTools returns the tools that can be managed automatically.
func InstallableTools() []string {
	var names []string
	for _, name := range KnownTools() {
		if !toolDefinitions[name].Installable {
			continue
		}
		names = append(names, name)
	}
	return names
}

// RequiredFFmpegFilters lists the ffmpeg filters used across the render pipeline.
var RequiredFFmpegFilters = []string{
	"scale", "pad", "setsar", "fps", "fade", "drawtext",
	"loudnorm", "aresample",
}

// Definition returns the tool definition for the provided name.
func Definition(name string) (ToolDefinition, bool) {
	def, ok := toolDefinitions[name]
	return def, ok
}
