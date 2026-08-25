package dashboard

import (
	"context"

	"powerhour/internal/tools"
)

// DetectToolStatuses probes every known tool and folds any pending update
// notices into the result. It is the single source of the dashboard's tools
// tab: the CLI calls it once before launching the dashboard, and the tools
// view's `r` key calls it again to re-read state after an update.
func DetectToolStatuses(ctx context.Context) ([]ToolStatus, string) {
	statuses, err := tools.Detect(ctx)
	if err != nil {
		return nil, ""
	}

	statusByName := make(map[string]tools.Status, len(statuses))
	for _, status := range statuses {
		statusByName[status.Tool] = status
	}

	result := make([]ToolStatus, 0, len(tools.KnownTools()))
	var warning string

	for _, name := range tools.KnownTools() {
		s, ok := statusByName[name]
		if !ok {
			continue
		}
		ts := ToolStatus{
			Name:          s.Tool,
			Optional:      s.Optional,
			Available:     s.Path != "",
			Version:       s.Version,
			Path:          s.Path,
			InstallMethod: s.InstallMethod,
		}
		if !s.Optional && !s.Satisfied {
			ts.UpdateAvail = "not satisfied"
			if warning == "" {
				warning = s.Tool
			}
		}
		result = append(result, ts)
	}

	// PendingUpdates, not CheckForUpdates: the tools tab displays live state,
	// so it must show an available update even after a CLI run has already
	// printed a notice for that version.
	notices := tools.PendingUpdates(ctx, statuses)
	for _, n := range notices {
		if warning == "" {
			warning = n.Tool + " update"
		}
		for i := range result {
			if result[i].Name == n.Tool {
				result[i].UpdateAvail = n.CurrentVersion + " → " + n.LatestVersion
			}
		}
	}

	return result, warning
}
