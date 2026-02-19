package config

import "testing"

func TestGlobalCacheEnabledDefault(t *testing.T) {
	cfg := Config{}
	if !cfg.GlobalCacheEnabled() {
		t.Fatal("expected GlobalCacheEnabled() = true when GlobalCache is nil")
	}
}

func TestGlobalCacheExplicitTrue(t *testing.T) {
	cfg := Config{Downloads: DownloadsConfig{GlobalCache: boolPtr(true)}}
	if !cfg.GlobalCacheEnabled() {
		t.Fatal("expected GlobalCacheEnabled() = true")
	}
}

func TestGlobalCacheExplicitFalse(t *testing.T) {
	cfg := Config{Downloads: DownloadsConfig{GlobalCache: boolPtr(false)}}
	if cfg.GlobalCacheEnabled() {
		t.Fatal("expected GlobalCacheEnabled() = false")
	}
}
