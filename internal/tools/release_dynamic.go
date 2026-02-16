package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

var errDynamicReleaseUnsupported = errors.New("dynamic release unsupported")

func resolveRelease(ctx context.Context, tool, version string) (releaseSpec, bool, error) {
	// Check the on-disk cache for "latest" lookups (version == "").
	if version == "" {
		if cached, ok := cachedLatestRelease(tool); ok {
			return cached, true, nil
		}
	}

	spec, err := fetchDynamicRelease(ctx, tool, version)
	if err == nil {
		if version == "" {
			cacheLatestRelease(tool, spec)
		}
		return spec, true, nil
	}

	var dynamicErr error
	if err != nil && !errors.Is(err, errDynamicReleaseUnsupported) {
		dynamicErr = err
	}

	spec, ok := lookupStaticRelease(tool, version)
	if ok {
		return spec, true, dynamicErr
	}

	if dynamicErr != nil {
		return releaseSpec{}, false, dynamicErr
	}
	return releaseSpec{}, false, nil
}

func fetchDynamicRelease(ctx context.Context, tool, version string) (releaseSpec, error) {
	switch tool {
	case "yt-dlp":
		return fetchYTDLPRelease(ctx, version)
	default:
		return releaseSpec{}, errDynamicReleaseUnsupported
	}
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string               `json:"tag_name"`
	Assets  []githubReleaseAsset `json:"assets"`
}

func fetchYTDLPRelease(ctx context.Context, version string) (releaseSpec, error) {
	candidates := ytDlpAssetCandidates()
	if len(candidates) == 0 {
		return releaseSpec{}, fmt.Errorf("yt-dlp download unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	endpoints := ytDlpReleaseEndpoints(version)
	client := &http.Client{Timeout: 30 * time.Second}

	var lastErr error
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "powerhour/1.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			lastErr = fmt.Errorf("yt-dlp release not found at %s", endpoint)
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			lastErr = fmt.Errorf("yt-dlp release query failed: %s", resp.Status)
			continue
		}

		var release githubRelease
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			resp.Body.Close()
			lastErr = fmt.Errorf("decode yt-dlp release: %w", err)
			continue
		}
		resp.Body.Close()

		assetURL, err := selectYtDlpAsset(release.Assets, candidates)
		if err != nil {
			lastErr = err
			continue
		}

		versionTag := strings.TrimPrefix(release.TagName, "v")
		if versionTag == "" {
			versionTag = release.TagName
		}

		return releaseSpec{
			Version:  versionTag,
			URL:      assetURL,
			Archive:  archiveFormatNone,
			Checksum: "",
		}, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("yt-dlp release metadata unavailable")
	}
	return releaseSpec{}, lastErr
}

func ytDlpReleaseEndpoints(version string) []string {
	base := "https://api.github.com/repos/yt-dlp/yt-dlp/releases"
	if version == "" {
		return []string{base + "/latest"}
	}

	ver := url.PathEscape(version)
	endpoints := []string{fmt.Sprintf("%s/tags/%s", base, ver)}
	if !strings.HasPrefix(version, "v") {
		endpoints = append(endpoints, fmt.Sprintf("%s/tags/%s", base, url.PathEscape("v"+version)))
	}
	return endpoints
}

func ytDlpAssetCandidates() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"yt-dlp_macos"}
	case "linux":
		switch runtime.GOARCH {
		case "amd64", "x86_64":
			return []string{"yt-dlp_linux"}
		case "arm64", "aarch64":
			return []string{"yt-dlp_linux_aarch64"}
		case "arm":
			return []string{"yt-dlp_linux_armv7l", "yt-dlp_linux_armv7"}
		}
	case "windows":
		return []string{"yt-dlp.exe"}
	}
	return nil
}

func selectYtDlpAsset(assets []githubReleaseAsset, candidates []string) (string, error) {
	for _, candidate := range candidates {
		for _, asset := range assets {
			if asset.Name == candidate {
				return asset.BrowserDownloadURL, nil
			}
		}
	}
	return "", fmt.Errorf("no yt-dlp asset available for platform")
}
