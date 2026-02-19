package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/render"
	"powerhour/internal/tools"
)

var checkStrict bool

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check external tool availability",
		RunE:  runCheck,
	}

	cmd.Flags().BoolVar(&checkStrict, "strict", false, "fail when required tools are missing or outdated")

	return cmd
}

func runCheck(cmd *cobra.Command, _ []string) error {
	pp, err := paths.Resolve(projectDir)
	if err != nil {
		return err
	}

	exists, err := paths.DirExists(pp.Root)
	if err != nil {
		return fmt.Errorf("stat project dir: %w", err)
	}
	if !exists {
		return fmt.Errorf("project directory does not exist: %s", pp.Root)
	}

	if err := pp.EnsureMetaDirs(); err != nil {
		return err
	}

	logger, closer, err := logx.New(pp)
	if err != nil {
		return err
	}
	defer closer.Close()
	logger.Printf("powerhour check: project=%s", pp.Root)

	cfg, err := config.Load(pp.ConfigFile)
	if err != nil {
		return err
	}
	logger.Printf("loaded config version=%d", cfg.Version)

	detectCtx := tools.WithMinimums(cmd.Context(), cfg.ToolMinimums())
	statuses, err := tools.Detect(detectCtx)
	if err != nil {
		return err
	}

	for _, st := range statuses {
		logger.Printf("tool %s: source=%s version=%s satisfied=%v error=%s", st.Tool, st.Source, st.Version, st.Satisfied, st.Error)
	}

	var validations []config.ValidationResult
	if checkStrict {
		if err := ensureStrict(statuses); err != nil {
			return err
		}
		validations = cfg.ValidateStrict(pp.Root, render.ValidSegmentTokens())
		for _, v := range validations {
			if v.Level == "warning" {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", v.Message)
			}
		}
		var errs []string
		for _, v := range validations {
			if v.Level == "error" {
				errs = append(errs, v.Message)
			}
		}
		if len(errs) > 0 {
			return errors.New("config validation failed: " + strings.Join(errs, "; "))
		}
	}

	payload := struct {
		Project     string                    `json:"project"`
		Tools       []tools.Status            `json:"tools"`
		Validations []config.ValidationResult `json:"validations,omitempty"`
	}{
		Project:     pp.Root,
		Tools:       statuses,
		Validations: validations,
	}

	var data []byte
	if outputJSON {
		data, err = json.MarshalIndent(payload, "", "  ")
	} else {
		data, err = json.Marshal(payload)
	}
	if err != nil {
		return fmt.Errorf("encode json: %w", err)
	}

	cmd.Println(string(data))
	return nil
}

func ensureStrict(statuses []tools.Status) error {
	var failures []string
	for _, st := range statuses {
		if st.Satisfied {
			continue
		}
		msg := st.Tool
		if st.Error != "" {
			msg = fmt.Sprintf("%s (%s)", st.Tool, st.Error)
		}
		failures = append(failures, msg)
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New("tool check failed: " + strings.Join(failures, ", "))
}
