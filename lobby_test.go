package dgo

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type lobbyRoundTripper func(*http.Request) (*http.Response, error)

func (f lobbyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type lobbyRequestExpectation struct {
	method        string
	path          string
	query         string
	authorization string
	body          string
	response      string
}

func newLobbyTestSession(t *testing.T, expectations ...lobbyRequestExpectation) *Session {
	t.Helper()

	session, err := New("Bot session-token")
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	next := 0
	session.Client.Transport = lobbyRoundTripper(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()

		if next >= len(expectations) {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL)
			return nil, errors.New("unexpected lobby request")
		}
		expectation := expectations[next]
		next++

		if request.Method != expectation.method {
			t.Errorf("request method = %q, want %q", request.Method, expectation.method)
		}
		if request.URL.Path != expectation.path {
			t.Errorf("request path = %q, want %q", request.URL.Path, expectation.path)
		}
		if request.URL.RawQuery != expectation.query {
			t.Errorf("request query = %q, want %q", request.URL.RawQuery, expectation.query)
		}
		if got := request.Header.Get("Authorization"); got != expectation.authorization {
			t.Errorf("authorization = %q, want %q", got, expectation.authorization)
		}
		var body []byte
		if request.Body != nil {
			readBody, readErr := io.ReadAll(request.Body)
			if readErr != nil {
				t.Errorf("read request body: %v", readErr)
			}
			body = readBody
		}
		if string(body) != expectation.body {
			t.Errorf("request body = %s, want %s", body, expectation.body)
		}

		status := http.StatusOK
		if expectation.response == "" {
			status = http.StatusNoContent
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(expectation.response)),
			Request:    request,
		}, nil
	})

	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if next != len(expectations) {
			t.Errorf("made %d lobby requests, want %d", next, len(expectations))
		}
	})
	return session
}

func TestLobbyBotEndpoints(t *testing.T) {
	lobbyJSON := `{"id":"l1","application_id":"app","metadata":null,"members":[],"flags":0}`
	memberJSON := `{"id":"u1","metadata":null,"flags":0}`
	session := newLobbyTestSession(t,
		lobbyRequestExpectation{
			method:        http.MethodPost,
			path:          "/api/v" + APIVersion + "/lobbies",
			authorization: "Bot session-token",
			body:          `{"metadata":{"mode":"ranked"},"members":[{"id":"u1"}],"idle_timeout_seconds":60}`,
			response:      lobbyJSON,
		},
		lobbyRequestExpectation{
			method:        http.MethodGet,
			path:          "/api/v" + APIVersion + "/lobbies/l1",
			authorization: "Bot session-token",
			response:      lobbyJSON,
		},
		lobbyRequestExpectation{
			method:        http.MethodPatch,
			path:          "/api/v" + APIVersion + "/lobbies/l1",
			authorization: "Bot session-token",
			body:          `{"metadata":null,"members":[]}`,
			response:      lobbyJSON,
		},
		lobbyRequestExpectation{
			method:        http.MethodPut,
			path:          "/api/v" + APIVersion + "/lobbies/l1/members/u1",
			authorization: "Bot session-token",
			body:          `{"additional_name":"Player One"}`,
			response:      memberJSON,
		},
		lobbyRequestExpectation{
			method:        http.MethodPost,
			path:          "/api/v" + APIVersion + "/lobbies/l1/members/bulk",
			authorization: "Bot session-token",
			body:          `[{"id":"u1"},{"id":"u2","remove_member":true}]`,
			response:      `[` + memberJSON + `]`,
		},
		lobbyRequestExpectation{
			method:        http.MethodDelete,
			path:          "/api/v" + APIVersion + "/lobbies/l1/members/u2",
			authorization: "Bot session-token",
		},
		lobbyRequestExpectation{
			method:        http.MethodPut,
			path:          "/api/v" + APIVersion + "/lobbies/l1/messages/m1/moderation-metadata",
			authorization: "Bot session-token",
			body:          `{"decision":"allow"}`,
		},
		lobbyRequestExpectation{
			method:        http.MethodPost,
			path:          "/api/v" + APIVersion + "/lobbies/l1/members/u1/invites",
			authorization: "Bot session-token",
			response:      `{"code":"invite"}`,
		},
		lobbyRequestExpectation{
			method:        http.MethodDelete,
			path:          "/api/v" + APIVersion + "/lobbies/l1",
			authorization: "Bot session-token",
		},
	)

	metadata := map[string]string{"mode": "ranked"}
	members := []LobbyMemberParams{{ID: "u1"}}
	timeout := 60
	created, err := session.LobbyCreate(LobbyCreateParams{
		Metadata:           &metadata,
		Members:            &members,
		IdleTimeoutSeconds: &timeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "l1" {
		t.Fatalf("created lobby ID = %q, want l1", created.ID)
	}

	if _, err = session.Lobby("l1"); err != nil {
		t.Fatal(err)
	}

	var nullMetadata map[string]string
	emptyMembers := []LobbyMemberParams{}
	if _, err = session.LobbyEdit("l1", LobbyEditParams{
		Metadata: &nullMetadata,
		Members:  &emptyMembers,
	}); err != nil {
		t.Fatal(err)
	}

	additionalName := "Player One"
	if _, err = session.LobbyMemberAdd("l1", "u1", LobbyMemberUpdateParams{
		AdditionalName: &additionalName,
	}); err != nil {
		t.Fatal(err)
	}

	remove := true
	updated, err := session.LobbyMembersBulkUpdate("l1", []LobbyMemberBulkUpdateParams{
		{ID: "u1"},
		{ID: "u2", RemoveMember: &remove},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated) != 1 || updated[0].ID != "u1" {
		t.Fatalf("updated members = %#v", updated)
	}

	if err = session.LobbyMemberDelete("l1", "u2"); err != nil {
		t.Fatal(err)
	}
	if err = session.LobbyMessageModerationMetadataUpdate(
		"l1",
		"m1",
		map[string]string{"decision": "allow"},
	); err != nil {
		t.Fatal(err)
	}
	invite, err := session.LobbyChannelInviteCreateForUser("l1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	if invite.Code != "invite" {
		t.Fatalf("invite code = %q, want invite", invite.Code)
	}
	if err = session.LobbyDelete("l1"); err != nil {
		t.Fatal(err)
	}
}

func TestLobbyBearerEndpoints(t *testing.T) {
	lobbyJSON := `{"id":"l1","application_id":"app","metadata":null,"members":[],"flags":0}`
	messageJSON := `{"id":"m1","type":0,"content":"hello","lobby_id":"l1","channel_id":"l1","author":{"id":"u1"},"flags":0}`
	session := newLobbyTestSession(t,
		lobbyRequestExpectation{
			method:        http.MethodPut,
			path:          "/api/v" + APIVersion + "/lobbies",
			authorization: "Bearer user-access-token",
			body:          `{"secret":"match-secret","lobby_metadata":{"mode":"ranked"}}`,
			response:      lobbyJSON,
		},
		lobbyRequestExpectation{
			method:        http.MethodPatch,
			path:          "/api/v" + APIVersion + "/lobbies/l1/channel-linking",
			authorization: "Bearer user-access-token",
			body:          `{"channel_id":"c1"}`,
			response:      lobbyJSON,
		},
		lobbyRequestExpectation{
			method:        http.MethodPatch,
			path:          "/api/v" + APIVersion + "/lobbies/l1/channel-linking",
			authorization: "Bearer user-access-token",
			body:          `{}`,
			response:      lobbyJSON,
		},
		lobbyRequestExpectation{
			method:        http.MethodPost,
			path:          "/api/v" + APIVersion + "/lobbies/l1/messages",
			authorization: "Bearer user-access-token",
			body:          `{"content":"hello","metadata":{"round":"1"}}`,
			response:      messageJSON,
		},
		lobbyRequestExpectation{
			method:        http.MethodGet,
			path:          "/api/v" + APIVersion + "/lobbies/l1/messages",
			query:         "limit=25",
			authorization: "Bearer user-access-token",
			response:      `[` + messageJSON + `]`,
		},
		lobbyRequestExpectation{
			method:        http.MethodPost,
			path:          "/api/v" + APIVersion + "/lobbies/l1/members/@me/invites",
			authorization: "Bearer user-access-token",
			response:      `{"code":"self-invite"}`,
		},
		lobbyRequestExpectation{
			method:        http.MethodDelete,
			path:          "/api/v" + APIVersion + "/lobbies/l1/members/@me",
			authorization: "Bearer user-access-token",
		},
	)

	metadata := map[string]string{"mode": "ranked"}
	lobby, err := session.LobbyCreateOrJoin(
		"user-access-token",
		LobbyCreateOrJoinParams{Secret: "match-secret", LobbyMetadata: &metadata},
		WithHeader("Authorization", "Bot must-not-win"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if lobby.ID != "l1" {
		t.Fatalf("lobby ID = %q, want l1", lobby.ID)
	}
	if _, err = session.LobbyChannelLink("user-access-token", "l1", "c1"); err != nil {
		t.Fatal(err)
	}
	if _, err = session.LobbyChannelUnlink("user-access-token", "l1"); err != nil {
		t.Fatal(err)
	}

	messageMetadata := map[string]string{"round": "1"}
	message, err := session.LobbyMessageSend(
		"user-access-token",
		"l1",
		LobbyMessageSendParams{Content: "hello", Metadata: &messageMetadata},
	)
	if err != nil {
		t.Fatal(err)
	}
	if message.ID != "m1" || message.Author == nil || message.Author.ID != "u1" {
		t.Fatalf("message = %#v", message)
	}
	messages, err := session.LobbyMessages("user-access-token", "l1", 25)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "m1" {
		t.Fatalf("messages = %#v", messages)
	}
	invite, err := session.LobbyChannelInviteCreateForSelf("user-access-token", "l1")
	if err != nil {
		t.Fatal(err)
	}
	if invite.Code != "self-invite" {
		t.Fatalf("invite code = %q", invite.Code)
	}
	if err = session.LobbyLeave("user-access-token", "l1"); err != nil {
		t.Fatal(err)
	}
}

func TestLobbyValidationRejectsBeforeRequest(t *testing.T) {
	session, err := New("Bot session-token")
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	session.Client.Transport = lobbyRoundTripper(func(request *http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("request must not be sent")
	})

	tooLongMetadata := map[string]string{"key": strings.Repeat("x", lobbyMetadataMaxLength)}
	tooManyMembers := make([]LobbyMemberParams, lobbyMemberMaximum+1)
	for index := range tooManyMembers {
		tooManyMembers[index].ID = string(rune('A' + index))
	}
	invalidTimeout := lobbyIdleTimeoutMinimum - 1
	invalidFlags := LobbyMemberFlags(1 << 7)
	tooLongSecret := strings.Repeat("x", lobbySecretMaxLength+1)
	tooLongMessage := strings.Repeat("x", lobbyMessageMaxLength+1)
	emptyAdditionalName := ""

	tests := []struct {
		name string
		call func() error
		want error
	}{
		{
			name: "empty lobby id",
			call: func() error {
				_, callErr := session.Lobby("")
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "metadata total length",
			call: func() error {
				_, callErr := session.LobbyCreate(LobbyCreateParams{Metadata: &tooLongMetadata})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "member maximum",
			call: func() error {
				_, callErr := session.LobbyCreate(LobbyCreateParams{Members: &tooManyMembers})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "duplicate member",
			call: func() error {
				members := []LobbyMemberParams{{ID: "u1"}, {ID: "u1"}}
				_, callErr := session.LobbyCreate(LobbyCreateParams{Members: &members})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "idle timeout",
			call: func() error {
				_, callErr := session.LobbyCreate(LobbyCreateParams{IdleTimeoutSeconds: &invalidTimeout})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "unsupported member flag",
			call: func() error {
				_, callErr := session.LobbyMemberAdd("l1", "u1", LobbyMemberUpdateParams{Flags: &invalidFlags})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "empty additional name",
			call: func() error {
				_, callErr := session.LobbyMemberAdd("l1", "u1", LobbyMemberUpdateParams{AdditionalName: &emptyAdditionalName})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "empty bulk members",
			call: func() error {
				_, callErr := session.LobbyMembersBulkUpdate("l1", nil)
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "secret too long",
			call: func() error {
				_, callErr := session.LobbyCreateOrJoin("token", LobbyCreateOrJoinParams{Secret: tooLongSecret})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "prefixed bearer token",
			call: func() error {
				_, callErr := session.LobbyCreateOrJoin("Bearer token", LobbyCreateOrJoinParams{Secret: "secret"})
				return callErr
			},
			want: ErrLobbyBearerToken,
		},
		{
			name: "bot token on user route",
			call: func() error {
				return session.LobbyLeave("Bot token", "l1")
			},
			want: ErrLobbyBearerToken,
		},
		{
			name: "empty message",
			call: func() error {
				_, callErr := session.LobbyMessageSend("token", "l1", LobbyMessageSendParams{})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "message too long",
			call: func() error {
				_, callErr := session.LobbyMessageSend("token", "l1", LobbyMessageSendParams{Content: tooLongMessage})
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "message limit",
			call: func() error {
				_, callErr := session.LobbyMessages("token", "l1", lobbyMessageLimitMaximum+1)
				return callErr
			},
			want: ErrLobbyValidation,
		},
		{
			name: "moderation entry maximum",
			call: func() error {
				metadata := make(map[string]string, lobbyModerationMaxEntries+1)
				for index := 0; index <= lobbyModerationMaxEntries; index++ {
					metadata[string(rune('a'+index))] = "value"
				}
				return session.LobbyMessageModerationMetadataUpdate("l1", "m1", metadata)
			},
			want: ErrLobbyValidation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if callErr := test.call(); !errors.Is(callErr, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", callErr, test.want)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("sent %d requests for invalid payloads", requests)
	}
}

func TestLobbyDecodeErrorsWrapErrJSONUnmarshal(t *testing.T) {
	session := newLobbyTestSession(t, lobbyRequestExpectation{
		method:        http.MethodGet,
		path:          "/api/v" + APIVersion + "/lobbies/l1",
		authorization: "Bot session-token",
		response:      `{`,
	})
	_, err := session.Lobby("l1")
	if !errors.Is(err, ErrJSONUnmarshal) {
		t.Fatalf("error = %v, want ErrJSONUnmarshal", err)
	}
}

func TestLobbyBearerOptionsDoNotMutateCallerSlice(t *testing.T) {
	var configured bytes.Buffer
	option := func(config *RequestConfig) {
		configured.WriteString(config.Request.Header.Get("Authorization"))
	}
	original := []RequestOption{option}
	result, err := lobbyBearerOptions("token", original)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) != 1 || len(result) != 2 {
		t.Fatalf("option lengths = %d, %d", len(original), len(result))
	}
}
