package dgo

import (
	"errors"
	"testing"

	"github.com/gorilla/websocket"
)

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
