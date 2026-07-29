package dgo

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
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
