package cache

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWriteProxyBannerSuccess(t *testing.T) {
	origLookup := lookupProxyLocation
	defer func() { lookupProxyLocation = origLookup }()

	lookupProxyLocation = func(_ context.Context, _ string) (proxyLocation, error) {
		return proxyLocation{
			IP:      "203.0.113.10",
			Country: "Wonderland",
			Region:  "Rabbit Hole",
			City:    "Tea Party",
		}, nil
	}

	var buf bytes.Buffer
	writeProxyBanner(context.Background(), &buf, "socks5://proxy.example:9050")

	output := buf.String()
	if !strings.Contains(output, "yt-dlp proxy") {
		t.Fatalf("expected proxy header, got %q", output)
	}
	if !strings.Contains(output, "203.0.113.10") {
		t.Fatalf("expected proxy IP, got %q", output)
	}
	if !strings.Contains(output, "Tea Party, Rabbit Hole, Wonderland") {
		t.Fatalf("expected proxy location, got %q", output)
	}
}

func TestWriteProxyBannerError(t *testing.T) {
	origLookup := lookupProxyLocation
	defer func() { lookupProxyLocation = origLookup }()

	lookupProxyLocation = func(_ context.Context, _ string) (proxyLocation, error) {
		return proxyLocation{}, errors.New("nope")
	}

	var buf bytes.Buffer
	writeProxyBanner(context.Background(), &buf, "socks5://proxy.example:9050")

	output := buf.String()
	if !strings.Contains(output, "proxy lookup failed") {
		t.Fatalf("expected lookup failure message, got %q", output)
	}
}

func TestWriteProxyBannerNoProxy(t *testing.T) {
	var buf bytes.Buffer
	writeProxyBanner(context.Background(), &buf, "")
	if buf.Len() != 0 {
		t.Fatalf("expected no output when proxy empty, got %q", buf.String())
	}
}
