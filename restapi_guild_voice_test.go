package dgo

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestGuildVoiceRESTEndpoints(t *testing.T) {
	var nullTime *time.Time
	var nullBool *bool
	var nullString *string
	var nullWelcomeChannels []*GuildWelcomeScreenChannel

	currentVoiceParams := &CurrentUserVoiceStateEditParams{
		UserVoiceStateEditParams: UserVoiceStateEditParams{
			ChannelID: stringPointer("channel"),
			Suppress:  boolPointer(false),
		},
		RequestToSpeakTimestamp: &nullTime,
	}
	userVoiceParams := &UserVoiceStateEditParams{
		ChannelID: stringPointer("channel"),
		Suppress:  boolPointer(true),
	}
	welcomeParams := &GuildWelcomeScreenEditParams{
		Enabled:         &nullBool,
		WelcomeChannels: &nullWelcomeChannels,
		Description:     &nullString,
	}
	incidentParams := &GuildIncidentActionsEditParams{
		InvitesDisabledUntil: &nullTime,
		DMsDisabledUntil:     &nullTime,
	}

	tests := []struct {
		name         string
		method       string
		path         string
		requestBody  string
		responseCode int
		responseBody string
		auditReason  string
		call         func(*Session) error
	}{
		{
			name:         "get current user voice state",
			method:       http.MethodGet,
			path:         "/api/v10/guilds/guild/voice-states/@me",
			responseCode: http.StatusOK,
			responseBody: `{"guild_id":"guild","channel_id":"channel","user_id":"bot","suppress":false,"request_to_speak_timestamp":null}`,
			call: func(session *Session) error {
				state, err := session.CurrentUserVoiceState("guild")
				if err == nil && (state == nil || state.UserID != "bot" || state.ChannelID != "channel") {
					t.Fatalf("unexpected current voice state: %#v", state)
				}
				return err
			},
		},
		{
			name:         "get user voice state",
			method:       http.MethodGet,
			path:         "/api/v10/guilds/guild/voice-states/user",
			responseCode: http.StatusOK,
			responseBody: `{"guild_id":"guild","channel_id":"channel","user_id":"user","suppress":true}`,
			call: func(session *Session) error {
				state, err := session.UserVoiceState("guild", "user")
				if err == nil && (state == nil || state.UserID != "user" || !state.Suppress) {
					t.Fatalf("unexpected user voice state: %#v", state)
				}
				return err
			},
		},
		{
			name:         "modify current user voice state with nullable timestamp",
			method:       http.MethodPatch,
			path:         "/api/v10/guilds/guild/voice-states/@me",
			requestBody:  `{"channel_id":"channel","suppress":false,"request_to_speak_timestamp":null}`,
			responseCode: http.StatusNoContent,
			call: func(session *Session) error {
				return session.CurrentUserVoiceStateEdit("guild", currentVoiceParams)
			},
		},
		{
			name:         "modify user voice state",
			method:       http.MethodPatch,
			path:         "/api/v10/guilds/guild/voice-states/user",
			requestBody:  `{"channel_id":"channel","suppress":true}`,
			responseCode: http.StatusNoContent,
			call: func(session *Session) error {
				return session.UserVoiceStateEdit("guild", "user", userVoiceParams)
			},
		},
		{
			name:         "bulk guild ban",
			method:       http.MethodPost,
			path:         "/api/v10/guilds/guild/bulk-ban",
			requestBody:  `{"user_ids":["one","two"],"delete_message_seconds":3600}`,
			responseCode: http.StatusOK,
			responseBody: `{"banned_users":["one"],"failed_users":["two"]}`,
			auditReason:  "bulk cleanup",
			call: func(session *Session) error {
				result, err := session.GuildBulkBan(
					"guild",
					&GuildBulkBanParams{
						UserIDs:              []string{"one", "two"},
						DeleteMessageSeconds: 3600,
					},
					WithAuditLogReason("bulk cleanup"),
				)
				if err == nil && (result == nil ||
					!reflect.DeepEqual(result.BannedUsers, []string{"one"}) ||
					!reflect.DeepEqual(result.FailedUsers, []string{"two"})) {
					t.Fatalf("unexpected bulk ban response: %#v", result)
				}
				return err
			},
		},
		{
			name:         "get single guild role",
			method:       http.MethodGet,
			path:         "/api/v10/guilds/guild/roles/role",
			responseCode: http.StatusOK,
			responseBody: `{"id":"role","name":"moderator","permissions":"8","colors":{"primary_color":1,"secondary_color":null,"tertiary_color":null}}`,
			call: func(session *Session) error {
				role, err := session.GuildRole("guild", "role")
				if err == nil && (role == nil || role.ID != "role" || role.Permissions != 8) {
					t.Fatalf("unexpected role: %#v", role)
				}
				return err
			},
		},
		{
			name:         "get role member counts",
			method:       http.MethodGet,
			path:         "/api/v10/guilds/guild/roles/member-counts",
			responseCode: http.StatusOK,
			responseBody: `{"role":1337,"another":2}`,
			call: func(session *Session) error {
				counts, err := session.GuildRoleMemberCounts("guild")
				if err == nil && !reflect.DeepEqual(counts, map[string]uint64{"role": 1337, "another": 2}) {
					t.Fatalf("unexpected role member counts: %#v", counts)
				}
				return err
			},
		},
		{
			name:         "get guild voice regions",
			method:       http.MethodGet,
			path:         "/api/v10/guilds/guild/regions",
			responseCode: http.StatusOK,
			responseBody: `[{"id":"vip-us-east","name":"VIP US East","optimal":true,"deprecated":false,"custom":true}]`,
			call: func(session *Session) error {
				regions, err := session.GuildVoiceRegions("guild")
				if err == nil && (len(regions) != 1 || regions[0].ID != "vip-us-east" || !regions[0].Custom) {
					t.Fatalf("unexpected guild voice regions: %#v", regions)
				}
				return err
			},
		},
		{
			name:         "get public guild widget",
			method:       http.MethodGet,
			path:         "/api/v10/guilds/guild/widget.json",
			responseCode: http.StatusOK,
			responseBody: `{"id":"guild","name":"Guild","instant_invite":null,"channels":[{"id":"channel","name":"Voice","position":1}],"members":[{"id":"0","username":"1234","discriminator":"0000","avatar":null,"status":"online","avatar_url":"https://cdn.example/avatar"}],"presence_count":1}`,
			call: func(session *Session) error {
				widget, err := session.GuildWidget("guild")
				if err == nil && (widget == nil || widget.InstantInvite != nil ||
					len(widget.Channels) != 1 || len(widget.Members) != 1 ||
					widget.Members[0].Avatar != nil || widget.Members[0].Status != StatusOnline) {
					t.Fatalf("unexpected guild widget: %#v", widget)
				}
				return err
			},
		},
		{
			name:         "get nullable vanity URL",
			method:       http.MethodGet,
			path:         "/api/v10/guilds/guild/vanity-url",
			responseCode: http.StatusOK,
			responseBody: `{"code":null,"uses":12}`,
			call: func(session *Session) error {
				vanity, err := session.GuildVanityURL("guild")
				if err == nil && (vanity == nil || vanity.Code != nil || vanity.Uses != 12) {
					t.Fatalf("unexpected vanity URL: %#v", vanity)
				}
				return err
			},
		},
		{
			name:         "get welcome screen",
			method:       http.MethodGet,
			path:         "/api/v10/guilds/guild/welcome-screen",
			responseCode: http.StatusOK,
			responseBody: `{"description":null,"welcome_channels":[{"channel_id":"channel","description":"Welcome","emoji_id":null,"emoji_name":"👋"}]}`,
			call: func(session *Session) error {
				screen, err := session.GuildWelcomeScreen("guild")
				if err == nil && (screen == nil || screen.Description != nil ||
					len(screen.WelcomeChannels) != 1 ||
					screen.WelcomeChannels[0].EmojiID != nil ||
					screen.WelcomeChannels[0].EmojiName == nil ||
					*screen.WelcomeChannels[0].EmojiName != "👋") {
					t.Fatalf("unexpected welcome screen: %#v", screen)
				}
				return err
			},
		},
		{
			name:         "clear nullable welcome screen fields",
			method:       http.MethodPatch,
			path:         "/api/v10/guilds/guild/welcome-screen",
			requestBody:  `{"enabled":null,"welcome_channels":null,"description":null}`,
			responseCode: http.StatusOK,
			responseBody: `{"description":null,"welcome_channels":[]}`,
			auditReason:  "reset welcome screen",
			call: func(session *Session) error {
				screen, err := session.GuildWelcomeScreenEdit(
					"guild",
					welcomeParams,
					WithAuditLogReason("reset welcome screen"),
				)
				if err == nil && (screen == nil || screen.Description != nil || len(screen.WelcomeChannels) != 0) {
					t.Fatalf("unexpected edited welcome screen: %#v", screen)
				}
				return err
			},
		},
		{
			name:         "clear nullable incident actions",
			method:       http.MethodPut,
			path:         "/api/v10/guilds/guild/incident-actions",
			requestBody:  `{"invites_disabled_until":null,"dms_disabled_until":null}`,
			responseCode: http.StatusOK,
			responseBody: `{"invites_disabled_until":null,"dms_disabled_until":"2026-07-30T12:00:00Z","dm_spam_detected_at":null}`,
			call: func(session *Session) error {
				incidents, err := session.GuildIncidentActionsEdit("guild", incidentParams)
				if err == nil && (incidents == nil || incidents.InvitesDisabledUntil != nil ||
					incidents.DMsDisabledUntil == nil ||
					!incidents.DMsDisabledUntil.Equal(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)) ||
					incidents.DMSpamDetectedAt != nil) {
					t.Fatalf("unexpected incidents data: %#v", incidents)
				}
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, err := New("Bot token")
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				requests++
				if request.Method != test.method {
					t.Fatalf("method = %q, want %q", request.Method, test.method)
				}
				if request.URL.Path != test.path {
					t.Fatalf("path = %q, want %q", request.URL.Path, test.path)
				}
				if got := request.Header.Get("X-Audit-Log-Reason"); got != test.auditReason {
					t.Fatalf("X-Audit-Log-Reason = %q, want %q", got, test.auditReason)
				}
				body, err := readRequestBody(request)
				if err != nil {
					t.Fatal(err)
				}
				assertJSONEqual(t, body, test.requestBody)
				return jsonResponse(test.responseCode, test.responseBody), nil
			})

			if err := test.call(session); err != nil {
				t.Fatal(err)
			}
			if requests != 1 {
				t.Fatalf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestGuildVoiceRESTNullableParams(t *testing.T) {
	var nullTime *time.Time
	var nullBool *bool
	var nullString *string
	var nullWelcomeChannels []*GuildWelcomeScreenChannel

	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{
			name:  "current voice omitted",
			value: CurrentUserVoiceStateEditParams{},
			want:  `{}`,
		},
		{
			name: "current voice timestamp clear",
			value: CurrentUserVoiceStateEditParams{
				RequestToSpeakTimestamp: &nullTime,
			},
			want: `{"request_to_speak_timestamp":null}`,
		},
		{
			name:  "welcome screen omitted",
			value: GuildWelcomeScreenEditParams{},
			want:  `{}`,
		},
		{
			name: "welcome screen clear",
			value: GuildWelcomeScreenEditParams{
				Enabled:         &nullBool,
				WelcomeChannels: &nullWelcomeChannels,
				Description:     &nullString,
			},
			want: `{"enabled":null,"welcome_channels":null,"description":null}`,
		},
		{
			name:  "incident actions omitted",
			value: GuildIncidentActionsEditParams{},
			want:  `{}`,
		},
		{
			name: "incident actions clear",
			value: GuildIncidentActionsEditParams{
				InvitesDisabledUntil: &nullTime,
				DMsDisabledUntil:     &nullTime,
			},
			want: `{"invites_disabled_until":null,"dms_disabled_until":null}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			body, err := json.Marshal(test.value)
			if err != nil {
				t.Fatal(err)
			}
			assertJSONEqual(t, string(body), test.want)
		})
	}
}

func TestGuildVoiceRESTModelsOnGuild(t *testing.T) {
	var guild Guild
	err := json.Unmarshal([]byte(`{
		"id":"guild",
		"welcome_screen":{"description":null,"welcome_channels":[]},
		"incidents_data":{
			"invites_disabled_until":null,
			"dms_disabled_until":"2026-07-30T12:00:00Z",
			"raid_detected_at":null
		}
	}`), &guild)
	if err != nil {
		t.Fatal(err)
	}
	if guild.WelcomeScreen == nil || guild.WelcomeScreen.Description != nil {
		t.Fatalf("unexpected guild welcome screen: %#v", guild.WelcomeScreen)
	}
	if guild.IncidentsData == nil || guild.IncidentsData.InvitesDisabledUntil != nil ||
		guild.IncidentsData.DMsDisabledUntil == nil ||
		!guild.IncidentsData.DMsDisabledUntil.Equal(time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)) ||
		guild.IncidentsData.RaidDetectedAt != nil {
		t.Fatalf("unexpected guild incidents data: %#v", guild.IncidentsData)
	}
}

func TestGuildVoiceRESTParamValidation(t *testing.T) {
	tooManyUsers := make([]string, 201)
	tooManyChannels := make([]*GuildWelcomeScreenChannel, 6)

	tests := []struct {
		name string
		call func(*Session) error
	}{
		{
			name: "nil bulk ban",
			call: func(session *Session) error {
				_, err := session.GuildBulkBan("guild", nil)
				return err
			},
		},
		{
			name: "empty bulk ban",
			call: func(session *Session) error {
				_, err := session.GuildBulkBan("guild", &GuildBulkBanParams{})
				return err
			},
		},
		{
			name: "too many bulk ban users",
			call: func(session *Session) error {
				_, err := session.GuildBulkBan("guild", &GuildBulkBanParams{UserIDs: tooManyUsers})
				return err
			},
		},
		{
			name: "negative bulk ban message seconds",
			call: func(session *Session) error {
				_, err := session.GuildBulkBan(
					"guild",
					&GuildBulkBanParams{UserIDs: []string{"user"}, DeleteMessageSeconds: -1},
				)
				return err
			},
		},
		{
			name: "excessive bulk ban message seconds",
			call: func(session *Session) error {
				_, err := session.GuildBulkBan(
					"guild",
					&GuildBulkBanParams{UserIDs: []string{"user"}, DeleteMessageSeconds: 604801},
				)
				return err
			},
		},
		{
			name: "nil current user voice state",
			call: func(session *Session) error {
				return session.CurrentUserVoiceStateEdit("guild", nil)
			},
		},
		{
			name: "nil user voice state",
			call: func(session *Session) error {
				return session.UserVoiceStateEdit("guild", "user", nil)
			},
		},
		{
			name: "nil welcome screen",
			call: func(session *Session) error {
				_, err := session.GuildWelcomeScreenEdit("guild", nil)
				return err
			},
		},
		{
			name: "too many welcome channels",
			call: func(session *Session) error {
				_, err := session.GuildWelcomeScreenEdit(
					"guild",
					&GuildWelcomeScreenEditParams{WelcomeChannels: &tooManyChannels},
				)
				return err
			},
		},
		{
			name: "nil incident actions",
			call: func(session *Session) error {
				_, err := session.GuildIncidentActionsEdit("guild", nil)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, err := New("Bot token")
			if err != nil {
				t.Fatal(err)
			}
			session.Client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("validation failure made an HTTP request")
				return nil, nil
			})
			if err := test.call(session); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func readRequestBody(request *http.Request) (string, error) {
	if request.Body == nil {
		return "", nil
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return "", err
	}
	return string(body), request.Body.Close()
}

func assertJSONEqual(t *testing.T, got, want string) {
	t.Helper()
	if got == "" || want == "" {
		if got != want {
			t.Fatalf("body = %q, want %q", got, want)
		}
		return
	}

	var gotJSON interface{}
	if err := json.Unmarshal([]byte(got), &gotJSON); err != nil {
		t.Fatalf("decode body %q: %v", got, err)
	}
	var wantJSON interface{}
	if err := json.Unmarshal([]byte(want), &wantJSON); err != nil {
		t.Fatalf("decode wanted body %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func stringPointer(value string) *string {
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}
