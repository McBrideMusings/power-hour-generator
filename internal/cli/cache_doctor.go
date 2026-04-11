package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	xterm "github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"powerhour/internal/cache"
	"powerhour/internal/cachedoctor"
	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/project"
)

type cacheDoctorOptions struct {
	all         bool
	write       bool
	yes         bool
	requery     bool
	indexArgs   []string
	identifiers []string
	artistLike  string
}

type cacheDoctorFinding = cachedoctor.Finding

func newCacheDoctorCmd() *cobra.Command {
	var opts cacheDoctorOptions

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Inspect and repair cached title/artist metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCacheDoctor(cmd, opts)
		},
	}

	cmd.Flags().BoolVar(&opts.all, "all", false, "Include cache entries not referenced by the current project")
	cmd.Flags().BoolVar(&opts.write, "write", false, "Apply high-confidence fixes non-interactively")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Accept all fixes when used with --write")
	cmd.Flags().BoolVar(&opts.requery, "requery", false, "Re-query yt-dlp metadata for URL-backed entries before normalization")
	cmd.Flags().StringSliceVar(&opts.indexArgs, "index", nil, "Limit to specific 1-based row index or range like 5-10 (repeat flag for multiple)")
	cmd.Flags().StringSliceVar(&opts.identifiers, "identifier", nil, "Limit to specific cache identifier(s) (repeat flag for multiple)")
	cmd.Flags().StringVar(&opts.artistLike, "artist", "", "Filter by current or proposed artist substring")
	return cmd
}

func runCacheDoctor(cmd *cobra.Command, opts cacheDoctorOptions) error {
	if opts.yes && !opts.write {
		return fmt.Errorf("--yes requires --write")
	}

	glogf, closer := logx.StartCommand("cache-doctor")
	defer closer.Close()
	glogf("cache doctor started")

	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}
	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	pp = paths.ApplyConfig(pp, cfg)
	pp = paths.ApplyLibrary(pp, cfg.LibraryShared(), cfg.LibraryPath())

	idx, err := cache.Load(pp)
	if err != nil {
		return err
	}

	var svc *cache.Service
	if opts.requery {
		svc, err = cache.NewService(cmd.Context(), pp, nil, nil)
		if err != nil {
			return err
		}
	}

	referenced := map[string]bool{}
	explicitIdentifiers := make(map[string]bool, len(opts.identifiers))
	for _, identifier := range opts.identifiers {
		identifier = strings.TrimSpace(identifier)
		if identifier != "" {
			explicitIdentifiers[identifier] = true
		}
	}
	if len(explicitIdentifiers) == 0 && !opts.all {
		referenced, err = projectReferencedIdentifiers(pp, cfg, idx, opts.indexArgs)
		if err != nil {
			return err
		}
	}

	normCfg := cache.LoadNormalizationConfig()
	knownArtists := cachedoctor.BuildKnownArtists(idx, normCfg)
	entries := cachedoctor.SortedEntries(idx)
	findings := make([]cacheDoctorFinding, 0, len(entries))
	for _, entry := range entries {
		if len(explicitIdentifiers) > 0 {
			if !explicitIdentifiers[entry.Identifier] {
				continue
			}
		} else if !opts.all && !referenced[entry.Identifier] {
			continue
		}
		finding, ok, err := cachedoctor.InspectEntry(cmd.Context(), svc, normCfg, knownArtists, entry, opts.requery)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if opts.artistLike != "" && !cachedoctor.MatchesArtistFilter(finding, opts.artistLike) {
			continue
		}
		findings = append(findings, finding)
	}

	if outputJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(findings)
	}

	if len(findings) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No cache metadata issues found.")
		return nil
	}

	if opts.write || !isInteractiveTTY() {
		applied, skipped, err := applyDoctorFindings(cmd, pp, idx, findings, normCfg, opts)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Reviewed %d entries. Applied %d fixes, skipped %d.\n", len(findings), applied, skipped)
		return nil
	}

	return runInteractiveCacheDoctor(cmd, pp, idx, findings, normCfg)
}

func projectReferencedIdentifiers(pp paths.ProjectPaths, cfg config.Config, idx *cache.Index, indexArgs []string) (map[string]bool, error) {
	out := map[string]bool{}
	resolver, err := project.NewCollectionResolver(cfg, pp)
	if err != nil {
		return nil, err
	}
	collections, err := resolver.LoadCollections()
	if err != nil {
		return nil, err
	}

	indexFilter := map[int]bool{}
	if len(indexArgs) > 0 {
		indexes, err := parseIndexArgs(indexArgs)
		if err != nil {
			return nil, err
		}
		for _, index := range indexes {
			indexFilter[index] = true
		}
	}

	for _, coll := range collections {
		for _, row := range coll.Rows {
			r := row.ToRow()
			if len(indexFilter) > 0 && !indexFilter[r.Index] {
				continue
			}
			if id, ok := idx.LookupLink(r.Link); ok {
				out[id] = true
				continue
			}
			if !strings.Contains(r.Link, "://") {
				path := r.Link
				if !filepath.IsAbs(path) {
					path = filepath.Join(pp.Root, path)
				}
				out[path] = true
			}
		}
	}
	return out, nil
}

func applyDoctorFindings(cmd *cobra.Command, pp paths.ProjectPaths, idx *cache.Index, findings []cacheDoctorFinding, normCfg cache.NormalizationConfig, opts cacheDoctorOptions) (int, int, error) {
	applied := 0
	skipped := 0
	for _, finding := range findings {
		if !opts.yes && finding.Confidence != "high" {
			skipped++
			continue
		}
		if !opts.write {
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %q / %q -> %q / %q [%s]\n",
				finding.File, finding.CurrentTitle, finding.CurrentArtist, finding.ProposedTitle, finding.ProposedArtist, finding.Confidence)
			skipped++
			continue
		}
		if err := cachedoctor.ApplyFinding(idx, finding); err != nil {
			return applied, skipped, err
		}
		applied++
	}
	if applied > 0 {
		if err := cache.Save(pp, idx); err != nil {
			return applied, skipped, err
		}
	}
	_ = normCfg
	return applied, skipped, nil
}

func runInteractiveCacheDoctor(cmd *cobra.Command, pp paths.ProjectPaths, idx *cache.Index, findings []cacheDoctorFinding, normCfg cache.NormalizationConfig) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()

	for _, finding := range findings {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s\n", finding.File)
		fmt.Fprintf(out, "  source:   %s\n", finding.Source)
		fmt.Fprintf(out, "  current:  %s | %s\n", cachedoctor.DisplayBlank(finding.CurrentArtist), cachedoctor.DisplayBlank(finding.CurrentTitle))
		fmt.Fprintf(out, "  proposed: %s | %s [%s]\n", cachedoctor.DisplayBlank(finding.ProposedArtist), cachedoctor.DisplayBlank(finding.ProposedTitle), finding.Confidence)
		if len(finding.Reasons) > 0 {
			fmt.Fprintf(out, "  reasons:  %s\n", strings.Join(finding.Reasons, "; "))
		}
		if finding.SimilarArtist != "" && !strings.EqualFold(finding.SimilarArtist, finding.ProposedArtist) {
			fmt.Fprintf(out, "  similar:  %s\n", finding.SimilarArtist)
		}

		for {
			fmt.Fprint(out, "Apply [y], skip [n], edit [e], alias+apply [a], quit [q]: ")
			line, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(line)) {
			case "y":
				if err := cachedoctor.ApplyFinding(idx, finding); err != nil {
					return err
				}
				if err := cache.Save(pp, idx); err != nil {
					return err
				}
				goto nextFinding
			case "n", "":
				goto nextFinding
			case "e":
				if err := interactiveEditFinding(reader, &finding); err != nil {
					return err
				}
				if err := cachedoctor.ApplyFinding(idx, finding); err != nil {
					return err
				}
				if err := cache.Save(pp, idx); err != nil {
					return err
				}
				goto nextFinding
			case "a":
				if strings.TrimSpace(finding.AliasCandidate) == "" || strings.TrimSpace(finding.ProposedArtist) == "" {
					fmt.Fprintln(out, "No alias candidate available for this entry.")
					continue
				}
				if err := cache.SaveArtistAlias(finding.AliasCandidate, finding.ProposedArtist); err != nil {
					return err
				}
				normCfg = cache.LoadNormalizationConfig()
				cachedoctor.ApplyAliasAcrossIndex(idx, normCfg, finding.AliasCandidate)
				if err := cache.Save(pp, idx); err != nil {
					return err
				}
				fmt.Fprintf(out, "Saved alias %q -> %q and reapplied matching entries.\n", finding.AliasCandidate, finding.ProposedArtist)
				goto nextFinding
			case "q":
				if err := cache.Save(pp, idx); err != nil {
					return err
				}
				return nil
			default:
				fmt.Fprintln(out, "Unrecognized choice.")
			}
		}
	nextFinding:
	}

	return nil
}

func interactiveEditFinding(reader *bufio.Reader, finding *cacheDoctorFinding) error {
	fmt.Printf("New artist [%s]: ", finding.ProposedArtist)
	artist, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	artist = strings.TrimSpace(artist)
	if artist != "" {
		finding.ProposedArtist = artist
	}

	fmt.Printf("New title [%s]: ", finding.ProposedTitle)
	title, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title != "" {
		finding.ProposedTitle = title
	}
	return nil
}

func isInteractiveTTY() bool {
	return xterm.IsTerminal(os.Stdout.Fd()) && xterm.IsTerminal(os.Stdin.Fd())
}
