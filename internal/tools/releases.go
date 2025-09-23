package tools

import (
	"runtime"
	"sort"
)

type archiveFormat string

const (
	archiveFormatNone  archiveFormat = "none"
	archiveFormatZip   archiveFormat = "zip"
	archiveFormatTarGz archiveFormat = "tar.gz"
	archiveFormatTarXz archiveFormat = "tar.xz"
)

type releaseSpec struct {
	Version         string
	URL             string
	Checksum        string
	Archive         archiveFormat
	StripComponents int
	Files           map[string]string
}

// releaseIndex captures known download artefacts per tool/OS/arch. Checksums are
// currently left blank; populate them as part of the release process when the
// authoritative SHA256 values are available.
var releaseIndex = map[string]map[string]map[string]releaseSpec{
	"yt-dlp": {
		"darwin-amd64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp_macos",
				Checksum: "",
				Archive:  archiveFormatNone,
			},
		},
		"darwin-arm64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp_macos",
				Checksum: "",
				Archive:  archiveFormatNone,
			},
		},
		"linux-amd64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp_linux",
				Checksum: "",
				Archive:  archiveFormatNone,
			},
		},
		"linux-arm64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp_linux_aarch64",
				Checksum: "",
				Archive:  archiveFormatNone,
			},
		},
		"windows-amd64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp.exe",
				Checksum: "",
				Archive:  archiveFormatNone,
			},
		},
	},
}

func currentPlatformKey() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

func lookupStaticRelease(tool, version string) (releaseSpec, bool) {
	perTool, ok := releaseIndex[tool]
	if !ok {
		return releaseSpec{}, false
	}
	perPlatform, ok := perTool[currentPlatformKey()]
	if !ok || len(perPlatform) == 0 {
		return releaseSpec{}, false
	}
	if version != "" {
		rel, ok := perPlatform[version]
		if ok {
			return rel, true
		}
		return releaseSpec{}, false
	}
	versions := make([]string, 0, len(perPlatform))
	for v := range perPlatform {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	latest := versions[len(versions)-1]
	rel := perPlatform[latest]
	return rel, true
}
