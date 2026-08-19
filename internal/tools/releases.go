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
// vendored from each release's SHA2-256SUMS asset at the time the version is
// pinned here (e.g. https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/SHA2-256SUMS)
// rather than fetched again at install time.
var releaseIndex = map[string]map[string]map[string]releaseSpec{
	"yt-dlp": {
		"darwin-amd64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp_macos",
				Checksum: "8ce707eb1b14432c531fb3c74466219b8aa60eaae1d9c7f83ff356a3cf862ee0",
				Archive:  archiveFormatNone,
			},
		},
		"darwin-arm64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp_macos",
				Checksum: "8ce707eb1b14432c531fb3c74466219b8aa60eaae1d9c7f83ff356a3cf862ee0",
				Archive:  archiveFormatNone,
			},
		},
		"linux-amd64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp_linux",
				Checksum: "a6b840e536014ce7b2c7c40b758080498ed5054aa96979e64fcc369752cdc8d3",
				Archive:  archiveFormatNone,
			},
		},
		"linux-arm64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp_linux_aarch64",
				Checksum: "3babd96d69327bb565874c858abe696c96b5b73d87ad36d0da71ccf623eb06cb",
				Archive:  archiveFormatNone,
			},
		},
		"windows-amd64": {
			"2024.07.16": {
				Version:  "2024.07.16",
				URL:      "https://github.com/yt-dlp/yt-dlp/releases/download/2024.07.16/yt-dlp.exe",
				Checksum: "f01b37ca4f3e934208a5439d1ec8ae49a18f2be9f68fec5e3cfed08cc38b3275",
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
