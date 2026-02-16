package project

import (
	"fmt"
	"strings"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

// Collection represents a resolved collection with its plan and configuration.
type Collection struct {
	Name        string
	Plan        string // Resolved plan file path
	OutputDir   string // Resolved output directory path
	Profile     string // Overlay profile name (may be empty)
	Config      config.CollectionConfig
	Rows        []csvplan.CollectionRow
	PlanErrors  csvplan.ValidationErrors
}

// CollectionResolver loads and resolves collections from configuration.
type CollectionResolver struct {
	cfg      config.Config
	paths    paths.ProjectPaths
	profiles map[string]ResolvedProfile
}

// NewCollectionResolver creates a resolver for collections.
func NewCollectionResolver(cfg config.Config, pp paths.ProjectPaths) (*CollectionResolver, error) {
	// Validate collections
	if err := cfg.ValidateCollections(); err != nil {
		return nil, err
	}

	// Load overlay profiles
	profiles := make(map[string]ResolvedProfile, len(cfg.Profiles))
	for name, profile := range cfg.Profiles {
		clone := cloneProfile(name, profile)
		profiles[name] = clone
	}

	return &CollectionResolver{
		cfg:      cfg,
		paths:    pp,
		profiles: profiles,
	}, nil
}

// LoadCollections loads all configured collections with their plan data.
func (r *CollectionResolver) LoadCollections() (map[string]Collection, error) {
	if r.cfg.Collections == nil || len(r.cfg.Collections) == 0 {
		return nil, nil
	}

	collections := make(map[string]Collection, len(r.cfg.Collections))

	for name, collCfg := range r.cfg.Collections {
		// Resolve plan path
		planPath := strings.TrimSpace(collCfg.Plan)
		if planPath == "" {
			return nil, fmt.Errorf("collection %q: plan path is required", name)
		}
		planPath = resolveProjectPath(r.paths.Root, planPath)

		// Resolve output directory
		outputDir := r.paths.CollectionOutputDir(r.cfg, name)

		// Validate profile if specified
		if collCfg.Profile != "" {
			if !profileExists(r.profiles, collCfg.Profile) {
				return nil, fmt.Errorf("collection %q: profile %q does not exist", name, collCfg.Profile)
			}
		}

		// Load collection plan
		opts := csvplan.CollectionOptions{
			LinkHeader:      collCfg.LinkHeader,
			StartHeader:     collCfg.StartHeader,
			DurationHeader:  collCfg.DurationHeader,
			DefaultDuration: 60, // TODO: Make this configurable?
		}

		rows, err := csvplan.LoadCollection(planPath, opts)
		var planErrs csvplan.ValidationErrors
		if err != nil {
			if ve, ok := err.(csvplan.ValidationErrors); ok {
				planErrs = ve
			} else {
				return nil, fmt.Errorf("load collection %q plan: %w", name, err)
			}
		}

		collection := Collection{
			Name:       name,
			Plan:       planPath,
			OutputDir:  outputDir,
			Profile:    collCfg.Profile,
			Config:     collCfg,
			Rows:       rows,
			PlanErrors: planErrs,
		}

		collections[name] = collection
	}

	return collections, nil
}

// Profile returns a resolved overlay profile by name.
func (r *CollectionResolver) Profile(name string) (ResolvedProfile, bool) {
	profile, ok := r.profiles[strings.TrimSpace(name)]
	return profile, ok
}

// Profiles returns all resolved overlay profiles.
func (r *CollectionResolver) Profiles() map[string]ResolvedProfile {
	out := make(map[string]ResolvedProfile, len(r.profiles))
	for name, profile := range r.profiles {
		out[name] = profile
	}
	return out
}

// CollectionPlanRow represents a row from a collection for fetch/validate operations.
type CollectionPlanRow struct {
	CollectionName string
	Row            csvplan.Row
}

// FlattenCollections converts collections into a flat list of plan rows for fetch operations.
func FlattenCollections(collections map[string]Collection) []CollectionPlanRow {
	if len(collections) == 0 {
		return nil
	}

	var flat []CollectionPlanRow
	for name, coll := range collections {
		for _, collRow := range coll.Rows {
			flat = append(flat, CollectionPlanRow{
				CollectionName: name,
				Row:            collRow.ToRow(),
			})
		}
	}

	return flat
}

// CollectionClip represents a clip from a collection for rendering.
type CollectionClip struct {
	CollectionName   string
	Clip             Clip
	OutputDir        string
	DefaultDuration  int
}

// BuildCollectionClips creates render-ready clips from all collections.
func (r *CollectionResolver) BuildCollectionClips(collections map[string]Collection) ([]CollectionClip, error) {
	if len(collections) == 0 {
		return nil, nil
	}

	var clips []CollectionClip
	sequence := 0

	for name, coll := range collections {
		// Validate profile if specified
		if coll.Profile != "" {
			_, hasProfile := r.Profile(coll.Profile)
			if !hasProfile {
				return nil, fmt.Errorf("collection %q references unknown profile %q", name, coll.Profile)
			}
		}

		// Get profile defaults if available
		var fadeIn, fadeOut float64
		var defaultDuration int = 60
		if coll.Profile != "" {
			if profile, hasProfile := r.Profile(coll.Profile); hasProfile {
				if profile.FadeInSec != nil {
					fadeIn = *profile.FadeInSec
				}
				if profile.FadeOutSec != nil {
					fadeOut = *profile.FadeOutSec
				}
				if profile.DefaultDurationSec != nil && *profile.DefaultDurationSec > 0 {
					defaultDuration = *profile.DefaultDurationSec
				}
			}
		}

		// Build clips from collection rows
		for _, collRow := range coll.Rows {
			sequence++
			row := collRow.ToRow()

			// Build a generic clip
			clip := Clip{
				Sequence:        sequence,
				ClipType:        ClipType(name), // Use collection name as clip type
				TypeIndex:       row.Index,
				Row:             row,
				SourceKind:      SourceKindPlan,
				DurationSeconds: row.DurationSeconds,
				FadeInSeconds:   fadeIn,
				FadeOutSeconds:  fadeOut,
				OverlayProfile:  coll.Profile,
			}

			collClip := CollectionClip{
				CollectionName:  name,
				Clip:            clip,
				OutputDir:       coll.OutputDir,
				DefaultDuration: defaultDuration,
			}

			clips = append(clips, collClip)
		}
	}

	return clips, nil
}
