package dgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type gatewayTestServer struct {
	server      *httptest.Server
	url         string
	connections atomic.Int32
}

func newGatewayTestServer(
	t *testing.T,
	handler func(server *gatewayTestServer, connection int, ws *websocket.Conn),
) *gatewayTestServer {
	t.Helper()
	testServer := &gatewayTestServer{}
	testServer.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		connection := int(testServer.connections.Add(1))
		handler(testServer, connection, ws)
	}))
	testServer.url = "ws" + strings.TrimPrefix(testServer.server.URL, "http")
	t.Cleanup(func() {
		testServer.server.CloseClientConnections()
		testServer.server.Close()
	})
	return testServer
}

func writeGatewayHello(ws *websocket.Conn, interval time.Duration) error {
	return ws.WriteJSON(map[string]interface{}{
		"op": 10,
		"d": map[string]interface{}{
			"heartbeat_interval": interval.Milliseconds(),
		},
	})
}

func writeGatewayReady(ws *websocket.Conn, gatewayURL, sessionID string) error {
	return ws.WriteJSON(map[string]interface{}{
		"op": 0,
		"s":  1,
		"t":  "READY",
		"d": map[string]interface{}{
			"v":                  10,
			"session_id":         sessionID,
			"resume_gateway_url": gatewayURL,
			"user": map[string]interface{}{
				"id":       "1",
				"username": "gateway-test",
			},
			"application": map[string]interface{}{
				"id": "2",
			},
			"guilds": []interface{}{},
		},
	})
}

func readGatewayOperation(ws *websocket.Conn) (int, json.RawMessage, error) {
	var payload struct {
		Operation int             `json:"op"`
		Data      json.RawMessage `json:"d"`
	}
	err := ws.ReadJSON(&payload)
	return payload.Operation, payload.Data, err
}

func waitForGatewayRoutineCount(t *testing.T, session *Session, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := atomic.LoadInt64(&session.gatewayRoutineCount); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf(
		"gateway routine count = %d, want %d",
		atomic.LoadInt64(&session.gatewayRoutineCount),
		want,
	)
}

func assertGatewayClosed(t *testing.T, session *Session) {
	t.Helper()
	session.gatewayLifecycleMu.Lock()
	lifecycle := session.gatewayLifecycle
	connection := session.gatewayConnection
	state := session.gatewayState
	session.gatewayLifecycleMu.Unlock()
	session.RLock()
	wsConn := session.wsConn
	listening := session.listening
	dataReady := session.DataReady
	session.RUnlock()
	if lifecycle != nil || connection != nil || state != gatewayStateClosed {
		t.Fatalf(
			"gateway lifecycle not closed: lifecycle=%p connection=%p state=%d",
			lifecycle,
			connection,
			state,
		)
	}
	if wsConn != nil || listening != nil || dataReady {
		t.Fatalf(
			"gateway fields not cleared: ws=%p listening=%p ready=%t",
			wsConn,
			listening,
			dataReady,
		)
	}
}

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
	for _, token := range []string{
		"raw-user-token",
		"Bot ",
	} {
		t.Run(token, func(t *testing.T) {
			s, err := New(token)
			if !errors.Is(err, ErrInvalidSessionToken) {
				t.Fatalf("New() error = %v, want %v", err, ErrInvalidSessionToken)
			}
			if s != nil {
				t.Fatalf("New() session = %#v, want nil", s)
			}
		})
	}

	for _, token := range []string{
		"",
		"Bearer oauth-token",
	} {
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

func TestInvalidSessionPayloadControlsResumeState(t *testing.T) {
	tests := []struct {
		name              string
		payload           string
		wantSessionID     string
		wantResumeGateway string
		wantSequence      int64
	}{
		{
			name:              "resumable",
			payload:           `{"op":9,"d":true}`,
			wantSessionID:     "session",
			wantResumeGateway: "wss://resume.discord.gg",
			wantSequence:      42,
		},
		{
			name:    "not resumable",
			payload: `{"op":9,"d":false}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, err := New("Bot token")
			if err != nil {
				t.Fatal(err)
			}
			session.ShouldReconnectOnError = false
			session.gatewaySessionMu.Lock()
			session.sessionID = "session"
			session.resumeGatewayURL = "wss://resume.discord.gg"
			session.gatewaySessionMu.Unlock()
			atomic.StoreInt64(session.sequence, 42)

			event, err := session.onEvent(websocket.TextMessage, []byte(test.payload))
			if err != nil {
				t.Fatal(err)
			}
			if event.Operation != 9 {
				t.Fatalf("operation = %d, want 9", event.Operation)
			}
			session.gatewaySessionMu.RLock()
			sessionID := session.sessionID
			resumeGateway := session.resumeGatewayURL
			session.gatewaySessionMu.RUnlock()
			if sessionID != test.wantSessionID {
				t.Fatalf("session ID = %q, want %q", sessionID, test.wantSessionID)
			}
			if resumeGateway != test.wantResumeGateway {
				t.Fatalf("resume gateway = %q, want %q", resumeGateway, test.wantResumeGateway)
			}
			if sequence := atomic.LoadInt64(session.sequence); sequence != test.wantSequence {
				t.Fatalf("sequence = %d, want %d", sequence, test.wantSequence)
			}
		})
	}
}

func TestInvalidSessionRejectsNonBooleanPayload(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.ShouldReconnectOnError = false
	if _, err = session.onEvent(websocket.TextMessage, []byte(`{"op":9,"d":"yes"}`)); err == nil {
		t.Fatal("accepted a non-boolean invalid session payload")
	}
}

func TestInvalidSessionBackoffIsWithinDiscordWindow(t *testing.T) {
	for range 100 {
		delay := invalidSessionBackoff()
		if delay < time.Second || delay >= 5*time.Second {
			t.Fatalf("backoff = %s, want [1s, 5s)", delay)
		}
	}
}

func TestGatewayCloseCodeClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want GatewayCloseRecovery
	}{
		{name: "network error", err: errors.New("network"), want: GatewayCloseRecoveryResume},
		{name: "unknown error", err: &websocket.CloseError{Code: 4000}, want: GatewayCloseRecoveryResume},
		{name: "rate limited", err: &websocket.CloseError{Code: 4008}, want: GatewayCloseRecoveryResume},
		{name: "normal closure", err: &websocket.CloseError{Code: 1000}, want: GatewayCloseRecoveryIdentify},
		{name: "going away", err: &websocket.CloseError{Code: 1001}, want: GatewayCloseRecoveryIdentify},
		{name: "invalid sequence", err: &websocket.CloseError{Code: 4007}, want: GatewayCloseRecoveryIdentify},
		{name: "session timeout", err: &websocket.CloseError{Code: 4009}, want: GatewayCloseRecoveryIdentify},
		{name: "authentication failed", err: &websocket.CloseError{Code: 4004}, want: GatewayCloseRecoveryStop},
		{name: "invalid shard", err: &websocket.CloseError{Code: 4010}, want: GatewayCloseRecoveryStop},
		{name: "sharding required", err: &websocket.CloseError{Code: 4011}, want: GatewayCloseRecoveryStop},
		{name: "invalid API version", err: &websocket.CloseError{Code: 4012}, want: GatewayCloseRecoveryStop},
		{name: "invalid intents", err: &websocket.CloseError{Code: 4013}, want: GatewayCloseRecoveryStop},
		{name: "disallowed intents", err: &websocket.CloseError{Code: 4014}, want: GatewayCloseRecoveryStop},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := gatewayReconnectActionForError(test.err); got != test.want {
				t.Fatalf("action = %d, want %d", got, test.want)
			}
		})
	}
}

func TestGatewayCloseEventExposesTerminalFailure(t *testing.T) {
	closeErr := &websocket.CloseError{Code: 4014, Text: "disallowed intents"}
	event := newGatewayCloseEvent(closeErr, gatewayReconnectActionForError(closeErr))
	if event.Code != 4014 ||
		event.Reason != "disallowed intents" ||
		event.Recovery != GatewayCloseRecoveryStop ||
		!errors.Is(event.Err, closeErr) {
		t.Fatalf("gateway close event = %#v", event)
	}
	if handlerForInterface(func(*Session, *GatewayClose) {}) == nil {
		t.Fatal("typed GatewayClose handler was not generated")
	}
}

func TestHeartbeatRequiresAckBeforeNextSend(t *testing.T) {
	sent := time.Unix(100, 0)
	if heartbeatAckPending(time.Time{}, time.Time{}) {
		t.Fatal("heartbeat was pending before the first send")
	}
	if !heartbeatAckPending(sent.Add(-time.Nanosecond), sent) {
		t.Fatal("unacknowledged heartbeat was not detected")
	}
	if heartbeatAckPending(sent, sent) {
		t.Fatal("equal acknowledgement time was treated as pending")
	}
	if heartbeatAckPending(sent.Add(time.Nanosecond), sent) {
		t.Fatal("acknowledged heartbeat was treated as pending")
	}
	if FailedHeartbeatAcks != 1 {
		t.Fatalf("FailedHeartbeatAcks = %d, want 1", FailedHeartbeatAcks)
	}
}

func TestHeartbeatMetricsAndJitter(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	sent := time.Unix(100, 0)
	ack := sent.Add(25 * time.Millisecond)
	session.Lock()
	session.LastHeartbeatSent = sent
	session.LastHeartbeatAck = ack
	session.Unlock()
	if latency := session.HeartbeatLatency(); latency != 25*time.Millisecond {
		t.Fatalf("latency = %s, want 25ms", latency)
	}
	atomic.AddUint64(&session.missedHeartbeatAcks, 2)
	if missed := session.MissedHeartbeatAcks(); missed != 2 {
		t.Fatalf("missed ACKs = %d, want 2", missed)
	}
	for range 100 {
		jitter := heartbeatJitter(45 * time.Second)
		if jitter < 0 || jitter >= 45*time.Second {
			t.Fatalf("jitter = %s, want [0s, 45s)", jitter)
		}
	}
	if jitter := heartbeatJitter(0); jitter != 0 {
		t.Fatalf("zero interval jitter = %s", jitter)
	}
}

func TestNormalCloseInvalidatesResumeState(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gatewaySessionMu.Lock()
	session.sessionID = "session"
	session.resumeGatewayURL = "wss://resume.discord.gg"
	session.gatewaySessionMu.Unlock()
	atomic.StoreInt64(session.sequence, 42)

	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	session.gatewaySessionMu.RLock()
	sessionID := session.sessionID
	resumeGateway := session.resumeGatewayURL
	session.gatewaySessionMu.RUnlock()
	if sessionID != "" || resumeGateway != "" || atomic.LoadInt64(session.sequence) != 0 {
		t.Fatalf("normal close preserved resume state: session=%q gateway=%q sequence=%d",
			sessionID, resumeGateway, atomic.LoadInt64(session.sequence))
	}
}

func TestSessionCloseIsIdempotentAndClosesVoiceConnections(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}

	voiceClosed := make(chan struct{})
	voice := &VoiceConnection{
		Ready: true,
		close: voiceClosed,
	}
	session.VoiceConnections = map[string]*VoiceConnection{"guild": voice}
	session.listening = make(chan interface{})
	session.SyncEvents = true

	var disconnects atomic.Int32
	session.AddHandler(func(*Session, *Disconnect) {
		disconnects.Add(1)
	})

	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-voiceClosed:
	default:
		t.Fatal("Session.Close did not stop the Voice connection")
	}
	voice.RLock()
	voiceReady := voice.Ready
	voice.RUnlock()
	if voiceReady {
		t.Fatal("Voice connection remained ready after Session.Close")
	}
	session.RLock()
	voiceCount := len(session.VoiceConnections)
	session.RUnlock()
	if voiceCount != 0 {
		t.Fatalf("Session retained %d Voice connections after Close", voiceCount)
	}
	if got := disconnects.Load(); got != 1 {
		t.Fatalf("Disconnect events = %d, want 1", got)
	}

	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	if got := disconnects.Load(); got != 1 {
		t.Fatalf("Disconnect events after second Close = %d, want 1", got)
	}
}

func TestChannelVoiceJoinBeforeGatewayIsSafe(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.VoiceConnections = nil

	if err = session.ChannelVoiceJoinManual("", "channel", false, false); err == nil {
		t.Fatal("accepted an empty guild ID")
	}
	if err = session.ChannelVoiceJoinManual("guild", "channel", false, false); !errors.Is(err, ErrWSNotFound) {
		t.Fatalf("manual voice join error = %v, want ErrWSNotFound", err)
	}
	voice, err := session.ChannelVoiceJoin("guild", "channel", false, false)
	if !errors.Is(err, ErrWSNotFound) {
		t.Fatalf("voice join error = %v, want ErrWSNotFound", err)
	}
	if voice == nil {
		t.Fatal("voice join returned a nil connection")
	}
	session.RLock()
	_, retained := session.VoiceConnections["guild"]
	session.RUnlock()
	if retained {
		t.Fatal("failed voice join left a stale connection in the session")
	}
}

func TestVoiceStateUpdateToleratesMissingStateUser(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	voice := &VoiceConnection{}
	session.VoiceConnections = map[string]*VoiceConnection{"guild": voice}

	session.State = nil
	session.onVoiceStateUpdate(&VoiceStateUpdate{VoiceState: &VoiceState{
		GuildID:   "guild",
		ChannelID: "channel",
		UserID:    "user",
		SessionID: "voice-session",
	}})
	session.State = NewState()
	session.onVoiceStateUpdate(&VoiceStateUpdate{VoiceState: &VoiceState{
		GuildID:   "guild",
		ChannelID: "channel",
		UserID:    "user",
		SessionID: "voice-session",
	}})

	voice.RLock()
	sessionID := voice.sessionID
	voice.RUnlock()
	if sessionID != "" {
		t.Fatalf("voice state without current user changed session ID to %q", sessionID)
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

func TestOpenStartsHeartbeatBeforeDelayedReady(t *testing.T) {
	var heartbeats atomic.Int32
	server := newGatewayTestServer(t, func(server *gatewayTestServer, _ int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, 20*time.Millisecond); err != nil {
			return
		}
		readySent := false
		for {
			operation, data, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			if operation != 1 {
				continue
			}
			count := heartbeats.Add(1)
			if err = ws.WriteJSON(map[string]interface{}{"op": 11, "d": data}); err != nil {
				return
			}
			if count >= 3 && !readySent {
				readySent = true
				if err = writeGatewayReady(ws, server.url, "delayed-ready"); err != nil {
					return
				}
			}
		}
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	session.SyncEvents = true
	var connects atomic.Int32
	var disconnects atomic.Int32
	session.AddHandler(func(*Session, *Connect) { connects.Add(1) })
	session.AddHandler(func(*Session, *Disconnect) { disconnects.Add(1) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err = session.OpenWithContext(ctx); err != nil {
		t.Fatal(err)
	}
	if got := heartbeats.Load(); got < 3 {
		t.Fatalf("heartbeats before READY = %d, want at least 3", got)
	}
	if got := connects.Load(); got != 1 {
		t.Fatalf("Connect events = %d, want 1", got)
	}
	session.RLock()
	dataReady := session.DataReady
	session.RUnlock()
	if !dataReady {
		t.Fatal("Session was not ready after READY")
	}

	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	if got := disconnects.Load(); got != 1 {
		t.Fatalf("Disconnect events = %d, want 1", got)
	}
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
}

func TestOpenWithContextCancelsGatewayREST(t *testing.T) {
	requestStarted := make(chan struct{})
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.Client = &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}

	ctx, cancel := context.WithCancel(context.Background())
	openDone := make(chan error, 1)
	go func() { openDone <- session.OpenWithContext(ctx) }()
	<-requestStarted
	cancel()
	if err = <-openDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenWithContext error = %v, want context.Canceled", err)
	}
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
}

func TestOpenWithContextCancelsGatewayDial(t *testing.T) {
	dialStarted := make(chan struct{})
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = "ws://gateway.invalid"
	session.Dialer = &websocket.Dialer{
		NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			close(dialStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	openDone := make(chan error, 1)
	go func() { openDone <- session.OpenWithContext(ctx) }()
	<-dialStarted
	cancel()
	if err = <-openDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenWithContext error = %v, want context.Canceled", err)
	}
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
}

func TestOpenWithContextCancelsHelloRead(t *testing.T) {
	connected := make(chan struct{})
	server := newGatewayTestServer(t, func(server *gatewayTestServer, _ int, ws *websocket.Conn) {
		close(connected)
		for {
			if _, _, err := readGatewayOperation(ws); err != nil {
				return
			}
		}
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	ctx, cancel := context.WithCancel(context.Background())
	openDone := make(chan error, 1)
	go func() { openDone <- session.OpenWithContext(ctx) }()
	<-connected
	cancel()
	if err = <-openDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenWithContext error = %v, want context.Canceled", err)
	}
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
}

func TestOpenWithContextCancelsReadyWait(t *testing.T) {
	identified := make(chan struct{})
	server := newGatewayTestServer(t, func(server *gatewayTestServer, _ int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, time.Second); err != nil {
			return
		}
		for {
			operation, _, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			if operation == 2 {
				close(identified)
				continue
			}
			if operation == 1 {
				if err = ws.WriteJSON(map[string]interface{}{"op": 11}); err != nil {
					return
				}
			}
		}
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	ctx, cancel := context.WithCancel(context.Background())
	openDone := make(chan error, 1)
	go func() { openDone <- session.OpenWithContext(ctx) }()
	<-identified
	cancel()
	if err = <-openDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenWithContext error = %v, want context.Canceled", err)
	}
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
}

func TestOpenReconnectsWhenOpcode7ArrivesBeforeReady(t *testing.T) {
	server := newGatewayTestServer(t, func(server *gatewayTestServer, connection int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, time.Second); err != nil {
			return
		}
		for {
			operation, _, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			if operation != 2 {
				continue
			}
			if connection == 1 {
				_ = ws.WriteJSON(map[string]interface{}{"op": 7, "d": nil})
				return
			}
			_ = writeGatewayReady(ws, server.url, "after-opcode-7")
			for {
				if _, _, err = readGatewayOperation(ws); err != nil {
					return
				}
			}
		}
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err = session.OpenWithContext(ctx); err != nil {
		t.Fatal(err)
	}
	if got := server.connections.Load(); got != 2 {
		t.Fatalf("Gateway connections = %d, want 2", got)
	}
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	waitForGatewayRoutineCount(t, session, 0)
}

func TestOpenCancelInterruptsInvalidSessionBackoff(t *testing.T) {
	invalidSessionSent := make(chan struct{})
	server := newGatewayTestServer(t, func(_ *gatewayTestServer, _ int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, time.Second); err != nil {
			return
		}
		for {
			operation, _, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			if operation == 2 {
				_ = ws.WriteJSON(map[string]interface{}{"op": 9, "d": false})
				close(invalidSessionSent)
				return
			}
		}
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	ctx, cancel := context.WithCancel(context.Background())
	openDone := make(chan error, 1)
	go func() { openDone <- session.OpenWithContext(ctx) }()
	<-invalidSessionSent
	cancelStarted := time.Now()
	cancel()
	if err = <-openDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenWithContext error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(cancelStarted); elapsed > 250*time.Millisecond {
		t.Fatalf("invalid-session backoff cancellation took %s", elapsed)
	}
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
}

func TestGatewayReconnectCloseRaceEmitsOneDisconnect(t *testing.T) {
	secondHello := make(chan struct{})
	server := newGatewayTestServer(t, func(server *gatewayTestServer, connection int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, time.Second); err != nil {
			return
		}
		for {
			operation, _, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			if connection == 1 && operation == 2 {
				if err = writeGatewayReady(ws, server.url, "reconnect-race"); err != nil {
					return
				}
				time.Sleep(20 * time.Millisecond)
				return
			}
			if connection == 2 && operation == 6 {
				close(secondHello)
				for {
					if _, _, err = readGatewayOperation(ws); err != nil {
						return
					}
				}
			}
		}
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	session.SyncEvents = true
	var connects atomic.Int32
	var disconnects atomic.Int32
	session.AddHandler(func(*Session, *Connect) { connects.Add(1) })
	session.AddHandler(func(*Session, *Disconnect) { disconnects.Add(1) })
	if err = session.Open(); err != nil {
		t.Fatal(err)
	}
	<-secondHello

	var closes sync.WaitGroup
	closes.Add(2)
	for range 2 {
		go func() {
			defer closes.Done()
			if closeErr := session.Close(); closeErr != nil {
				t.Errorf("Close returned error: %v", closeErr)
			}
		}()
	}
	closes.Wait()
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
	if got := connects.Load(); got != 1 {
		t.Fatalf("Connect events = %d, want 1", got)
	}
	if got := disconnects.Load(); got != 1 {
		t.Fatalf("Disconnect events = %d, want 1", got)
	}
}

func TestGatewayLifetimeContextCancellationClosesReadyConnection(t *testing.T) {
	server := newGatewayTestServer(t, func(server *gatewayTestServer, _ int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, time.Second); err != nil {
			return
		}
		for {
			operation, _, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			switch operation {
			case 2:
				if err = writeGatewayReady(ws, server.url, "lifetime-cancel"); err != nil {
					return
				}
			case 1:
				if err = ws.WriteJSON(map[string]interface{}{"op": 11}); err != nil {
					return
				}
			}
		}
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	session.SyncEvents = true
	var connects atomic.Int32
	var disconnects atomic.Int32
	session.AddHandler(func(*Session, *Connect) { connects.Add(1) })
	session.AddHandler(func(*Session, *Disconnect) { disconnects.Add(1) })
	ctx, cancel := context.WithCancel(context.Background())
	if err = session.OpenWithContext(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
	if got := connects.Load(); got != 1 {
		t.Fatalf("Connect events = %d, want 1", got)
	}
	if got := disconnects.Load(); got != 1 {
		t.Fatalf("Disconnect events = %d, want 1", got)
	}
}

func TestConnectHandlerCanCloseWithoutDeadlock(t *testing.T) {
	server := newGatewayTestServer(t, func(server *gatewayTestServer, _ int, ws *websocket.Conn) {
		if err := writeGatewayHello(ws, time.Second); err != nil {
			return
		}
		for {
			operation, _, err := readGatewayOperation(ws)
			if err != nil {
				return
			}
			if operation == 2 {
				_ = writeGatewayReady(ws, server.url, "close-in-connect")
			}
		}
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	session.SyncEvents = true
	var connects atomic.Int32
	var disconnects atomic.Int32
	session.AddHandler(func(session *Session, _ *Connect) {
		connects.Add(1)
		if closeErr := session.Close(); closeErr != nil {
			t.Errorf("Close in Connect handler: %v", closeErr)
		}
	})
	session.AddHandler(func(*Session, *Disconnect) { disconnects.Add(1) })

	openDone := make(chan error, 1)
	go func() { openDone <- session.Open() }()
	select {
	case err = <-openDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Open deadlocked when Connect handler called Close")
	}
	waitForGatewayRoutineCount(t, session, 0)
	assertGatewayClosed(t, session)
	if got := connects.Load(); got != 1 {
		t.Fatalf("Connect events = %d, want 1", got)
	}
	if got := disconnects.Load(); got != 1 {
		t.Fatalf("Disconnect events = %d, want 1", got)
	}
}

func TestSessionCloseWaitsForLifetimeVoiceRoutines(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.beginGatewayLifecycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	lifetime := session.gatewayContext()
	routineDone := make(chan struct{})
	if started := session.startVoiceRoutine(func() {
		<-lifetime.Done()
		close(routineDone)
	}); !started {
		t.Fatal("voice routine did not start for active Gateway lifetime")
	}

	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-routineDone:
	default:
		t.Fatal("Session.Close returned before the lifetime voice routine stopped")
	}
	assertGatewayClosed(t, session)
}

func TestSessionCloseStopsVoiceReconnectRoutine(t *testing.T) {
	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.beginGatewayLifecycle(context.Background()); err != nil {
		t.Fatal(err)
	}

	voice := &VoiceConnection{
		GuildID:   "guild",
		ChannelID: "channel",
		session:   session,
	}
	if started := session.startVoiceRoutine(voice.reconnect); !started {
		t.Fatal("voice reconnect routine did not start")
	}

	deadline := time.Now().Add(time.Second)
	for {
		voice.RLock()
		reconnecting := voice.reconnecting
		voice.RUnlock()
		if reconnecting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("voice reconnect routine did not enter reconnecting state")
		}
		time.Sleep(time.Millisecond)
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- session.Close() }()
	select {
	case err = <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Session.Close waited for a canceled voice reconnect routine")
	}
	assertGatewayClosed(t, session)
}

func TestServerHeartbeatBypassesGatewayRateLimitQueue(t *testing.T) {
	serverResult := make(chan error, 1)
	server := newGatewayTestServer(t, func(server *gatewayTestServer, _ int, ws *websocket.Conn) {
		fail := func(err error) {
			select {
			case serverResult <- err:
			default:
			}
		}

		if err := writeGatewayHello(ws, time.Hour); err != nil {
			fail(err)
			return
		}
		operation, _, err := readGatewayOperation(ws)
		if err != nil {
			fail(err)
			return
		}
		if operation != 2 {
			fail(fmt.Errorf("first Gateway operation = %d, want identify", operation))
			return
		}
		if err := writeGatewayReady(ws, server.url, "heartbeat-priority"); err != nil {
			fail(err)
			return
		}

		for i := 0; i < gatewaySendLimit-1; i++ {
			operation, _, err = readGatewayOperation(ws)
			if err != nil {
				fail(err)
				return
			}
			if operation != 3 {
				fail(fmt.Errorf("application Gateway operation %d = %d, want update status", i, operation))
				return
			}
		}

		if err := ws.WriteJSON(map[string]interface{}{"op": 1, "d": nil}); err != nil {
			fail(err)
			return
		}
		_ = ws.SetReadDeadline(time.Now().Add(time.Second))
		operation, _, err = readGatewayOperation(ws)
		if err != nil {
			fail(fmt.Errorf("timed out waiting for server heartbeat response: %w", err))
			return
		}
		if operation != 1 {
			fail(fmt.Errorf("server heartbeat response operation = %d, want heartbeat", operation))
			return
		}
		serverResult <- nil
	})

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.gateway = server.url
	if err := session.Open(); err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	for i := 0; i < gatewaySendLimit-1; i++ {
		if err := session.GatewayWriteStruct(map[string]interface{}{
			"op": 3,
			"d":  map[string]interface{}{},
		}); err != nil {
			t.Fatalf("application Gateway write %d: %v", i, err)
		}
	}

	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server heartbeat response test timed out")
	}
}
