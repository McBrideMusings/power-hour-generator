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

	statuses, err := tools.Detect(nil)
	if err != nil {
		return err
	}

	for _, st := range statuses {
		logger.Printf("tool %s: source=%s version=%s satisfied=%v error=%s", st.Tool, st.Source, st.Version, st.Satisfied, st.Error)
	}

	if checkStrict {
		if err := ensureStrict(statuses); err != nil {
			return err
		}
	}

	payload := struct {
		Project string         `json:"project"`
		Tools   []tools.Status `json:"tools"`
	}{
		Project: pp.Root,
		Tools:   statuses,
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
