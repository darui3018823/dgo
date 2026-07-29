// Discordgo - Discord bindings for Go
// Available at https://github.com/darui3018823/dgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dgo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIdentifyPropertiesUseCurrentFieldNames(t *testing.T) {
	data, err := json.Marshal(IdentifyProperties{
		OS:              "linux",
		Browser:         "dgo",
		Device:          "dgo",
		Referer:         "legacy",
		ReferringDomain: "legacy",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := string(data)
	for _, field := range []string{`"os"`, `"browser"`, `"device"`} {
		if !strings.Contains(got, field) {
			t.Errorf("identify properties %s missing %s", got, field)
		}
	}
	if strings.Contains(got, `"$`) || strings.Contains(got, "legacy") {
		t.Errorf("identify properties contain deprecated fields: %s", got)
	}
}

func TestApplicationIntegrationTypesConfigJSONTag(t *testing.T) {
	data, err := json.Marshal(Application{
		IntegrationTypesConfig: map[ApplicationIntegrationType]*ApplicationIntegrationTypeConfig{
			ApplicationIntegrationGuildInstall: {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := string(data)
	if !strings.Contains(got, `"integration_types_config"`) {
		t.Fatalf("application JSON missing integration_types_config: %s", got)
	}
	if strings.Contains(got, `"integration_types"`) {
		t.Fatalf("application JSON contains obsolete integration_types key: %s", got)
	}
}

func TestSticker_URL(t *testing.T) {
	t.Run("png", func(t *testing.T) {
		s := &Sticker{ID: "123", FormatType: StickerFormatTypePNG}
		if got, want := s.URL(), "https://cdn.discordapp.com/stickers/123.png"; got != want {
			t.Errorf("Sticker.URL() = %q, want %q", got, want)
		}
	})

	t.Run("apng", func(t *testing.T) {
		s := &Sticker{ID: "123", FormatType: StickerFormatTypeAPNG}
		if got, want := s.URL(), "https://cdn.discordapp.com/stickers/123.png"; got != want {
			t.Errorf("Sticker.URL() = %q, want %q", got, want)
		}
	})

	t.Run("gif", func(t *testing.T) {
		s := &Sticker{ID: "123", FormatType: StickerFormatTypeGIF}
		if got, want := s.URL(), "https://cdn.discordapp.com/stickers/123.gif"; got != want {
			t.Errorf("Sticker.URL() = %q, want %q", got, want)
		}
	})

	t.Run("lottie", func(t *testing.T) {
		s := &Sticker{ID: "123", FormatType: StickerFormatTypeLottie}
		if got, want := s.URL(), "https://cdn.discordapp.com/stickers/123.json"; got != want {
			t.Errorf("Sticker.URL() = %q, want %q", got, want)
		}
	})
}

func TestMember_DisplayName(t *testing.T) {
	user := &User{
		GlobalName: "Global",
	}
	t.Run("no server nickname set", func(t *testing.T) {
		m := &Member{
			Nick: "",
			User: user,
		}
		want := user.DisplayName()
		if dn := m.DisplayName(); dn != want {
			t.Errorf("Member.DisplayName() = %v, want %v", dn, want)
		}
	})
	t.Run("server nickname set", func(t *testing.T) {
		m := &Member{
			Nick: "Server",
			User: user,
		}
		if dn := m.DisplayName(); dn != m.Nick {
			t.Errorf("Member.DisplayName() = %v, want %v", dn, m.Nick)
		}
	})
}
