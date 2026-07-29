package dgo

import (
	"runtime/debug"
	"testing"
)

func TestResolveBuildInfoVersion(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{
			name: "unavailable",
			want: develVersion,
		},
		{
			name: "tagged main module",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Path: dgoModulePath, Version: "v0.30.6"},
			},
			want: "0.30.6",
		},
		{
			name: "tagged dependency",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/bot", Version: "(devel)"},
				Deps: []*debug.Module{
					{Path: dgoModulePath, Version: "v0.30.6"},
				},
			},
			want: "0.30.6",
		},
		{
			name: "pseudo version dependency",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/bot", Version: "(devel)"},
				Deps: []*debug.Module{
					{Path: dgoModulePath, Version: "v0.0.0-20260729153000-0123456789ab"},
				},
			},
			want: "0.0.0-20260729153000-0123456789ab",
		},
		{
			name: "versioned replacement",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/bot", Version: "(devel)"},
				Deps: []*debug.Module{
					{
						Path:    dgoModulePath,
						Version: "v0.30.6",
						Replace: &debug.Module{
							Path:    "example.com/dgo-fork",
							Version: "v0.31.0",
						},
					},
				},
			},
			want: "0.31.0",
		},
		{
			name: "local replacement does not report required version",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/bot", Version: "(devel)"},
				Deps: []*debug.Module{
					{
						Path:    dgoModulePath,
						Version: "v0.30.6",
						Replace: &debug.Module{Path: "../../dgo"},
					},
				},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "application-revision"},
				},
			},
			want: develVersion,
		},
		{
			name: "clean development checkout",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Path: dgoModulePath, Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			want: "devel+0123456789ab",
		},
		{
			name: "dirty development checkout",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Path: dgoModulePath, Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "0123456789abcdef"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			want: "devel+0123456789ab.dirty",
		},
		{
			name: "unrelated build information",
			ok:   true,
			info: &debug.BuildInfo{
				Main: debug.Module{Path: "example.com/bot", Version: "v1.0.0"},
			},
			want: develVersion,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveBuildInfoVersion(test.info, test.ok); got != test.want {
				t.Fatalf("resolveBuildInfoVersion() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFormattedVersion(t *testing.T) {
	tests := map[string]string{
		"":                                     "devel",
		"devel":                                "devel",
		"devel+0123456789ab":                   "devel+0123456789ab",
		"0.30.6":                               "v0.30.6",
		"0.0.0-20260729153000-0123456789ab":    "v0.0.0-20260729153000-0123456789ab",
		"0.30.6-0.20260729153000-0123456789ab": "v0.30.6-0.20260729153000-0123456789ab",
	}
	for version, want := range tests {
		if got := formattedVersion(version); got != want {
			t.Errorf("formattedVersion(%q) = %q, want %q", version, got, want)
		}
	}
}

func TestNewUsesResolvedVersion(t *testing.T) {
	session, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	versionLabel := formattedVersion(Version())
	if want := "DiscordBot (https://github.com/darui3018823/dgo, " + versionLabel + ")"; session.UserAgent != want {
		t.Errorf("UserAgent = %q, want %q", session.UserAgent, want)
	}
	if want := "DiscordGo " + versionLabel; session.Identify.Properties.Browser != want {
		t.Errorf("Identify browser = %q, want %q", session.Identify.Properties.Browser, want)
	}
}
