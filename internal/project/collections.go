package project

import (
	"fmt"
	"path/filepath"
	"strings"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

// Collection represents a resolved collection with its plan and configuration.
type Collection struct {
	Name       string
	Plan       string // Resolved plan file path
	OutputDir  string // Resolved output directory path
	Config     config.CollectionConfig
	Rows       []csvplan.CollectionRow
	PlanErrors csvplan.ValidationErrors
	Headers    []string          // Raw CSV headers (normalized), for write-back
	Defaults   map[string]string // YAML column defaults, for write-back and row creation
	Delimiter  rune              // CSV delimiter (comma or tab), for write-back
	PlanFormat string            // "csv" or "yaml", for write-back
}

// CollectionResolver loads and resolves collections from configuration.
type CollectionResolver struct {
	cfg   config.Config
	paths paths.ProjectPaths
}

// NewCollectionResolver creates a resolver for collections.
func NewCollectionResolver(cfg config.Config, pp paths.ProjectPaths) (*CollectionResolver, error) {
	// Validate collections
	if err := cfg.ValidateCollections(); err != nil {
		return nil, err
	}

	return &CollectionResolver{
		cfg:   cfg,
		paths: pp,
	}, nil
}

// LoadCollections loads all configured collections with their plan data.
func (r *CollectionResolver) LoadCollections() (map[string]Collection, error) {
	if r.cfg.Collections == nil || len(r.cfg.Collections) == 0 {
		return nil, nil
	}

	collections := make(map[string]Collection, len(r.cfg.Collections))

	for name, collCfg := range r.cfg.Collections {
		outputDir := r.paths.CollectionOutputDir(r.cfg, name)

		// Single-file collection: synthesize one row, no CSV loading
		if file := strings.TrimSpace(collCfg.File); file != "" {
			filePath := resolveProjectPath(r.paths.Root, file)
			rows := []csvplan.CollectionRow{{
				Index:           1,
				Link:            filePath,
				StartRaw:        "0:00",
				Start:           0,
				DurationSeconds: collCfg.Duration,
				CustomFields:    map[string]string{},
			}}
			collections[name] = Collection{
				Name:      name,
				OutputDir: outputDir,
				Config:    collCfg,
				Rows:      rows,
			}
			continue
		}

		// Plan-based collection: load CSV/YAML
		planPath := strings.TrimSpace(collCfg.Plan)
		if planPath == "" {
			return nil, fmt.Errorf("collection %q: plan path is required", name)
		}
		planPath = resolveProjectPath(r.paths.Root, planPath)

		opts := CollectionOptionsForConfig(Collection{Config: collCfg})

		var (
			rows       []csvplan.CollectionRow
			err        error
			headers    []string
			defaults   map[string]string
			delimiter  rune
			planFormat string
		)
		ext := strings.ToLower(filepath.Ext(planPath))
		if ext == ".yaml" || ext == ".yml" {
			planFormat = "yaml"
			result, yamlErr := csvplan.LoadCollectionYAML(planPath, opts)
			rows = result.Rows
			headers = result.Columns
			defaults = result.Defaults
			err = yamlErr
		} else {
			planFormat = "csv"
			rows, err = csvplan.LoadCollection(planPath, opts)
			headers, delimiter, _ = csvplan.ReadHeaders(planPath)
		}
		var planErrs csvplan.ValidationErrors
		if err != nil {
			if err.Error() == "no data rows found" {
				rows = nil
			} else if ve, ok := err.(csvplan.ValidationErrors); ok {
				planErrs = ve
			} else {
				return nil, fmt.Errorf("load collection %q plan: %w", name, err)
			}
		}

		collections[name] = Collection{
			Name:       name,
			Plan:       planPath,
			OutputDir:  outputDir,
			Config:     collCfg,
			Rows:       rows,
			PlanErrors: planErrs,
			Headers:    headers,
			Defaults:   defaults,
			Delimiter:  delimiter,
			PlanFormat: planFormat,
		}
	}

	return collections, nil
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
	CollectionName  string
	Clip            Clip
	Overlays        []config.OverlayEntry
	OutputDir       string
	DefaultDuration int
}

// BuildCollectionClips creates render-ready clips from all collections.
func (r *CollectionResolver) BuildCollectionClips(collections map[string]Collection) ([]CollectionClip, error) {
	if len(collections) == 0 {
		return nil, nil
	}

	var clips []CollectionClip
	sequence := 0

	for name, coll := range collections {
		collCfg := coll.Config

		// Build clips from collection rows
		fadeIn, fadeOut := config.ResolveFade(collCfg.Fade, collCfg.FadeIn, collCfg.FadeOut)
		for _, collRow := range coll.Rows {
			sequence++
			row := collRow.ToRow()

			clip := Clip{
				Sequence:        sequence,
				ClipType:        ClipType(name),
				TypeIndex:       row.Index,
				Row:             row,
				SourceKind:      SourceKindPlan,
				DurationSeconds: row.DurationSeconds,
				FadeInSeconds:   fadeIn,
				FadeOutSeconds:  fadeOut,
			}

			collClip := CollectionClip{
				CollectionName:  name,
				Clip:            clip,
				Overlays:        collCfg.Overlays,
				OutputDir:       coll.OutputDir,
				DefaultDuration: 60,
			}

			clips = append(clips, collClip)
		}
	}

	return clips, nil
}
