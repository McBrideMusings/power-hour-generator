package tools

import (
	"os"
	"strings"
)

// UpdateSupported reports whether powerhour knows how to update a tool given
// how it was installed. Externally-managed tools always have a package-manager
// path; everything else is only updatable when powerhour can install it itself.
func UpdateSupported(tool, installMethod string) bool {
	if externalInstallMethod(installMethod) {
		return true
	}
	def, ok := Definition(tool)
	return ok && def.Installable
}

// UpdateArgv returns the argv that updates a tool, routed by its install
// method. Externally-managed tools go through their package manager;
// powerhour-managed tools re-enter this binary's `tools install`, pinned to
// the cached latest version when one is known.
//
// projectRoot is passed through as --project on the managed path so the
// install resolves the same project the caller is working in. Pass "" to let
// the install resolve the current directory.
//
// Callers that run the argv themselves must resolve it through a real shell
// exec: it is a plain command and arguments, never a shell string.
func UpdateArgv(tool, installMethod, projectRoot string) []string {
	if externalInstallMethod(installMethod) {
		notice := UpdateNotice{Tool: tool, InstallMethod: installMethod}
		return strings.Fields(notice.UpdateCommand())
	}

	argv := []string{powerhourBinary(), "tools", "install", tool, "--force"}
	if version := FormatUpdateTarget(tool); version != "" {
		argv = append(argv, "--version", version)
	}
	if projectRoot != "" {
		argv = append(argv, "--project", projectRoot)
	}
	return argv
}

// externalInstallMethod reports whether a package manager owns the binary, in
// which case powerhour defers the upgrade to it rather than installing over it.
func externalInstallMethod(installMethod string) bool {
	switch installMethod {
	case InstallMethodHomebrew, InstallMethodApt, InstallMethodSnap, InstallMethodPip:
		return true
	}
	return false
}

// powerhourBinary resolves the running executable so a dashboard launched from
// a dev build updates through that same build rather than whatever `powerhour`
// happens to be first on PATH.
func powerhourBinary() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return "powerhour"
	}
	return exe
}
