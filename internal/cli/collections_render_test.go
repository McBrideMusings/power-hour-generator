package cli

import (
	"testing"

	"powerhour/internal/cache"
)

// TestCacheResolverForNilServiceStaysNil pins issue #183/#190: a nil
// *cache.Service passed through cacheResolverFor (the normal assignment
// path used at every job.RunCollectionJob call site in this file) must
// produce a true nil job.CacheResolver interface, not a non-nil interface
// wrapping a nil pointer. RunCollectionJob's "nil CacheService disables
// auto-fetch" contract depends on the interface itself being nil.
func TestCacheResolverForNilServiceStaysNil(t *testing.T) {
	var cacheSvc *cache.Service // never assigned, mirrors needsFetch == false

	resolver := cacheResolverFor(cacheSvc)

	if resolver != nil {
		t.Fatalf("cacheResolverFor(nil *cache.Service) = %#v, want a true nil interface (auto-fetch would incorrectly run)", resolver)
	}
}

// TestCacheResolverForNonNilServicePassesThrough confirms the helper still
// forwards a real *cache.Service unchanged, so auto-fetch continues to work
// when a cache service was actually constructed.
func TestCacheResolverForNonNilServicePassesThrough(t *testing.T) {
	cacheSvc := &cache.Service{}

	resolver := cacheResolverFor(cacheSvc)

	if resolver == nil {
		t.Fatalf("cacheResolverFor(non-nil *cache.Service) = nil, want the service to pass through")
	}
}
