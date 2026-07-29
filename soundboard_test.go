package dgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSoundboardSoundModel(t *testing.T) {
	var guild Guild
	err := json.Unmarshal([]byte(`{
		"id":"guild",
		"soundboard_sounds":[{
			"name":"Yay",
			"sound_id":"1106714396018884649",
			"volume":0.75,
			"emoji_id":"989193655938064464",
			"emoji_name":null,
			"guild_id":"guild",
			"available":true,
			"user":{"id":"creator","username":"sound-maker"}
		}]
	}`), &guild)
	if err != nil {
		t.Fatal(err)
	}
	if len(guild.SoundboardSounds) != 1 {
		t.Fatalf("soundboard sounds = %#v, want one sound", guild.SoundboardSounds)
	}
	sound := guild.SoundboardSounds[0]
	if sound.Name != "Yay" || sound.SoundID != "1106714396018884649" ||
		sound.Volume != 0.75 || sound.EmojiID == nil ||
		*sound.EmojiID != "989193655938064464" || sound.EmojiName != nil ||
		sound.GuildID != "guild" || !sound.Available ||
		sound.User == nil || sound.User.ID != "creator" {
		t.Fatalf("unexpected soundboard sound: %#v", sound)
	}

	var defaultSound SoundboardSound
	err = json.Unmarshal([]byte(`{
		"name":"quack",
		"sound_id":"1",
		"volume":1.0,
		"emoji_id":null,
		"emoji_name":"🦆",
		"available":true
	}`), &defaultSound)
	if err != nil {
		t.Fatal(err)
	}
	if defaultSound.GuildID != "" || defaultSound.User != nil ||
		defaultSound.EmojiID != nil || defaultSound.EmojiName == nil ||
		*defaultSound.EmojiName != "🦆" {
		t.Fatalf("unexpected default soundboard sound: %#v", defaultSound)
	}
}

func TestSoundboardSoundIDJSONUnion(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		value     string
		isInteger bool
	}{
		{
			name:  "snowflake string",
			input: `"1106714396018884649"`,
			value: "1106714396018884649",
		},
		{
			name:      "default sound integer",
			input:     `1`,
			value:     "1",
			isInteger: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var id SoundboardSoundID
			if err := json.Unmarshal([]byte(test.input), &id); err != nil {
				t.Fatal(err)
			}
			if id.String() != test.value || id.IsInteger != test.isInteger {
				t.Fatalf("sound ID = %#v, want value=%q integer=%t", id, test.value, test.isInteger)
			}
			encoded, err := json.Marshal(id)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.input {
				t.Fatalf("encoded sound ID = %s, want %s", encoded, test.input)
			}
		})
	}

	for _, input := range []string{`""`, `-1`, `1.5`, `1e3`, `{}`} {
		t.Run("invalid "+input, func(t *testing.T) {
			var id SoundboardSoundID
			if err := json.Unmarshal([]byte(input), &id); err == nil {
				t.Fatalf("accepted invalid sound ID %s", input)
			}
		})
	}

	encoded, err := json.Marshal(SoundboardSoundID{})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "null" {
		t.Fatalf("zero sound ID = %s, want null", encoded)
	}
}

func TestRequestSoundboardSoundsPayloadAndValidation(t *testing.T) {
	payload := requestSoundboardSoundsOp{
		Op: 31,
		Data: requestSoundboardSoundsData{
			GuildIDs: []string{"613425648685547541", "81384788765712384"},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"op":31,"d":{"guild_ids":["613425648685547541","81384788765712384"]}}`
	if string(data) != want {
		t.Fatalf("payload = %s, want %s", data, want)
	}

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	err = session.RequestSoundboardSounds([]string{"guild-one", "guild-two"})
	if !errors.Is(err, ErrWSNotFound) {
		t.Fatalf("valid request error = %v, want ErrWSNotFound without a connection", err)
	}

	tests := []struct {
		name     string
		guildIDs []string
	}{
		{name: "nil"},
		{name: "empty", guildIDs: []string{}},
		{name: "empty ID", guildIDs: []string{"guild", ""}},
		{name: "blank ID", guildIDs: []string{"guild", "  "}},
		{name: "duplicate ID", guildIDs: []string{"guild", "guild"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := session.RequestSoundboardSounds(test.guildIDs)
			if err == nil || errors.Is(err, ErrWSNotFound) {
				t.Fatalf("validation error = %v, want an input error", err)
			}
		})
	}
}

func TestSoundboardGatewayEventsAndState(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.SyncEvents = true
	if err = session.State.GuildAdd(&Guild{ID: "guild", Name: "Guild"}); err != nil {
		t.Fatal(err)
	}

	event := dispatchGatewayFixture(t, session, "GUILD_SOUNDBOARD_SOUND_CREATE", `{
		"name":"first",
		"sound_id":"sound-one",
		"volume":0.5,
		"emoji_id":null,
		"emoji_name":"🔊",
		"guild_id":"guild",
		"available":true,
		"user":{"id":"creator","username":"sound-maker"}
	}`)
	created, ok := event.Struct.(*GuildSoundboardSoundCreate)
	if !ok {
		t.Fatalf("event struct = %T, want *GuildSoundboardSoundCreate", event.Struct)
	}
	if created.SoundboardSound == nil || created.SoundID != "sound-one" ||
		created.User == nil || created.User.ID != "creator" {
		t.Fatalf("unexpected create event: %#v", created)
	}
	cached, err := session.State.SoundboardSound("guild", "sound-one")
	if err != nil {
		t.Fatal(err)
	}
	if cached.Name != "first" {
		t.Fatalf("cached create = %#v", cached)
	}
	cachedPointer := cached

	event = dispatchGatewayFixture(t, session, "GUILD_SOUNDBOARD_SOUND_UPDATE", `{
		"name":"updated",
		"sound_id":"sound-one",
		"volume":1,
		"emoji_id":"emoji",
		"emoji_name":null,
		"guild_id":"guild",
		"available":false
	}`)
	updated, ok := event.Struct.(*GuildSoundboardSoundUpdate)
	if !ok {
		t.Fatalf("event struct = %T, want *GuildSoundboardSoundUpdate", event.Struct)
	}
	if updated.Name != "updated" || updated.Available {
		t.Fatalf("unexpected update event: %#v", updated)
	}
	cached, err = session.State.SoundboardSound("guild", "sound-one")
	if err != nil {
		t.Fatal(err)
	}
	if cached != cachedPointer {
		t.Fatal("soundboard update replaced the cached pointer")
	}
	if cached.Name != "updated" || cached.Volume != 1 || cached.Available {
		t.Fatalf("cached update = %#v", cached)
	}

	event = dispatchGatewayFixture(t, session, "GUILD_SOUNDBOARD_SOUND_DELETE", `{
		"sound_id":"sound-one",
		"guild_id":"guild"
	}`)
	deleted, ok := event.Struct.(*GuildSoundboardSoundDelete)
	if !ok {
		t.Fatalf("event struct = %T, want *GuildSoundboardSoundDelete", event.Struct)
	}
	if deleted.SoundID != "sound-one" || deleted.GuildID != "guild" {
		t.Fatalf("unexpected delete event: %#v", deleted)
	}
	if _, err = session.State.SoundboardSound("guild", "sound-one"); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("deleted sound lookup error = %v, want ErrStateNotFound", err)
	}

	event = dispatchGatewayFixture(t, session, "GUILD_SOUNDBOARD_SOUNDS_UPDATE", `{
		"guild_id":"guild",
		"soundboard_sounds":[{
			"name":"bulk",
			"sound_id":"sound-two",
			"volume":0.25,
			"emoji_id":null,
			"emoji_name":null,
			"available":true
		}]
	}`)
	fullUpdate, ok := event.Struct.(*GuildSoundboardSoundsUpdate)
	if !ok {
		t.Fatalf("event struct = %T, want *GuildSoundboardSoundsUpdate", event.Struct)
	}
	if len(fullUpdate.SoundboardSounds) != 1 {
		t.Fatalf("unexpected full update event: %#v", fullUpdate)
	}
	cached, err = session.State.SoundboardSound("guild", "sound-two")
	if err != nil {
		t.Fatal(err)
	}
	if cached.GuildID != "guild" {
		t.Fatalf("full update did not populate guild ID: %#v", cached)
	}

	event = dispatchGatewayFixture(t, session, "SOUNDBOARD_SOUNDS", `{
		"guild_id":"guild",
		"soundboard_sounds":[{
			"name":"response",
			"sound_id":"sound-three",
			"volume":1,
			"emoji_id":null,
			"emoji_name":"🎉",
			"available":true
		}]
	}`)
	response, ok := event.Struct.(*SoundboardSounds)
	if !ok {
		t.Fatalf("event struct = %T, want *SoundboardSounds", event.Struct)
	}
	if len(response.SoundboardSounds) != 1 || response.SoundboardSounds[0].SoundID != "sound-three" {
		t.Fatalf("unexpected soundboard response: %#v", response)
	}
	if _, err = session.State.SoundboardSound("guild", "sound-two"); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("replaced sound lookup error = %v, want ErrStateNotFound", err)
	}
	if _, err = session.State.SoundboardSound("guild", "sound-three"); err != nil {
		t.Fatal(err)
	}

	// Partial Guild Update payloads do not include soundboard_sounds and must
	// not erase sounds learned from Guild Create or Soundboard events.
	if err = session.State.GuildAdd(&Guild{ID: "guild", Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	if _, err = session.State.SoundboardSound("guild", "sound-three"); err != nil {
		t.Fatalf("partial guild update erased soundboard cache: %v", err)
	}

	session.State.TrackSoundboardSounds = false
	dispatchGatewayFixture(t, session, "GUILD_SOUNDBOARD_SOUND_CREATE", `{
		"name":"ignored",
		"sound_id":"sound-four",
		"volume":1,
		"emoji_id":null,
		"emoji_name":null,
		"guild_id":"guild",
		"available":true
	}`)
	if _, err = session.State.SoundboardSound("guild", "sound-four"); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("disabled sound tracking lookup error = %v, want ErrStateNotFound", err)
	}
}

func TestVoiceChannelEffectSendSoundIDVariants(t *testing.T) {
	tests := []struct {
		name      string
		soundID   string
		value     string
		isInteger bool
	}{
		{
			name:    "guild snowflake",
			soundID: `"1106714396018884649"`,
			value:   "1106714396018884649",
		},
		{
			name:      "default integer",
			soundID:   `1`,
			value:     "1",
			isInteger: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, err := New("Bot token")
			if err != nil {
				t.Fatal(err)
			}
			session.SyncEvents = true
			event := dispatchGatewayFixture(t, session, "VOICE_CHANNEL_EFFECT_SEND", fmt.Sprintf(`{
				"channel_id":"channel",
				"guild_id":"guild",
				"user_id":"user",
				"emoji":{"id":null,"name":"🔊"},
				"animation_type":0,
				"animation_id":42,
				"sound_id":%s,
				"sound_volume":0.5
			}`, test.soundID))
			effect, ok := event.Struct.(*VoiceChannelEffectSend)
			if !ok {
				t.Fatalf("event struct = %T, want *VoiceChannelEffectSend", event.Struct)
			}
			if effect.ChannelID != "channel" || effect.GuildID != "guild" ||
				effect.UserID != "user" || effect.Emoji == nil ||
				effect.AnimationType == nil ||
				*effect.AnimationType != VoiceChannelEffectAnimationPremium ||
				effect.AnimationID == nil || *effect.AnimationID != 42 ||
				effect.SoundID == nil || effect.SoundID.String() != test.value ||
				effect.SoundID.IsInteger != test.isInteger ||
				effect.SoundVolume == nil || *effect.SoundVolume != 0.5 {
				t.Fatalf("unexpected voice channel effect: %#v", effect)
			}
		})
	}
}

func TestSoundboardStateHelpersValidateAndReplace(t *testing.T) {
	state := NewState()
	if !state.TrackSoundboardSounds {
		t.Fatal("soundboard sound tracking is disabled by default")
	}
	if err := state.GuildAdd(&Guild{ID: "guild"}); err != nil {
		t.Fatal(err)
	}
	if err := state.SoundboardSoundAdd("guild", nil); err == nil {
		t.Fatal("SoundboardSoundAdd accepted nil sound")
	}
	if err := state.SoundboardSoundAdd("guild", &SoundboardSound{}); err == nil {
		t.Fatal("SoundboardSoundAdd accepted empty sound ID")
	}
	if err := state.SoundboardSoundAdd("guild", &SoundboardSound{
		SoundID: "sound",
		GuildID: "other",
	}); err == nil {
		t.Fatal("SoundboardSoundAdd accepted mismatched guild ID")
	}
	if err := state.SoundboardSoundsUpdate("guild", []*SoundboardSound{nil}); err == nil {
		t.Fatal("SoundboardSoundsUpdate accepted nil sound")
	}

	sounds := []*SoundboardSound{{SoundID: "one"}, {SoundID: "two"}}
	if err := state.SoundboardSoundsUpdate("guild", sounds); err != nil {
		t.Fatal(err)
	}
	guild, err := state.Guild("guild")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(guild.SoundboardSounds, sounds) {
		t.Fatalf("soundboard sounds = %#v, want %#v", guild.SoundboardSounds, sounds)
	}
	for _, sound := range guild.SoundboardSounds {
		if sound.GuildID != "guild" {
			t.Fatalf("sound guild ID = %q, want guild", sound.GuildID)
		}
	}
}

func dispatchGatewayFixture(t *testing.T, session *Session, eventType, data string) *Event {
	t.Helper()
	message := fmt.Sprintf(`{"op":0,"s":1,"t":%q,"d":%s}`, eventType, data)
	event, err := session.onEvent(websocket.TextMessage, []byte(message))
	if err != nil {
		t.Fatalf("dispatch %s: %v", eventType, err)
	}
	if event.Struct == nil {
		t.Fatalf("event %s was not registered", eventType)
	}
	return event
}
