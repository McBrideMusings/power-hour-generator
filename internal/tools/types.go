package tools

type Source string

const (
	SourceUnknown Source = ""
	SourceCache   Source = "cache"
	SourceSystem  Source = "system"
)

// Status captures the resolved state for a managed tool.
type Status struct {
	Tool        string            `json:"tool"`
	Version     string            `json:"version,omitempty"`
	Minimum     string            `json:"minimum,omitempty"`
	Source      Source            `json:"source"`
	Path        string            `json:"path,omitempty"`
	Paths       map[string]string `json:"paths,omitempty"`
	InstalledAt string            `json:"installed_at,omitempty"`
	Checksum    string            `json:"checksum,omitempty"`
	Satisfied   bool              `json:"satisfied"`
	Error       string            `json:"error,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
}

// BinarySpec describes an executable managed for a tool.
type BinarySpec struct {
	ID            string
	Executable    string
	VersionSwitch string
}

// ToolDefinition contains metadata required to manage a tool.
type ToolDefinition struct {
	Name           string
	MinimumVersion string
	DefaultVersion string
	Binaries       []BinarySpec
}

// ManifestEntry records a resolved tool in the cache manifest.
type ManifestEntry struct {
	Tool        string            `json:"tool"`
	Version     string            `json:"version"`
	Source      Source            `json:"source"`
	Paths       map[string]string `json:"paths"`
	Checksum    string            `json:"checksum,omitempty"`
	InstalledAt string            `json:"installed_at,omitempty"`
}

// Manifest wraps persisted entries for quick lookup.
type Manifest struct {
	Entries map[string]ManifestEntry `json:"entries"`
}
