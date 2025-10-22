package project

import (
	"fmt"
	"path/filepath"
	"strings"

	"powerhour/internal/config"
	"powerhour/internal/paths"
	"powerhour/pkg/csvplan"
)

// ClipType identifies a configured clip category such as song or interstitial.
type ClipType string

const (
	ClipTypeSong         ClipType = "song"
	ClipTypeInterstitial ClipType = "interstitial"
	ClipTypeIntro        ClipType = "intro"
	ClipTypeOutro        ClipType = "outro"
)

// ClipSourceKind conveys how a clip type is sourced.
type ClipSourceKind string

const (
	SourceKindUnknown ClipSourceKind = ""
	SourceKindPlan    ClipSourceKind = "plan"
	SourceKindMedia   ClipSourceKind = "media"
)

// Resolver turns the raw configuration into resolved clip/timeline definitions.
type Resolver struct {
	cfg       config.Config
	paths     paths.ProjectPaths
	profiles  map[string]ResolvedProfile
	clipTypes map[ClipType]ClipTypeDefinition
	overrides []config.ClipOverride
	planErrs  map[ClipType]csvplan.ValidationErrors
}

// ResolvedProfile represents an overlay profile ready for use.
type ResolvedProfile struct {
	Name          string
	DefaultStyle  config.TextStyle
	Segments      []config.OverlaySegment
	segmentLookup map[string]int
}

// ResolveSegments returns a clone of the profile's segments with overrides applied.
func (rp ResolvedProfile) ResolveSegments(overrides map[string]SegmentOverride) []config.OverlaySegment {
	segments := cloneSegments(rp.Segments)
	if len(overrides) == 0 {
		return segments
	}
	for key, override := range overrides {
		idx, ok := rp.segmentLookup[key]
		if !ok || idx < 0 || idx >= len(segments) {
			continue
		}
		segment := &segments[idx]
		if override.Template != nil {
			segment.Template = *override.Template
		}
		if override.Disabled != nil {
			segment.Disabled = *override.Disabled
		}
		if override.Style != nil {
			segment.Style = mergeStyles(segment.Style, *override.Style)
		}
	}
	return segments
}

// ClipTypeDefinition captures per-type defaults derived from configuration.
type ClipTypeDefinition struct {
	Name           ClipType
	Source         ClipSourceDefinition
	RenderDefaults ClipRenderDefaults
	OverlayProfile string
}

// ClipSourceDefinition describes where clips of a given type originate.
type ClipSourceDefinition struct {
	Kind            ClipSourceKind
	PlanPath        string
	MediaPath       string
	DefaultDuration int
}

// ClipRenderDefaults captures default render behaviours for a clip type.
type ClipRenderDefaults struct {
	Duration *int
	FadeIn   float64
	FadeOut  float64
}

// Clip models a single entry in the resolved render timeline.
type Clip struct {
	Sequence         int
	ClipType         ClipType
	TypeIndex        int
	Row              csvplan.Row
	SourceKind       ClipSourceKind
	MediaPath        string
	DurationSeconds  int
	FadeInSeconds    float64
	FadeOutSeconds   float64
	OverlayProfile   string
	SegmentOverrides map[string]SegmentOverride
}

// SegmentOverride customises overlay segments at the clip level.
type SegmentOverride struct {
	Template *string
	Disabled *bool
	Style    *config.TextStyle
}

// NewResolver prepares a resolver for the supplied project configuration.
func NewResolver(cfg config.Config, pp paths.ProjectPaths) (*Resolver, error) {
	profiles := make(map[string]ResolvedProfile, len(cfg.Profiles.Overlays))
	for name, profile := range cfg.Profiles.Overlays {
		clone := cloneProfile(name, profile)
		profiles[name] = clone
	}

	clipTypes := map[ClipType]ClipTypeDefinition{}

	addType := func(name ClipType, ct config.ClipTypeConfig, fallbackProfile string, defaultDuration int) error {
		def := ClipTypeDefinition{
			Name:           name,
			Source:         resolveSource(name, ct.Source, pp, cfg, defaultDuration),
			RenderDefaults: resolveRenderDefaults(ct.Render),
			OverlayProfile: strings.TrimSpace(ct.Overlays.Profile),
		}
		if def.OverlayProfile == "" {
			def.OverlayProfile = fallbackProfile
		}
		if def.OverlayProfile != "" && !profileExists(profiles, def.OverlayProfile) {
			return fmt.Errorf("clip type %s references unknown overlay profile %q", name, def.OverlayProfile)
		}
		clipTypes[name] = def
		return nil
	}

	fallbackProfile := strings.TrimSpace(cfg.Clips.OverlayProfile)
	if fallbackProfile != "" && !profileExists(profiles, fallbackProfile) {
		return nil, fmt.Errorf("clips.overlay_profile references unknown overlay profile %q", fallbackProfile)
	}

	if err := addType(ClipTypeSong, cfg.Clips.Song, fallbackProfile, cfg.Clips.Song.Source.DefaultDurationSec); err != nil {
		return nil, err
	}
	if err := addType(ClipTypeInterstitial, cfg.Clips.Interstitial, fallbackProfile, cfg.Clips.Interstitial.Source.DefaultDurationSec); err != nil {
		return nil, err
	}
	if err := addType(ClipTypeIntro, cfg.Clips.Intro, fallbackProfile, cfg.Clips.Intro.Source.DefaultDurationSec); err != nil {
		return nil, err
	}
	if err := addType(ClipTypeOutro, cfg.Clips.Outro, fallbackProfile, cfg.Clips.Outro.Source.DefaultDurationSec); err != nil {
		return nil, err
	}

	return &Resolver{
		cfg:       cfg,
		paths:     pp,
		profiles:  profiles,
		clipTypes: clipTypes,
		overrides: cfg.Clips.Overrides,
		planErrs:  map[ClipType]csvplan.ValidationErrors{},
	}, nil
}

// Profiles returns the resolved overlay profiles.
func (r *Resolver) Profiles() map[string]ResolvedProfile {
	out := make(map[string]ResolvedProfile, len(r.profiles))
	for name, profile := range r.profiles {
		out[name] = profile
	}
	return out
}

// Profile returns a resolved profile when available.
func (r *Resolver) Profile(name string) (ResolvedProfile, bool) {
	profile, ok := r.profiles[strings.TrimSpace(name)]
	return profile, ok
}

// ClipType returns the definition for a given clip type when registered.
func (r *Resolver) ClipType(name ClipType) (ClipTypeDefinition, bool) {
	def, ok := r.clipTypes[name]
	return def, ok
}

// LoadPlans retrieves CSV plans for all configured clip types sourcing from plan files.
func (r *Resolver) LoadPlans() (map[ClipType][]csvplan.Row, error) {
	plans := make(map[ClipType][]csvplan.Row)
	headerAliases := r.cfg.HeaderAliases()
	r.planErrs = map[ClipType]csvplan.ValidationErrors{}

	for clipType, def := range r.clipTypes {
		if def.Source.Kind != SourceKindPlan || def.Source.PlanPath == "" {
			continue
		}

		opts := csvplan.Options{
			HeaderAliases:   headerAliases,
			DefaultDuration: def.Source.DefaultDuration,
		}
		if opts.DefaultDuration <= 0 {
			opts.DefaultDuration = r.cfg.PlanDefaultDuration()
		}

		rows, err := csvplan.LoadWithOptions(def.Source.PlanPath, opts)
		if err != nil {
			if ve, ok := err.(csvplan.ValidationErrors); ok {
				plans[clipType] = rows
				if len(ve) > 0 {
					r.planErrs[clipType] = ve
				}
				continue
			}
			return nil, fmt.Errorf("load %s plan %q: %w", clipType, def.Source.PlanPath, err)
		}
		plans[clipType] = rows
	}

	return plans, nil
}

// PlanErrors returns any validation errors encountered during the last LoadPlans call.
func (r *Resolver) PlanErrors() map[ClipType]csvplan.ValidationErrors {
	if len(r.planErrs) == 0 {
		return nil
	}
	dup := make(map[ClipType]csvplan.ValidationErrors, len(r.planErrs))
	for clipType, errs := range r.planErrs {
		clone := make(csvplan.ValidationErrors, len(errs))
		copy(clone, errs)
		dup[clipType] = clone
	}
	return dup
}

// BuildTimeline constructs the render sequence using provided plan rows.
func (r *Resolver) BuildTimeline(plans map[ClipType][]csvplan.Row) ([]Clip, error) {
	var timeline []Clip
	sequence := 0

	addClip := func(clip Clip) error {
		if clip.OverlayProfile != "" && !profileExists(r.profiles, clip.OverlayProfile) {
			return fmt.Errorf("clip %s#%d references unknown overlay profile %q", clip.ClipType, clip.TypeIndex, clip.OverlayProfile)
		}
		if clip.DurationSeconds <= 0 {
			return fmt.Errorf("clip %s#%d has invalid duration %d", clip.ClipType, clip.TypeIndex, clip.DurationSeconds)
		}
		sequence++
		clip.Sequence = sequence
		timeline = append(timeline, clip)
		return nil
	}

	if intro, ok := r.clipTypes[ClipTypeIntro]; ok && intro.Source.Kind == SourceKindMedia && intro.Source.MediaPath != "" {
		clip := newStaticClip(ClipTypeIntro, intro, intro.Source.MediaPath, 1)
		clip = r.applyOverrides(clip)
		if err := addClip(clip); err != nil {
			return nil, err
		}
	}

	songRows := plans[ClipTypeSong]
	songDef := r.clipTypes[ClipTypeSong]
	interRows := plans[ClipTypeInterstitial]
	interDef := r.clipTypes[ClipTypeInterstitial]
	interIndex := 0

	for _, row := range songRows {
		clip := newPlanClip(ClipTypeSong, songDef, row)
		clip = r.applyOverrides(clip)
		if err := addClip(clip); err != nil {
			return nil, err
		}

		if interIndex < len(interRows) {
			interClip := newPlanClip(ClipTypeInterstitial, interDef, interRows[interIndex])
			interIndex++
			interClip = r.applyOverrides(interClip)
			if err := addClip(interClip); err != nil {
				return nil, err
			}
		}
	}

	for interIndex < len(interRows) {
		clip := newPlanClip(ClipTypeInterstitial, interDef, interRows[interIndex])
		interIndex++
		clip = r.applyOverrides(clip)
		if err := addClip(clip); err != nil {
			return nil, err
		}
	}

	if outro, ok := r.clipTypes[ClipTypeOutro]; ok && outro.Source.Kind == SourceKindMedia && outro.Source.MediaPath != "" {
		clip := newStaticClip(ClipTypeOutro, outro, outro.Source.MediaPath, 1)
		clip = r.applyOverrides(clip)
		if err := addClip(clip); err != nil {
			return nil, err
		}
	}

	return timeline, nil
}

func (r *Resolver) applyOverrides(clip Clip) Clip {
	for _, override := range r.overrides {
		if !matchesOverride(clip, override.Match) {
			continue
		}

		if override.Render.DurationSec != nil && *override.Render.DurationSec > 0 {
			clip.DurationSeconds = *override.Render.DurationSec
		}
		if override.Render.FadeInSec != nil {
			clip.FadeInSeconds = *override.Render.FadeInSec
		}
		if override.Render.FadeOutSec != nil {
			clip.FadeOutSeconds = *override.Render.FadeOutSec
		}

		if name := strings.TrimSpace(override.Overlays.Profile); name != "" {
			clip.OverlayProfile = name
		}

		for _, seg := range override.Overlays.Segments {
			clip.applySegmentOverride(seg)
		}
	}
	return clip
}

func (c *Clip) applySegmentOverride(adj config.ClipOverlaySegmentAdjustment) {
	if c.SegmentOverrides == nil {
		c.SegmentOverrides = map[string]SegmentOverride{}
	}
	key := strings.ToLower(strings.TrimSpace(adj.Name))
	if key == "" {
		return
	}
	existing := c.SegmentOverrides[key]
	if adj.Template != nil {
		val := strings.TrimSpace(*adj.Template)
		existing.Template = &val
	}
	if adj.Disabled != nil {
		disabled := *adj.Disabled
		existing.Disabled = &disabled
	}
	if adj.Style != nil {
		style := cloneTextStyle(*adj.Style)
		existing.Style = &style
	}
	c.SegmentOverrides[key] = existing
}

func matchesOverride(clip Clip, match config.ClipMatch) bool {
	targetType := strings.TrimSpace(match.ClipType)
	if targetType == "" {
		return false
	}
	if strings.ToLower(targetType) != string(clip.ClipType) {
		return false
	}

	if match.Index != nil && clip.TypeIndex != *match.Index {
		return false
	}

	if name := strings.TrimSpace(match.Name); name != "" {
		if !clipMatchesName(clip, name) {
			return false
		}
	}

	if id := strings.TrimSpace(match.ID); id != "" {
		if !clipMatchesID(clip, id) {
			return false
		}
	}

	return true
}

func clipMatchesName(clip Clip, name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return true
	}
	if clip.Row.Title != "" && strings.ToLower(clip.Row.Title) == name {
		return true
	}
	if clip.Row.Name != "" && strings.ToLower(clip.Row.Name) == name {
		return true
	}
	return false
}

func clipMatchesID(clip Clip, id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return true
	}
	if clip.Row.Link != "" && strings.ToLower(clip.Row.Link) == id {
		return true
	}
	return false
}

func newPlanClip(name ClipType, def ClipTypeDefinition, row csvplan.Row) Clip {
	duration := row.DurationSeconds
	if duration <= 0 && def.RenderDefaults.Duration != nil && *def.RenderDefaults.Duration > 0 {
		duration = *def.RenderDefaults.Duration
	}
	return Clip{
		ClipType:        name,
		TypeIndex:       row.Index,
		Row:             row,
		SourceKind:      SourceKindPlan,
		DurationSeconds: duration,
		FadeInSeconds:   def.RenderDefaults.FadeIn,
		FadeOutSeconds:  def.RenderDefaults.FadeOut,
		OverlayProfile:  def.OverlayProfile,
	}
}

func newStaticClip(name ClipType, def ClipTypeDefinition, path string, index int) Clip {
	duration := 0
	if def.RenderDefaults.Duration != nil && *def.RenderDefaults.Duration > 0 {
		duration = *def.RenderDefaults.Duration
	} else if def.Source.DefaultDuration > 0 {
		duration = def.Source.DefaultDuration
	}
	return Clip{
		ClipType:        name,
		TypeIndex:       index,
		SourceKind:      SourceKindMedia,
		MediaPath:       path,
		DurationSeconds: duration,
		FadeInSeconds:   def.RenderDefaults.FadeIn,
		FadeOutSeconds:  def.RenderDefaults.FadeOut,
		OverlayProfile:  def.OverlayProfile,
	}
}

func resolveSource(name ClipType, source config.ClipSourceConfig, pp paths.ProjectPaths, cfg config.Config, defaultDuration int) ClipSourceDefinition {
	planPath := strings.TrimSpace(source.Plan)
	mediaPath := strings.TrimSpace(source.Media)

	if planPath == "" && name == ClipTypeSong {
		planPath = cfg.PlanFile()
		if planPath == "" {
			planPath = pp.CSVFile
		}
	}

	if planPath != "" {
		planPath = resolveProjectPath(pp.Root, planPath)
	}
	if mediaPath != "" {
		mediaPath = resolveProjectPath(pp.Root, mediaPath)
	}

	switch {
	case planPath != "":
		return ClipSourceDefinition{
			Kind:            SourceKindPlan,
			PlanPath:        planPath,
			DefaultDuration: pickPositive(source.DefaultDurationSec, defaultDuration),
		}
	case mediaPath != "":
		return ClipSourceDefinition{
			Kind:            SourceKindMedia,
			MediaPath:       mediaPath,
			DefaultDuration: pickPositive(source.DefaultDurationSec, defaultDuration),
		}
	default:
		return ClipSourceDefinition{
			Kind:            SourceKindUnknown,
			DefaultDuration: pickPositive(source.DefaultDurationSec, defaultDuration),
		}
	}
}

func resolveRenderDefaults(render config.ClipRenderConfig) ClipRenderDefaults {
	def := ClipRenderDefaults{
		Duration: copyIntPtr(render.DurationSec),
		FadeIn:   valueOrZero(render.FadeInSec),
		FadeOut:  valueOrZero(render.FadeOutSec),
	}
	return def
}

func cloneProfile(name string, profile config.OverlayProfile) ResolvedProfile {
	segments := cloneSegments(profile.Segments)
	index := make(map[string]int, len(segments))
	for i, segment := range segments {
		index[strings.ToLower(segment.Name)] = i
	}
	return ResolvedProfile{
		Name:          name,
		DefaultStyle:  cloneTextStyle(profile.DefaultStyle),
		Segments:      segments,
		segmentLookup: index,
	}
}

func profileExists(profiles map[string]ResolvedProfile, name string) bool {
	_, ok := profiles[strings.TrimSpace(name)]
	return ok
}

func resolveProjectPath(root, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, value)
}

func pickPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func valueOrZero(ptr *float64) float64 {
	if ptr == nil {
		return 0
	}
	return *ptr
}

func cloneTextStyle(style config.TextStyle) config.TextStyle {
	clone := style
	if style.FontSize != nil {
		value := *style.FontSize
		clone.FontSize = &value
	}
	if style.OutlineWidth != nil {
		value := *style.OutlineWidth
		clone.OutlineWidth = &value
	}
	if style.LineSpacing != nil {
		value := *style.LineSpacing
		clone.LineSpacing = &value
	}
	if style.LetterSpacing != nil {
		value := *style.LetterSpacing
		clone.LetterSpacing = &value
	}
	return clone
}

func cloneSegments(segments []config.OverlaySegment) []config.OverlaySegment {
	if len(segments) == 0 {
		return nil
	}
	clones := make([]config.OverlaySegment, len(segments))
	for i, segment := range segments {
		clones[i] = segment
		clones[i].Style = cloneTextStyle(segment.Style)
	}
	return clones
}

func copyIntPtr(src *int) *int {
	if src == nil {
		return nil
	}
	value := *src
	return &value
}

func mergeStyles(base, override config.TextStyle) config.TextStyle {
	result := cloneTextStyle(base)
	if strings.TrimSpace(override.FontFile) != "" {
		result.FontFile = override.FontFile
	}
	if override.FontSize != nil {
		value := *override.FontSize
		result.FontSize = &value
	}
	if strings.TrimSpace(override.FontColor) != "" {
		result.FontColor = override.FontColor
	}
	if strings.TrimSpace(override.OutlineColor) != "" {
		result.OutlineColor = override.OutlineColor
	}
	if override.OutlineWidth != nil {
		value := *override.OutlineWidth
		result.OutlineWidth = &value
	}
	if override.LineSpacing != nil {
		value := *override.LineSpacing
		result.LineSpacing = &value
	}
	if override.LetterSpacing != nil {
		value := *override.LetterSpacing
		result.LetterSpacing = &value
	}
	return result
}
