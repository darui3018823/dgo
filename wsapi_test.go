package dgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestOnEventLogsZlibError(t *testing.T) {
	s, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err = s.onEvent(websocket.BinaryMessage, []byte("not zlib"))
	if err == nil {
		t.Fatal("onEvent accepted malformed zlib data")
	}
	if got := logs.String(); !strings.Contains(got, err.Error()) || strings.Contains(got, "<nil>") {
		t.Fatalf("zlib log = %q, want actual error %q", got, err)
	}
}

func TestOpenRejectsNonBotCredentials(t *testing.T) {
	tests := []string{
		"",
		"raw-user-token",
		"Bearer oauth-token",
		"Bot ",
	}

	for _, token := range tests {
		t.Run(token, func(t *testing.T) {
			s, err := New(token)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Open(); !errors.Is(err, ErrWSInvalidToken) {
				t.Fatalf("Open() error = %v, want %v", err, ErrWSInvalidToken)
			}
		})
	}
}

func TestOnEventVoiceChannelStatusUpdate(t *testing.T) {
	s, err := New("Bot token")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	message := []byte(`{"op":0,"s":82,"t":"VOICE_CHANNEL_STATUS_UPDATE","d":{"status":null,"id":"1203011144998985748","guild_id":"1202274544208064653"}}`)
	event, err := s.onEvent(websocket.TextMessage, message)
	if err != nil {
		t.Fatalf("onEvent returned error: %v", err)
	}

	update, ok := event.Struct.(*VoiceChannelStatusUpdate)
	if !ok {
		t.Fatalf("event.Struct = %T, want *VoiceChannelStatusUpdate", event.Struct)
	}
	if update.ID != "1203011144998985748" {
		t.Errorf("ID = %q, want %q", update.ID, "1203011144998985748")
	}
	if update.GuildID != "1202274544208064653" {
		t.Errorf("GuildID = %q, want %q", update.GuildID, "1202274544208064653")
	}
	if update.Status != nil {
		t.Errorf("Status = %q, want nil", *update.Status)
	}
}

func TestRequestChannelInfoPayload(t *testing.T) {
	payload := requestChannelInfoOp{
		Op: 43,
		Data: requestChannelInfoData{
			GuildID: "guild",
			Fields: []ChannelInfoField{
				ChannelInfoFieldStatus,
				ChannelInfoFieldVoiceStartTime,
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"op":43,"d":{"guild_id":"guild","fields":["status","voice_start_time"]}}`
	if string(data) != want {
		t.Fatalf("payload = %s, want %s", data, want)
	}

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	err = session.RequestChannelInfo("guild", ChannelInfoFieldStatus)
	if !errors.Is(err, ErrWSNotFound) {
		t.Fatalf("valid request error = %v, want ErrWSNotFound without a connection", err)
	}
	if err = session.RequestChannelInfo("", ChannelInfoFieldStatus); err == nil {
		t.Fatal("accepted empty guild ID")
	}
	if err = session.RequestChannelInfo("guild", "future"); err == nil {
		t.Fatal("accepted unsupported field")
	}
}

func TestOnEventChannelInfo(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	message := []byte(`{
		"op":0,
		"s":84,
		"t":"CHANNEL_INFO",
		"d":{
			"guild_id":"guild",
			"channels":[
				{"id":"voice","status":"Town hall","voice_start_time":1785300000},
				{"id":"empty","status":null,"voice_start_time":null}
			]
		}
	}`)
	event, err := session.onEvent(websocket.TextMessage, message)
	if err != nil {
		t.Fatal(err)
	}
	info, ok := event.Struct.(*ChannelInfo)
	if !ok {
		t.Fatalf("event.Struct = %T, want *ChannelInfo", event.Struct)
	}
	if info.GuildID != "guild" || len(info.Channels) != 2 {
		t.Fatalf("channel info = %#v", info)
	}
	if info.Channels[0].Status == nil || *info.Channels[0].Status != "Town hall" {
		t.Fatalf("status = %#v", info.Channels[0].Status)
	}
	if info.Channels[0].VoiceStartTime == nil || *info.Channels[0].VoiceStartTime != 1785300000 {
		t.Fatalf("voice start time = %#v", info.Channels[0].VoiceStartTime)
	}
	if info.Channels[1].Status != nil || info.Channels[1].VoiceStartTime != nil {
		t.Fatalf("nullable fields = %#v", info.Channels[1])
	}
}

func TestOnEventStageInstanceCreate(t *testing.T) {
	s, err := New("Bot token")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	message := []byte(`{"op":0,"s":83,"t":"STAGE_INSTANCE_CREATE","d":{"id":"1","guild_id":"2","channel_id":"3","topic":"town hall","privacy_level":2}}`)
	event, err := s.onEvent(websocket.TextMessage, message)
	if err != nil {
		t.Fatalf("onEvent returned error: %v", err)
	}

	stage, ok := event.Struct.(*StageInstanceEventCreate)
	if !ok {
		t.Fatalf("event.Struct = %T, want *StageInstanceEventCreate", event.Struct)
	}
	if stage.ID != "1" || stage.GuildID != "2" || stage.ChannelID != "3" {
		t.Fatalf("stage instance IDs = %#v", stage.StageInstance)
	}
}
