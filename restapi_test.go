package dgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

//////////////////////////////////////////////////////////////////////////////
/////////////////////////////////////////////////////////////// START OF TESTS

// TestChannelMessageSend tests the ChannelMessageSend() function. This should not return an error.
func TestChannelMessageSend(t *testing.T) {

	if envChannel == "" {
		t.Skip("Skipping, DG_CHANNEL not set.")
	}

	if dg == nil {
		t.Skip("Skipping, dg not set.")
	}

	_, err := dg.ChannelMessageSend(envChannel, "Running REST API Tests!")
	if err != nil {
		t.Errorf("ChannelMessageSend returned error: %+v", err)
	}
}

/*
// removed for now, only works on BOT accounts now
func TestUserAvatar(t *testing.T) {

	if dg == nil {
		t.Skip("Cannot TestUserAvatar, dg not set.")
	}

	u, err := dg.User("@me")
	if err != nil {
		t.Error("error fetching @me user,", err)
	}

	a, err := dg.UserAvatar(u.ID)
	if err != nil {
		if err.Error() == `HTTP 404 NOT FOUND, {"code": 0, "message": "404: Not Found"}` {
			t.Skip("Skipped, @me doesn't have an Avatar")
		}
		t.Errorf(err.Error())
	}

	if a == nil {
		t.Errorf("a == nil, should be image.Image")
	}
}
*/

/* Running this causes an error due to 2/hour rate limit on username changes
func TestUserUpdate(t *testing.T) {
	if dg == nil {
		t.Skip("Cannot test logout, dg not set.")
	}

	u, err := dg.User("@me")
	if err != nil {
		t.Errorf(err.Error())
	}

	s, err := dg.UserUpdate(envEmail, envPassword, "testname", u.Avatar, "")
	if err != nil {
		t.Error(err.Error())
	}
	if s.Username != "testname" {
		t.Error("Username != testname")
	}
	s, err = dg.UserUpdate(envEmail, envPassword, u.Username, u.Avatar, "")
	if err != nil {
		t.Error(err.Error())
	}
	if s.Username != u.Username {
		t.Error("Username != " + u.Username)
	}
}
*/

//func (s *Session) UserChannelCreate(recipientID string) (st *Channel, err error) {

func TestUserChannelCreate(t *testing.T) {
	if dg == nil {
		t.Skip("Cannot TestUserChannelCreate, dg not set.")
	}

	if envAdmin == "" {
		t.Skip("Skipped, DG_ADMIN not set.")
	}

	_, err := dg.UserChannelCreate(envAdmin)
	if err != nil {
		t.Error(err)
	}

	// TODO make sure the channel was added
}

func TestUserGuilds(t *testing.T) {
	if dg == nil {
		t.Skip("Cannot TestUserGuilds, dg not set.")
	}

	_, err := dg.UserGuilds(10, "", "", false)
	if err != nil {
		t.Error(err)
	}
}

func TestGateway(t *testing.T) {

	if dg == nil {
		t.Skip("Skipping, dg not set.")
	}
	_, err := dg.Gateway()
	if err != nil {
		t.Errorf("Gateway() returned error: %+v", err)
	}
}

func TestGatewayBot(t *testing.T) {

	if dgBot == nil {
		t.Skip("Skipping, dgBot not set.")
	}
	_, err := dgBot.GatewayBot()
	if err != nil {
		t.Errorf("GatewayBot() returned error: %+v", err)
	}
}

func TestVoiceRegions(t *testing.T) {

	if dg == nil {
		t.Skip("Skipping, dg not set.")
	}

	_, err := dg.VoiceRegions()
	if err != nil {
		t.Errorf("VoiceRegions() returned error: %+v", err)
	}
}
func TestGuildRoles(t *testing.T) {

	if envGuild == "" {
		t.Skip("Skipping, DG_GUILD not set.")
	}

	if dg == nil {
		t.Skip("Skipping, dg not set.")
	}

	_, err := dg.GuildRoles(envGuild)
	if err != nil {
		t.Errorf("GuildRoles(envGuild) returned error: %+v", err)
	}

}

func TestGuildMemberNickname(t *testing.T) {

	if envGuild == "" {
		t.Skip("Skipping, DG_GUILD not set.")
	}

	if dg == nil {
		t.Skip("Skipping, dg not set.")
	}

	err := dg.GuildMemberNickname(envGuild, "@me/nick", "B1nzyRocks")
	if err != nil {
		t.Errorf("GuildNickname returned error: %+v", err)
	}
}

// TestChannelMessageSend2 tests the ChannelMessageSend() function. This should not return an error.
func TestChannelMessageSend2(t *testing.T) {

	if envChannel == "" {
		t.Skip("Skipping, DG_CHANNEL not set.")
	}

	if dg == nil {
		t.Skip("Skipping, dg not set.")
	}

	_, err := dg.ChannelMessageSend(envChannel, "All done running REST API Tests!")
	if err != nil {
		t.Errorf("ChannelMessageSend returned error: %+v", err)
	}
}

// TestGuildPruneCount tests GuildPruneCount() function. This should not return an error.
func TestGuildPruneCount(t *testing.T) {

	if envGuild == "" {
		t.Skip("Skipping, DG_GUILD not set.")
	}

	if dg == nil {
		t.Skip("Skipping, dg not set.")
	}

	_, err := dg.GuildPruneCount(envGuild, 1)
	if err != nil {
		t.Errorf("GuildPruneCount returned error: %+v", err)
	}
}

/*
// TestGuildPrune tests GuildPrune() function. This should not return an error.
func TestGuildPrune(t *testing.T) {

	if envGuild == "" {
		t.Skip("Skipping, DG_GUILD not set.")
	}

	if dg == nil {
		t.Skip("Skipping, dg not set.")
	}

	_, err := dg.GuildPrune(envGuild, 1)
	if err != nil {
		t.Errorf("GuildPrune returned error: %+v", err)
	}
}
*/

func Test_unmarshal(t *testing.T) {
	err := unmarshal([]byte{}, &struct{}{})
	if !errors.Is(err, ErrJSONUnmarshal) {
		t.Errorf("Unexpected error type: %T", err)
	}
}

func TestInviteAcceptIsUnsupported(t *testing.T) {
	session, err := New("Bot test-token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("InviteAccept must not call Discord's private invite acceptance route")
		return nil, nil
	})

	invite, err := session.InviteAccept("invite-code")
	if invite != nil {
		t.Fatalf("InviteAccept returned an invite: %#v", invite)
	}
	if !errors.Is(err, ErrInviteAcceptUnsupported) {
		t.Fatalf("InviteAccept error = %v, want %v", err, ErrInviteAcceptUnsupported)
	}
}

func TestRESTDebugLogRedactsCredentials(t *testing.T) {
	session, err := New("Bot bot-secret")
	if err != nil {
		t.Fatal(err)
	}
	session.Debug = true
	session.Client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Status:     "204 No Content",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	})

	previousWriter := log.Writer()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(previousWriter)

	_, err = session.Request(
		http.MethodPost,
		"https://discord.com/api/v10/webhooks/123/webhook-secret",
		map[string]string{"token": "payload-secret"},
	)
	if err != nil {
		t.Fatal(err)
	}
	got := logs.String()
	for _, secret := range []string{"bot-secret", "webhook-secret", "payload-secret"} {
		if strings.Contains(got, secret) {
			t.Fatalf("REST debug log leaked %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("REST debug log has no redaction marker: %s", got)
	}
}

func TestEntitlementPaginationUsesSnowflakes(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("before") != "100" || query.Get("after") != "200" {
			t.Errorf("pagination query = %q", request.URL.RawQuery)
		}
		if query.Get("exclude_deleted") != "true" {
			t.Errorf("exclude_deleted = %q, want true", query.Get("exclude_deleted"))
		}
		return jsonResponse(http.StatusOK, `[]`), nil
	})

	_, err = session.Entitlements("app", &EntitlementFilterOptions{
		Before:         "100",
		After:          "200",
		ExcludeDeleted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionPaginationUsesSnowflakes(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("before") != "300" || query.Get("after") != "400" {
			t.Errorf("pagination query = %q", request.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, `[]`), nil
	})

	if _, err = session.Subscriptions("sku", "user", "300", "400", 50); err != nil {
		t.Fatal(err)
	}
}

func TestJoinedPrivateArchivedThreadsUseSnowflakeBefore(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if got := request.URL.Query().Get("before"); got != "500" {
			t.Errorf("before = %q, want 500", got)
		}
		return jsonResponse(http.StatusOK, `{"threads":[],"members":[],"has_more":false}`), nil
	})

	if _, err = session.ThreadsPrivateJoinedArchived("channel", "500", 25); err != nil {
		t.Fatal(err)
	}
}

func TestChannelMessagesPinsUsesCurrentPaginatedRoute(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	before := time.Date(2026, time.July, 29, 12, 30, 0, 0, time.UTC)
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v10/channels/channel/messages/pins" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if got := request.URL.Query().Get("before"); got != before.Format(time.RFC3339) {
			t.Errorf("before = %q", got)
		}
		if got := request.URL.Query().Get("limit"); got != "25" {
			t.Errorf("limit = %q", got)
		}
		return jsonResponse(http.StatusOK, `{
			"items":[{"pinned_at":"2026-07-29T12:00:00Z","message":{"id":"message"}}],
			"has_more":true
		}`), nil
	})

	pins, err := session.ChannelMessagesPins("channel", &before, 25)
	if err != nil {
		t.Fatal(err)
	}
	if !pins.HasMore || len(pins.Items) != 1 || pins.Items[0].Message.ID != "message" {
		t.Fatalf("pins = %#v", pins)
	}
}

func TestChannelMessagePinUsesCurrentRoute(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path != "/api/v10/channels/channel/messages/pins/message" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Method != http.MethodPut && request.Method != http.MethodDelete {
			t.Errorf("method = %q", request.Method)
		}
		return jsonResponse(http.StatusNoContent, ``), nil
	})

	if err = session.ChannelMessagePin("channel", "message"); err != nil {
		t.Fatal(err)
	}
	if err = session.ChannelMessageUnpin("channel", "message"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestChannelVoiceStatusUpdate(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != http.MethodPut {
			t.Errorf("method = %q", request.Method)
		}
		if request.URL.Path != "/api/v10/channels/channel/voice-status" {
			t.Errorf("path = %q", request.URL.Path)
		}
		body, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		want := `{"status":"Town hall"}`
		if requests == 2 {
			want = `{"status":null}`
		}
		if string(body) != want {
			t.Errorf("body = %s, want %s", body, want)
		}
		return jsonResponse(http.StatusNoContent, ``), nil
	})

	status := "Town hall"
	if err = session.ChannelVoiceStatusUpdate("channel", &status); err != nil {
		t.Fatal(err)
	}
	if err = session.ChannelVoiceStatusUpdate("channel", nil); err != nil {
		t.Fatal(err)
	}

	tooLong := strings.Repeat("声", 501)
	if err = session.ChannelVoiceStatusUpdate("channel", &tooLong); err == nil {
		t.Fatal("accepted voice channel status longer than 500 characters")
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestApplicationActivityInstance(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodGet {
			t.Errorf("method = %q", request.Method)
		}
		if request.URL.Path != "/api/v10/applications/app/activity-instances/i-123" {
			t.Errorf("path = %q", request.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{
			"application_id":"app",
			"instance_id":"i-123",
			"launch_id":"launch",
			"location":{"id":"gc-1-2","kind":"gc","channel_id":"2","guild_id":"1"},
			"users":["user"]
		}`), nil
	})

	instance, err := session.ApplicationActivityInstance("app", "i-123")
	if err != nil {
		t.Fatal(err)
	}
	if instance.InstanceID != "i-123" || instance.Location.Kind != ApplicationActivityLocationGuildChannel {
		t.Fatalf("instance = %#v", instance)
	}
	if instance.Location.GuildID == nil || *instance.Location.GuildID != "1" {
		t.Fatalf("guild ID = %#v", instance.Location.GuildID)
	}
}

func TestPrimaryEntryPointAndLaunchActivityPayloads(t *testing.T) {
	handler := ApplicationCommandHandlerDiscordLaunchActivity
	command := &ApplicationCommand{
		Type:        PrimaryEntryPointApplicationCommand,
		Name:        "launch",
		Description: "Launch the Activity",
		Handler:     &handler,
	}
	commandJSON, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(commandJSON), `"type":4`) || !strings.Contains(string(commandJSON), `"handler":2`) {
		t.Fatalf("command JSON = %s", commandJSON)
	}

	responseJSON, err := json.Marshal(&InteractionResponse{Type: InteractionResponseLaunchActivity})
	if err != nil {
		t.Fatal(err)
	}
	if string(responseJSON) != `{"type":12}` {
		t.Fatalf("response JSON = %s", responseJSON)
	}
}

func TestGuildMessagesSearch(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	slop := 0
	pinned := false
	includeNSFW := true
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v10/guilds/guild/messages/search" {
			t.Errorf("path = %q", request.URL.Path)
		}
		query := request.URL.Query()
		for key, want := range map[string]string{
			"limit":            "10",
			"offset":           "20",
			"max_id":           "900",
			"min_id":           "100",
			"slop":             "0",
			"content":          "release notes",
			"pinned":           "false",
			"sort_by":          "relevance",
			"sort_order":       "asc",
			"include_nsfw":     "true",
			"author_type":      "bot",
			"has":              "file",
			"embed_type":       "article",
			"mention_everyone": "",
		} {
			if got := query.Get(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		if got := query["channel_id"]; !reflect.DeepEqual(got, []string{"channel-1", "channel-2"}) {
			t.Errorf("channel_id = %#v", got)
		}
		return jsonResponse(http.StatusOK, `{
			"doing_deep_historical_index":true,
			"documents_indexed":9,
			"total_results":1,
			"messages":[[{"id":"message"}]],
			"threads":[{"id":"thread"}],
			"members":[{"id":"thread","user_id":"user"}]
		}`), nil
	})

	result, err := session.GuildMessagesSearch("guild", &GuildMessageSearchParams{
		Limit:       10,
		Offset:      20,
		MaxID:       "900",
		MinID:       "100",
		Slop:        &slop,
		Content:     "release notes",
		ChannelIDs:  []string{"channel-1", "channel-2"},
		AuthorTypes: []GuildMessageSearchAuthorType{GuildMessageSearchAuthorBot},
		Pinned:      &pinned,
		Has:         []GuildMessageSearchHasType{GuildMessageSearchHasFile},
		EmbedTypes:  []GuildMessageSearchEmbedType{GuildMessageSearchEmbedArticle},
		SortBy:      GuildMessageSearchSortRelevance,
		SortOrder:   GuildMessageSearchSortAscending,
		IncludeNSFW: &includeNSFW,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalResults != 1 || len(result.Messages) != 1 || result.Messages[0][0].ID != "message" {
		t.Fatalf("result = %#v", result)
	}
	if result.DocumentsIndexed == nil || *result.DocumentsIndexed != 9 {
		t.Fatalf("documents indexed = %#v", result.DocumentsIndexed)
	}
}

func TestGuildMessagesSearchIndexPending(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusAccepted, `{
			"message":"Index not yet available. Try again later",
			"code":110000,
			"documents_indexed":42,
			"retry_after":1.5
		}`), nil
	})

	result, err := session.GuildMessagesSearch("guild", nil)
	if result != nil {
		t.Fatalf("result = %#v, want nil", result)
	}
	var pending *GuildMessageSearchIndexingError
	if !errors.As(err, &pending) {
		t.Fatalf("error = %T %v, want GuildMessageSearchIndexingError", err, err)
	}
	if pending.DocumentsIndexed != 42 || pending.RetryAfter != 1500*time.Millisecond {
		t.Fatalf("pending = %#v", pending)
	}
}

func TestRemovedPublicAPIRoutesFailWithoutRequests(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatalf("removed API helper made request to %s", request.URL)
		return nil, nil
	})

	if _, err = session.GuildCreate("guild"); !errors.Is(err, ErrGuildCreateUnsupported) {
		t.Fatalf("GuildCreate error = %v", err)
	}
	if _, err = session.GuildCreateWithTemplate("template", "guild", ""); !errors.Is(err, ErrGuildCreateUnsupported) {
		t.Fatalf("GuildCreateWithTemplate error = %v", err)
	}
	if _, err = session.ThreadsActive("channel"); !errors.Is(err, ErrChannelActiveThreadsUnsupported) {
		t.Fatalf("ThreadsActive error = %v", err)
	}
	if err = session.GuildIntegrationCreate("guild", "type", "integration"); !errors.Is(err, ErrGuildIntegrationMutationUnsupported) {
		t.Fatalf("GuildIntegrationCreate error = %v", err)
	}
	if err = session.GuildIntegrationEdit("guild", "integration", 0, 0, false); !errors.Is(err, ErrGuildIntegrationMutationUnsupported) {
		t.Fatalf("GuildIntegrationEdit error = %v", err)
	}
	if err = session.ApplicationCommandPermissionsBatchEdit("app", "guild", nil); !errors.Is(err, ErrCommandPermissionsBatchUnsupported) {
		t.Fatalf("ApplicationCommandPermissionsBatchEdit error = %v", err)
	}
	if _, err = session.Application("app"); !errors.Is(err, ErrOAuthApplicationCRUDUnsupported) {
		t.Fatalf("Application error = %v", err)
	}
	if _, err = session.Applications(); !errors.Is(err, ErrOAuthApplicationCRUDUnsupported) {
		t.Fatalf("Applications error = %v", err)
	}
	if _, err = session.ApplicationCreate(&Application{}); !errors.Is(err, ErrOAuthApplicationCRUDUnsupported) {
		t.Fatalf("ApplicationCreate error = %v", err)
	}
	if _, err = session.ApplicationUpdate("app", &Application{}); !errors.Is(err, ErrOAuthApplicationCRUDUnsupported) {
		t.Fatalf("ApplicationUpdate error = %v", err)
	}
	if err = session.ApplicationDelete("app"); !errors.Is(err, ErrOAuthApplicationCRUDUnsupported) {
		t.Fatalf("ApplicationDelete error = %v", err)
	}
}

func TestCurrentApplicationHelpers(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Path != "/api/v10/applications/@me" {
			t.Errorf("path = %q", request.URL.Path)
		}
		switch requests {
		case 1:
			if request.Method != http.MethodGet {
				t.Errorf("method = %q, want GET", request.Method)
			}
			return jsonResponse(http.StatusOK, `{"id":"app","name":"before"}`), nil
		case 2:
			if request.Method != http.MethodPatch {
				t.Errorf("method = %q, want PATCH", request.Method)
			}
			body, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(body), `"description":"updated"`) {
				t.Errorf("body = %s", body)
			}
			return jsonResponse(http.StatusOK, `{"id":"app","name":"after","description":"updated"}`), nil
		default:
			t.Fatalf("unexpected request %d", requests)
			return nil, nil
		}
	})

	application, err := session.CurrentApplication()
	if err != nil {
		t.Fatal(err)
	}
	if application.ID != "app" || application.Name != "before" {
		t.Fatalf("application = %#v", application)
	}
	description := "updated"
	application, err = session.CurrentApplicationEdit(&ApplicationEdit{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	if application.Description != "updated" {
		t.Fatalf("updated application = %#v", application)
	}
}

func TestGuildTemplateCreateReturnsErrors(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	session.Client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		requests++
		switch requests {
		case 1:
			return jsonResponse(http.StatusOK, `{"code":"template"}`), nil
		case 2:
			return jsonResponse(http.StatusBadRequest, `{"message":"invalid","code":50035}`), nil
		case 3:
			return jsonResponse(http.StatusOK, `{`), nil
		default:
			t.Fatalf("unexpected request %d", requests)
			return nil, nil
		}
	})

	template, err := session.GuildTemplateCreate("guild", &GuildTemplateParams{Name: "Template"})
	if err != nil {
		t.Fatal(err)
	}
	if template.Code != "template" {
		t.Fatalf("template = %#v", template)
	}
	if _, err = session.GuildTemplateCreate("guild", &GuildTemplateParams{Name: ""}); err == nil {
		t.Fatal("HTTP error was discarded")
	}
	if _, err = session.GuildTemplateCreate("guild", &GuildTemplateParams{Name: "Template"}); !errors.Is(err, ErrJSONUnmarshal) {
		t.Fatalf("unmarshal error = %v, want ErrJSONUnmarshal", err)
	}
}

func TestEntitlementHelpersReturnResponses(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		switch request.Method {
		case http.MethodGet:
			return jsonResponse(http.StatusOK, `{"id":"existing"}`), nil
		case http.MethodPost:
			return jsonResponse(http.StatusOK, `{"id":"created"}`), nil
		default:
			t.Fatalf("unexpected method %s", request.Method)
			return nil, nil
		}
	})

	entitlement, err := session.Entitlement("app", "existing")
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.ID != "existing" {
		t.Fatalf("Entitlement ID = %q", entitlement.ID)
	}

	entitlement, err = session.EntitlementTestCreate("app", &EntitlementTest{SKUID: "sku"})
	if err != nil {
		t.Fatal(err)
	}
	if entitlement.ID != "created" {
		t.Fatalf("created entitlement ID = %q", entitlement.ID)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestGuildRoleColors(t *testing.T) {
	var role Role
	err := json.Unmarshal([]byte(`{
		"id":"role",
		"color":11127295,
		"colors":{
			"primary_color":11127295,
			"secondary_color":16759788,
			"tertiary_color":16761760
		}
	}`), &role)
	if err != nil {
		t.Fatal(err)
	}
	if role.Colors.PrimaryColor != RoleHolographicPrimaryColor ||
		role.Colors.SecondaryColor == nil ||
		*role.Colors.SecondaryColor != RoleHolographicSecondaryColor ||
		role.Colors.TertiaryColor == nil ||
		*role.Colors.TertiaryColor != RoleHolographicTertiaryColor {
		t.Fatalf("unexpected role colors: %#v", role.Colors)
	}
}

func TestGuildRoleCreateAndEditSendColors(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}

	requests := 0
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		var params RoleParams
		if err := json.NewDecoder(request.Body).Decode(&params); err != nil {
			t.Fatal(err)
		}
		if params.Colors == nil ||
			params.Colors.PrimaryColor != 0x123456 ||
			params.Colors.SecondaryColor == nil ||
			*params.Colors.SecondaryColor != 0x654321 {
			t.Fatalf("unexpected role params: %#v", params)
		}
		return jsonResponse(http.StatusOK, `{"id":"role","colors":{"primary_color":1193046,"secondary_color":6636321,"tertiary_color":null}}`), nil
	})

	secondary := 0x654321
	params := &RoleParams{Colors: &RoleColors{
		PrimaryColor:   0x123456,
		SecondaryColor: &secondary,
	}}
	if _, err := session.GuildRoleCreate("guild", params); err != nil {
		t.Fatal(err)
	}
	if _, err := session.GuildRoleEdit("guild", "role", params); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestGuildRoleColorsValidation(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatal("validation failure must not make a request")
		return nil, nil
	})

	invalid := -1
	if _, err := session.GuildRoleCreate("guild", &RoleParams{Color: &invalid}); err == nil {
		t.Fatal("expected legacy color validation error")
	}
	tooLarge := 0x1000000
	if _, err := session.GuildRoleEdit("guild", "role", &RoleParams{
		Colors: &RoleColors{PrimaryColor: tooLarge},
	}); err == nil {
		t.Fatal("expected primary color validation error")
	}
	secondary := 0x654321
	tertiary := 0xabcdef
	if _, err := session.GuildRoleCreate("guild", &RoleParams{
		Colors: &RoleColors{
			PrimaryColor:   0x123456,
			SecondaryColor: &secondary,
			TertiaryColor:  &tertiary,
		},
	}); err == nil {
		t.Fatal("expected holographic preset validation error")
	}
}

func TestMessageReactionsComplexIncludesReactionType(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		query := request.URL.Query()
		if query.Get("type") != "1" ||
			query.Get("limit") != "50" ||
			query.Get("after") != "user" {
			t.Fatalf("unexpected reaction query: %s", request.URL.RawQuery)
		}
		return jsonResponse(http.StatusOK, `[{"id":"reactor"}]`), nil
	})

	users, err := session.MessageReactionsComplex("channel", "message", "🔥", &MessageReactionsParams{
		Type:    ReactionTypeBurst,
		Limit:   50,
		AfterID: "user",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].ID != "reactor" {
		t.Fatalf("unexpected users: %#v", users)
	}
}

func TestMessageReactionsComplexRejectsInvalidParams(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		t.Fatal("invalid reaction parameters must not make a request")
		return nil, nil
	})

	if _, err := session.MessageReactionsComplex("channel", "message", "emoji", &MessageReactionsParams{
		Type: 2,
	}); err == nil {
		t.Fatal("expected reaction type validation error")
	}
	if _, err := session.MessageReactionsComplex("channel", "message", "emoji", &MessageReactionsParams{
		Limit: 101,
	}); err == nil {
		t.Fatal("expected reaction limit validation error")
	}
}

func TestWithContext(t *testing.T) {
	// Set up a test context.
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "value")

	// Set up a test client.
	session, err := New("")
	if err != nil {
		t.Fatal(err)
	}

	testErr := errors.New("test")

	// Intercept the request to assert the context.
	session.Client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		val, _ := r.Context().Value(key{}).(string)
		if val != "value" {
			t.Errorf("missing value in context (got %q, wanted %q)", val, "value")
		}
		return nil, testErr
	})

	// Run any client method using WithContext.
	_, err = session.User("", WithContext(ctx))

	// Verify that the assertion code was actually run.
	if !errors.Is(err, testErr) {
		t.Errorf("unexpected error %v returned from client", err)
	}
}

// roundTripperFunc implements http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
