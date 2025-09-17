package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"powerhour/internal/config"
	"powerhour/internal/logx"
	"powerhour/internal/paths"
	"powerhour/internal/tools"
)

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check external tool availability",
		RunE:  runCheck,
	}

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

	toolInfo := tools.Probe(nil)
	for name, info := range toolInfo {
		logger.Printf("tool %s: available=%v version=%s error=%s", name, info.Available, info.Version, info.Error)
	}

	payload := struct {
		Project string                    `json:"project"`
		Tools   map[string]tools.ToolInfo `json:"tools"`
	}{
		Project: pp.Root,
		Tools:   toolInfo,
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
