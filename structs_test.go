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

func TestApplicationCurrentFields(t *testing.T) {
	payload := []byte(`{
		"id":"app",
		"name":"dgo",
		"bot":{"id":"bot","username":"dgo"},
		"guild_id":"guild",
		"guild":{"id":"guild","name":"support"},
		"flags":8192,
		"flags_new":"4294975488",
		"approximate_guild_count":42,
		"approximate_user_install_count":13,
		"approximate_user_authorization_count":21,
		"redirect_uris":["https://example.com/callback"],
		"interactions_endpoint_url":"https://example.com/interactions",
		"role_connections_verification_url":null,
		"event_webhooks_url":"https://example.com/events",
		"event_webhooks_status":3,
		"event_webhooks_types":["APPLICATION_AUTHORIZED"],
		"tags":["utility"],
		"install_params":{"scopes":["bot"],"permissions":"2048"},
		"integration_types_config":{"0":{"oauth2_install_params":{"scopes":["bot"],"permissions":"2048"}}},
		"custom_install_url":"https://example.com/install"
	}`)

	var application Application
	if err := json.Unmarshal(payload, &application); err != nil {
		t.Fatal(err)
	}
	if application.Bot == nil || application.Bot.ID != "bot" {
		t.Fatalf("unexpected application bot: %#v", application.Bot)
	}
	if application.Guild == nil || application.Guild.ID != "guild" {
		t.Fatalf("unexpected application guild: %#v", application.Guild)
	}
	if application.FlagsNew != "4294975488" {
		t.Fatalf("FlagsNew = %q", application.FlagsNew)
	}
	if application.ApproximateGuildCount == nil || *application.ApproximateGuildCount != 42 ||
		application.ApproximateUserInstallCount == nil || *application.ApproximateUserInstallCount != 13 ||
		application.ApproximateUserAuthorizationCount == nil || *application.ApproximateUserAuthorizationCount != 21 {
		t.Fatalf("unexpected approximate counts: %#v", application)
	}
	if application.InteractionsEndpointURL == nil ||
		*application.InteractionsEndpointURL != "https://example.com/interactions" ||
		application.RoleConnectionsVerificationURL != nil ||
		application.EventWebhooksStatus != ApplicationEventWebhooksDisabledByDiscord {
		t.Fatalf("unexpected application URLs/status: %#v", application)
	}
	if application.InstallParams == nil ||
		application.InstallParams.Permissions != 2048 ||
		application.IntegrationTypesConfig[ApplicationIntegrationGuildInstall] == nil {
		t.Fatalf("unexpected install configuration: %#v", application)
	}
}

func TestCurrentPermissionConstantsAndAggregates(t *testing.T) {
	if PermissionSetVoiceChannelStatus != 1<<48 {
		t.Fatalf("PermissionSetVoiceChannelStatus = %#x", PermissionSetVoiceChannelStatus)
	}
	if PermissionPinMessages != 1<<51 {
		t.Fatalf("PermissionPinMessages = %#x", PermissionPinMessages)
	}
	if PermissionBypassSlowmode != 1<<52 {
		t.Fatalf("PermissionBypassSlowmode = %#x", PermissionBypassSlowmode)
	}

	current := int64(PermissionSetVoiceChannelStatus | PermissionPinMessages | PermissionBypassSlowmode)
	if int64(PermissionAll)&current != current {
		t.Fatalf("PermissionAll %#x does not include current permissions %#x", int64(PermissionAll), current)
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
