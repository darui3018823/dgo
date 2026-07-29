package dgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

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

func TestRequestGuildMembersUsesSingleGuildPayload(t *testing.T) {
	query := ""
	payload := requestGuildMembersOp{
		Op: 8,
		Data: requestGuildMembersData{
			GuildID:   "41771983444115456",
			Query:     &query,
			Limit:     0,
			Nonce:     "request",
			Presences: false,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"op":8,"d":{"guild_id":"41771983444115456","query":"","limit":0,"nonce":"request","presences":false}}`
	if string(data) != want {
		t.Fatalf("payload = %s, want %s", data, want)
	}
}

func TestReadySelectsResumeGatewayURL(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = "wss://gateway.discord.gg/?compress=zlib-stream"

	var ready Ready
	if err = json.Unmarshal([]byte(`{
		"v":10,
		"session_id":"session",
		"resume_gateway_url":"wss://gateway-us-east1-b.discord.gg/?region=us-east"
	}`), &ready); err != nil {
		t.Fatal(err)
	}
	session.onReady(&ready)

	connectURL, usingResume, err := session.gatewayConnectURL()
	if err != nil {
		t.Fatal(err)
	}
	if !usingResume {
		t.Fatal("gatewayConnectURL did not select READY resume_gateway_url")
	}
	wantResume := "wss://gateway-us-east1-b.discord.gg/?encoding=json&region=us-east&v=" + APIVersion
	if connectURL != wantResume {
		t.Fatalf("resume gateway URL = %q, want %q", connectURL, wantResume)
	}

	session.gatewaySessionMu.Lock()
	session.resumeGatewayURL = ""
	session.gatewaySessionMu.Unlock()
	connectURL, usingResume, err = session.gatewayConnectURL()
	if err != nil {
		t.Fatal(err)
	}
	if usingResume {
		t.Fatal("gatewayConnectURL selected an empty resume URL")
	}
	wantInitial := "wss://gateway.discord.gg/?compress=zlib-stream&encoding=json&v=" + APIVersion
	if connectURL != wantInitial {
		t.Fatalf("initial gateway URL = %q, want %q", connectURL, wantInitial)
	}
}

func TestGatewayConnectURLRejectsInvalidURL(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	for _, gateway := range []string{"", "https://gateway.discord.gg", "wss:///missing-host"} {
		session.gateway = gateway
		if _, _, err = session.gatewayConnectURL(); err == nil {
			t.Fatalf("accepted invalid gateway URL %q", gateway)
		}
	}
}

func TestRequestGuildMembersValidation(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}

	if err = session.RequestGuildMembers("guild", "prefix", 100, "", false); !errors.Is(err, ErrWSNotFound) {
		t.Fatalf("valid prefix request error = %v, want ErrWSNotFound", err)
	}
	if err = session.RequestGuildMembers("guild", "", 0, "", false); err == nil ||
		!strings.Contains(err.Error(), "GUILD_MEMBERS") {
		t.Fatalf("all-member request error = %v, want missing GUILD_MEMBERS intent", err)
	}

	session.Identify.Intents = IntentGuildMembers
	if err = session.RequestGuildMembers("guild", "", 0, "", false); !errors.Is(err, ErrWSNotFound) {
		t.Fatalf("valid all-member request error = %v, want ErrWSNotFound", err)
	}
	if err = session.RequestGuildMembers("guild", "prefix", 1, "", true); err == nil ||
		!strings.Contains(err.Error(), "GUILD_PRESENCES") {
		t.Fatalf("presence request error = %v, want missing GUILD_PRESENCES intent", err)
	}

	session.Identify.Intents |= IntentGuildPresences
	if err = session.RequestGuildMembers("guild", "prefix", 101, "", true); err == nil {
		t.Fatal("accepted a query limit above 100")
	}
	if err = session.RequestGuildMembers("guild", "prefix", 1, strings.Repeat("n", 33), true); err == nil {
		t.Fatal("accepted a nonce above 32 bytes")
	}
	if err = session.RequestGuildMembers("", "prefix", 1, "", false); err == nil {
		t.Fatal("accepted an empty guild ID")
	}
	if err = session.RequestGuildMembersList("guild", nil, 0, "", false); err == nil {
		t.Fatal("accepted an empty user ID list")
	}
	if err = session.RequestGuildMembersList("guild", []string{""}, 0, "", false); err == nil {
		t.Fatal("accepted an empty user ID")
	}
	if err = session.RequestGuildMembersList("guild", make([]string, 101), 0, "", false); err == nil {
		t.Fatal("accepted more than 100 user IDs")
	}
	if err = session.RequestGuildMembersBatch([]string{"one", "two"}, "prefix", 1, "", false); err == nil {
		t.Fatal("accepted multiple guild IDs")
	}
	if err = session.RequestGuildMembersBatchList(nil, []string{"user"}, 0, "", false); err == nil {
		t.Fatal("accepted no guild IDs")
	}
}

func TestAllGuildMembersRequestCooldown(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)

	first, err := session.reserveAllGuildMembersRequest("guild", now)
	if err != nil {
		t.Fatal(err)
	}
	if first != now {
		t.Fatalf("reservation = %v, want %v", first, now)
	}
	_, err = session.reserveAllGuildMembersRequest("guild", now.Add(10*time.Second))
	var limited *GuildMembersRequestRateLimitError
	if !errors.As(err, &limited) {
		t.Fatalf("second reservation error = %v, want GuildMembersRequestRateLimitError", err)
	}
	if limited.GuildID != "guild" || limited.RetryAfter != 20*time.Second {
		t.Fatalf("rate limit error = %#v", limited)
	}
	if _, err = session.reserveAllGuildMembersRequest("other", now.Add(10*time.Second)); err != nil {
		t.Fatalf("different guild reservation error = %v", err)
	}
	if _, err = session.reserveAllGuildMembersRequest("guild", now.Add(30*time.Second)); err != nil {
		t.Fatalf("reservation after cooldown error = %v", err)
	}

	reservation := now.Add(31 * time.Second)
	session.guildMembersRequests["rollback"] = reservation
	session.releaseAllGuildMembersRequest("rollback", reservation)
	if _, ok := session.guildMembersRequests["rollback"]; ok {
		t.Fatal("failed request reservation was not released")
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

func TestOnEventGatewayRateLimited(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	message := []byte(`{
		"op":0,
		"s":85,
		"t":"RATE_LIMITED",
		"d":{
			"opcode":8,
			"retry_after":12.5,
			"meta":{"guild_id":"guild","nonce":"request"}
		}
	}`)
	event, err := session.onEvent(websocket.TextMessage, message)
	if err != nil {
		t.Fatal(err)
	}
	limited, ok := event.Struct.(*GatewayRateLimited)
	if !ok {
		t.Fatalf("event.Struct = %T, want *GatewayRateLimited", event.Struct)
	}
	if limited.Opcode != 8 || limited.RetryAfter != 12.5 {
		t.Fatalf("rate limit = %#v", limited)
	}
	if limited.Meta.GuildID != "guild" || limited.Meta.Nonce != "request" {
		t.Fatalf("metadata = %#v", limited.Meta)
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
