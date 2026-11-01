package main

import (
	"os"
	"runtime/debug"
)

// describeInputSource identifies the validation input without exposing a local
// filesystem path in a report that a supplier may share publicly.
func describeInputSource(packageRef string, info os.FileInfo) string {
	if packageRef != "" {
		return packageRef
	}
	if info != nil && info.IsDir() {
		return "local directory"
	}
	return "local archive"
}

// cttSourceRevision uses Go's embedded VCS build information when available.
// An exported source tree or a build without VCS stamping leaves this blank.
func cttSourceRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return revisionFromBuildSettings(info.Settings)
}

func revisionFromBuildSettings(settings []debug.BuildSetting) string {
	var revision string
	var modified bool
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision != "" && modified {
		return revision + " (modified working tree)"
	}
	return revision
}
