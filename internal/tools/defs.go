package tools

import (
	"runtime"
	"sort"
)

var toolDefinitions = map[string]ToolDefinition{
	"ffmpeg": {
		Name:           "ffmpeg",
		MinimumVersion: "6.0",
		DefaultVersion: "6.0",
		Binaries: []BinarySpec{
			{ID: "ffmpeg", Executable: executableName("ffmpeg"), VersionSwitch: "-version"},
			{ID: "ffprobe", Executable: executableName("ffprobe"), VersionSwitch: "-version"},
		},
	},
	"yt-dlp": {
		Name:           "yt-dlp",
		MinimumVersion: "2023.01.01",
		DefaultVersion: "2024.07.16",
		Binaries: []BinarySpec{
			{ID: "yt-dlp", Executable: executableName("yt-dlp"), VersionSwitch: "--version"},
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

// Definition returns the tool definition for the provided name.
func Definition(name string) (ToolDefinition, bool) {
	def, ok := toolDefinitions[name]
	return def, ok
}
