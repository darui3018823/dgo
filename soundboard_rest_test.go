package dgo

import (
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type soundboardRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip soundboardRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type soundboardRequestExpectation struct {
	method        string
	path          string
	status        int
	authorization string
	contentType   string
	auditReason   string
	body          string
	response      string
}

func newSoundboardTestSession(t *testing.T, expectations ...soundboardRequestExpectation) *Session {
	t.Helper()

	session, err := New("Bot soundboard-token")
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	next := 0
	session.Client.Transport = soundboardRoundTripper(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()

		if next >= len(expectations) {
			t.Errorf("unexpected request: %s %s", request.Method, request.URL)
			return nil, errors.New("unexpected soundboard request")
		}
		expectation := expectations[next]
		next++

		if request.Method != expectation.method {
			t.Errorf("request method = %q, want %q", request.Method, expectation.method)
		}
		if request.URL.Path != expectation.path {
			t.Errorf("request path = %q, want %q", request.URL.Path, expectation.path)
		}
		if request.URL.RawQuery != "" {
			t.Errorf("request query = %q, want empty", request.URL.RawQuery)
		}
		if got := request.Header.Get("Authorization"); got != expectation.authorization {
			t.Errorf("Authorization = %q, want %q", got, expectation.authorization)
		}
		if got := request.Header.Get("Content-Type"); got != expectation.contentType {
			t.Errorf("Content-Type = %q, want %q", got, expectation.contentType)
		}
		if got := request.Header.Get("X-Audit-Log-Reason"); got != expectation.auditReason {
			t.Errorf("X-Audit-Log-Reason = %q, want %q", got, expectation.auditReason)
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

		status := expectation.status
		if status == 0 {
			status = http.StatusOK
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
			t.Errorf("made %d soundboard requests, want %d", next, len(expectations))
		}
	})
	return session
}

func TestSoundboardRESTEndpoints(t *testing.T) {
	const (
		guildID   = "613425648685547541"
		soundID   = "1106714396018884649"
		channelID = "81384788765712384"
	)
	defaultSound := `{"name":"quack","sound_id":"1","volume":1,"emoji_id":null,"emoji_name":"🦆","available":true}`
	guildSound := `{"name":"Yay","sound_id":"1106714396018884649","volume":1,"emoji_id":"989193655938064464","emoji_name":null,"guild_id":"613425648685547541","available":true,"user":{"id":"100000000000000001"}}`
	editedSound := `{"name":"Yay","sound_id":"1106714396018884649","volume":1,"emoji_id":null,"emoji_name":"🎉","guild_id":"613425648685547541","available":true}`

	session := newSoundboardTestSession(t,
		soundboardRequestExpectation{
			method:        http.MethodGet,
			path:          "/api/v" + APIVersion + "/soundboard-default-sounds",
			authorization: "Bot soundboard-token",
			response:      "[" + defaultSound + "]",
		},
		soundboardRequestExpectation{
			method:        http.MethodGet,
			path:          "/api/v" + APIVersion + "/guilds/" + guildID + "/soundboard-sounds",
			authorization: "Bot soundboard-token",
			response:      `{"items":[` + guildSound + `]}`,
		},
		soundboardRequestExpectation{
			method:        http.MethodGet,
			path:          "/api/v" + APIVersion + "/guilds/" + guildID + "/soundboard-sounds/" + soundID,
			authorization: "Bot soundboard-token",
			response:      guildSound,
		},
		soundboardRequestExpectation{
			method:        http.MethodPost,
			path:          "/api/v" + APIVersion + "/guilds/" + guildID + "/soundboard-sounds",
			status:        http.StatusCreated,
			authorization: "Bot soundboard-token",
			contentType:   "application/json",
			auditReason:   "create launch sound",
			body:          `{"name":"Yay","sound":"data:audio/ogg;base64,T2dnUw==","volume":0.5,"emoji_id":null,"emoji_name":"🔊"}`,
			response:      guildSound,
		},
		soundboardRequestExpectation{
			method:        http.MethodPatch,
			path:          "/api/v" + APIVersion + "/guilds/" + guildID + "/soundboard-sounds/" + soundID,
			authorization: "Bot soundboard-token",
			contentType:   "application/json",
			auditReason:   "replace sound emoji",
			body:          `{"volume":null,"emoji_id":null,"emoji_name":"🎉"}`,
			response:      editedSound,
		},
		soundboardRequestExpectation{
			method:        http.MethodDelete,
			path:          "/api/v" + APIVersion + "/guilds/" + guildID + "/soundboard-sounds/" + soundID,
			status:        http.StatusNoContent,
			authorization: "Bot soundboard-token",
			auditReason:   "remove old sound",
		},
		soundboardRequestExpectation{
			method:        http.MethodPost,
			path:          "/api/v" + APIVersion + "/channels/" + channelID + "/send-soundboard-sound",
			status:        http.StatusNoContent,
			authorization: "Bot soundboard-token",
			contentType:   "application/json",
			body:          `{"sound_id":"1106714396018884649","source_guild_id":"613425648685547541"}`,
		},
	)

	defaults, err := session.SoundboardDefaultSounds()
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults) != 1 || defaults[0].SoundID != "1" ||
		defaults[0].EmojiID != nil || defaults[0].EmojiName == nil ||
		*defaults[0].EmojiName != "🦆" {
		t.Fatalf("unexpected default sounds: %#v", defaults)
	}

	guildSounds, err := session.GuildSoundboardSounds(guildID)
	if err != nil {
		t.Fatal(err)
	}
	if len(guildSounds) != 1 || guildSounds[0].GuildID != guildID {
		t.Fatalf("unexpected guild sounds: %#v", guildSounds)
	}

	sound, err := session.GuildSoundboardSound(guildID, soundID)
	if err != nil {
		t.Fatal(err)
	}
	if sound.SoundID != soundID || sound.User == nil || sound.User.ID != "100000000000000001" {
		t.Fatalf("unexpected guild sound: %#v", sound)
	}

	sound, err = session.GuildSoundboardSoundCreate(
		guildID,
		&SoundboardSoundCreateParams{
			Name:      "Yay",
			Sound:     "data:audio/ogg;base64,T2dnUw==",
			Volume:    SoundboardValue(0.5),
			EmojiID:   SoundboardNull[string](),
			EmojiName: SoundboardValue("🔊"),
		},
		WithAuditLogReason("create launch sound"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if sound.SoundID != soundID {
		t.Fatalf("created sound ID = %q", sound.SoundID)
	}

	sound, err = session.GuildSoundboardSoundEdit(
		guildID,
		soundID,
		&SoundboardSoundEditParams{
			Volume:    SoundboardNull[float64](),
			EmojiID:   SoundboardNull[string](),
			EmojiName: SoundboardValue("🎉"),
		},
		WithAuditLogReason("replace sound emoji"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if sound.EmojiID != nil || sound.EmojiName == nil || *sound.EmojiName != "🎉" {
		t.Fatalf("unexpected edited sound: %#v", sound)
	}

	if err = session.GuildSoundboardSoundDelete(
		guildID,
		soundID,
		WithAuditLogReason("remove old sound"),
	); err != nil {
		t.Fatal(err)
	}
	if err = session.ChannelSoundboardSoundSend(
		channelID,
		&SoundboardSoundSendParams{
			SoundID:       soundID,
			SourceGuildID: SoundboardValue(guildID),
		},
	); err != nil {
		t.Fatal(err)
	}
}

func TestSoundboardRESTValidation(t *testing.T) {
	const validSound = "data:audio/mpeg;base64,SUQz"
	validName := "Valid"
	validCreate := func() *SoundboardSoundCreateParams {
		return &SoundboardSoundCreateParams{Name: validName, Sound: validSound}
	}

	tests := []struct {
		name string
		call func(*Session) error
	}{
		{
			name: "guild ID required",
			call: func(session *Session) error {
				_, err := session.GuildSoundboardSounds("")
				return err
			},
		},
		{
			name: "guild ID must be snowflake",
			call: func(session *Session) error {
				_, err := session.GuildSoundboardSound("guild", "1")
				return err
			},
		},
		{
			name: "sound ID required",
			call: func(session *Session) error {
				_, err := session.GuildSoundboardSound("1", "")
				return err
			},
		},
		{
			name: "create params required",
			call: func(session *Session) error {
				_, err := session.GuildSoundboardSoundCreate("1", nil)
				return err
			},
		},
		{
			name: "name too short",
			call: func(session *Session) error {
				data := validCreate()
				data.Name = "x"
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "name too long",
			call: func(session *Session) error {
				data := validCreate()
				data.Name = strings.Repeat("音", soundboardSoundNameMaxLength+1)
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "sound MIME required",
			call: func(session *Session) error {
				data := validCreate()
				data.Sound = "data:audio/wav;base64,UklGRg=="
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "sound base64 required",
			call: func(session *Session) error {
				data := validCreate()
				data.Sound = "data:audio/ogg;base64,%%%"
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "sound size limited",
			call: func(session *Session) error {
				data := validCreate()
				data.Sound = "data:audio/ogg;base64," +
					base64.StdEncoding.EncodeToString(make([]byte, soundboardSoundMaxFileSize+1))
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "volume lower bound",
			call: func(session *Session) error {
				data := validCreate()
				data.Volume = SoundboardValue(-0.01)
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "volume finite",
			call: func(session *Session) error {
				data := validCreate()
				data.Volume = SoundboardValue(math.NaN())
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "emoji ID must be snowflake",
			call: func(session *Session) error {
				data := validCreate()
				data.EmojiID = SoundboardValue("emoji")
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "emoji name nonempty",
			call: func(session *Session) error {
				data := validCreate()
				data.EmojiName = SoundboardValue("")
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "emoji name length limited",
			call: func(session *Session) error {
				data := validCreate()
				data.EmojiName = SoundboardValue(strings.Repeat("🔊", 33))
				_, err := session.GuildSoundboardSoundCreate("1", data)
				return err
			},
		},
		{
			name: "edit params required",
			call: func(session *Session) error {
				_, err := session.GuildSoundboardSoundEdit("1", "2", nil)
				return err
			},
		},
		{
			name: "edit name validated",
			call: func(session *Session) error {
				name := "x"
				_, err := session.GuildSoundboardSoundEdit(
					"1",
					"2",
					&SoundboardSoundEditParams{Name: &name},
				)
				return err
			},
		},
		{
			name: "send params required",
			call: func(session *Session) error {
				return session.ChannelSoundboardSoundSend("1", nil)
			},
		},
		{
			name: "send sound required",
			call: func(session *Session) error {
				return session.ChannelSoundboardSoundSend("1", &SoundboardSoundSendParams{})
			},
		},
		{
			name: "send source guild validated",
			call: func(session *Session) error {
				return session.ChannelSoundboardSoundSend(
					"1",
					&SoundboardSoundSendParams{
						SoundID:       "2",
						SourceGuildID: SoundboardValue("guild"),
					},
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			session, err := New("Bot soundboard-token")
			if err != nil {
				t.Fatal(err)
			}
			session.Client.Transport = soundboardRoundTripper(func(*http.Request) (*http.Response, error) {
				requests++
				return nil, errors.New("validation made a request")
			})

			err = test.call(session)
			if !errors.Is(err, ErrSoundboardValidation) {
				t.Fatalf("error = %v, want ErrSoundboardValidation", err)
			}
			if requests != 0 {
				t.Fatalf("made %d requests after validation failure", requests)
			}
		})
	}
}

func TestSoundboardNullableJSON(t *testing.T) {
	type payload struct {
		Unset SoundboardNullable[string]  `json:"unset,omitzero"`
		Null  SoundboardNullable[string]  `json:"null,omitzero"`
		Value SoundboardNullable[float64] `json:"value,omitzero"`
	}

	body, err := Marshal(payload{
		Null:  SoundboardNull[string](),
		Value: SoundboardValue(0.25),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"null":null,"value":0.25}` {
		t.Fatalf("nullable JSON = %s", body)
	}

	body, err = Marshal(SoundboardSoundSendParams{
		SoundID:       "1",
		SourceGuildID: SoundboardNull[string](),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"sound_id":"1","source_guild_id":null}` {
		t.Fatalf("nullable source guild JSON = %s", body)
	}
	body, err = Marshal(SoundboardSoundSendParams{SoundID: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"sound_id":"1"}` {
		t.Fatalf("omitted source guild JSON = %s", body)
	}

	var decoded SoundboardNullable[string]
	if err = Unmarshal([]byte(`null`), &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.IsSet() || !decoded.IsNull() {
		t.Fatalf("decoded null state = set %t null %t", decoded.IsSet(), decoded.IsNull())
	}
	if err = Unmarshal([]byte(`"emoji"`), &decoded); err != nil {
		t.Fatal(err)
	}
	value, ok := decoded.Get()
	if !ok || value != "emoji" {
		t.Fatalf("decoded value = %q, %t", value, ok)
	}
}
