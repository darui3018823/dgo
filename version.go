package dgo

import (
	"runtime/debug"
	"strings"
)

const (
	dgoModulePath   = "github.com/darui3018823/dgo"
	develVersion    = "devel"
	vcsRevisionSize = 12
)

// buildVersion can be set by release tooling with:
//
//	-ldflags "-X github.com/darui3018823/dgo.buildVersion=v1.2.3"
var buildVersion string

// VERSION is the dgo module version reported by sessions and user agents.
//
// Tagged and pseudo-version module builds use the version recorded by Go.
// Local source and replacement builds report devel, including a VCS revision
// when the dgo module is the main module. VERSION remains exported for source
// compatibility with earlier releases.
var VERSION = resolvePackageVersion()

// Version returns the dgo module version.
func Version() string {
	return VERSION
}

func resolvePackageVersion() string {
	if version := normalizeBuildVersion(buildVersion); version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	return resolveBuildInfoVersion(info, ok)
}

func resolveBuildInfoVersion(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return develVersion
	}

	if info.Main.Path == dgoModulePath {
		if version := moduleVersion(info.Main); version != "" {
			return version
		}
		return resolveDevelVersion(info)
	}

	for _, dependency := range info.Deps {
		if dependency == nil || dependency.Path != dgoModulePath {
			continue
		}
		if version := moduleVersion(*dependency); version != "" {
			return version
		}
		// A local replacement has no reliable dgo VCS settings because the
		// build settings describe the application's main module.
		return develVersion
	}

	return develVersion
}

func moduleVersion(module debug.Module) string {
	if module.Replace != nil {
		return normalizeBuildVersion(module.Replace.Version)
	}
	return normalizeBuildVersion(module.Version)
}

func normalizeBuildVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || version == "(devel)" || version == develVersion {
		return ""
	}
	if len(version) > 1 && version[0] == 'v' && version[1] >= '0' && version[1] <= '9' {
		version = version[1:]
	}
	return version
}

func resolveDevelVersion(info *debug.BuildInfo) string {
	revision := ""
	modified := false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}

	if len(revision) > vcsRevisionSize {
		revision = revision[:vcsRevisionSize]
	}
	if revision == "" {
		if modified {
			return develVersion + "+dirty"
		}
		return develVersion
	}
	version := develVersion + "+" + revision
	if modified {
		version += ".dirty"
	}
	return version
}

func formattedVersion(version string) string {
	if version == "" {
		return develVersion
	}
	if version == develVersion || strings.HasPrefix(version, develVersion+"+") {
		return version
	}
	return "v" + version
}
