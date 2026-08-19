package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

		assetName, assetURL, err := selectYtDlpAsset(release.Assets, candidates)
		if err != nil {
			lastErr = err
			continue
		}

		checksum, err := fetchYtDlpChecksum(ctx, client, release.Assets, assetName)
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
			Checksum: checksum,
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

func selectYtDlpAsset(assets []githubReleaseAsset, candidates []string) (string, string, error) {
	for _, candidate := range candidates {
		for _, asset := range assets {
			if asset.Name == candidate {
				return asset.Name, asset.BrowserDownloadURL, nil
			}
		}
	}
	return "", "", fmt.Errorf("no yt-dlp asset available for platform")
}

// ytDlpChecksumAssetName is the filename yt-dlp publishes alongside each
// release containing "sha256  filename" lines for every release artefact.
const ytDlpChecksumAssetName = "SHA2-256SUMS"

// fetchYtDlpChecksum fetches the resolved release's SHA2-256SUMS asset (from
// the same release's already-resolved assets list, avoiding a second round
// of endpoint fallback) and returns the checksum entry matching assetName.
// It fails with a clear error if the sums asset or the specific filename's
// entry can't be found or fetched, rather than silently skipping
// verification.
func fetchYtDlpChecksum(ctx context.Context, client *http.Client, assets []githubReleaseAsset, assetName string) (string, error) {
	var sumsURL string
	for _, asset := range assets {
		if asset.Name == ytDlpChecksumAssetName {
			sumsURL = asset.BrowserDownloadURL
			break
		}
	}
	if sumsURL == "" {
		return "", fmt.Errorf("no %s asset found for yt-dlp release", ytDlpChecksumAssetName)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", fmt.Errorf("create checksum request: %w", err)
	}
	req.Header.Set("User-Agent", "powerhour/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", ytDlpChecksumAssetName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch %s: unexpected status %s", ytDlpChecksumAssetName, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", ytDlpChecksumAssetName, err)
	}

	sums := parseSha256Sums(string(body))
	checksum, ok := sums[assetName]
	if !ok {
		return "", fmt.Errorf("no checksum entry for %s in %s", assetName, ytDlpChecksumAssetName)
	}
	return checksum, nil
}

// parseSha256Sums parses the standard "sha256sum" tool output format:
// one "<hex checksum>  <filename>" pair per line (two spaces, or a single
// space with an optional '*' binary-mode marker before the filename).
func parseSha256Sums(content string) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		checksum := fields[0]
		filename := strings.TrimPrefix(fields[len(fields)-1], "*")
		result[filename] = checksum
	}
	return result
}
