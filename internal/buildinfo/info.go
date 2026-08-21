package buildinfo

import (
	"runtime"
	"runtime/debug"
)

var (
	Version = "dev"
	Commit  = "unknown"
)

type Info struct {
	Version      string            `json:"version"`
	Commit       string            `json:"commit"`
	GoVersion    string            `json:"goVersion"`
	Dependencies map[string]string `json:"dependencies"`
}

func Current() Info {
	info := Info{Version: Version, Commit: Commit, GoVersion: runtime.Version(), Dependencies: map[string]string{}}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, dependency := range build.Deps {
			info.Dependencies[dependency.Path] = dependency.Version
		}
	}
	return info
}
