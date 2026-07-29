package dgo

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type testWebsocketUpgradeResult struct {
	conn *websocket.Conn
	err  error
}

func testWebsocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	upgraded := make(chan testWebsocketUpgradeResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		}).Upgrade(w, r, nil)
		upgraded <- testWebsocketUpgradeResult{conn: conn, err: err}
	}))

	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}

	result := <-upgraded
	if result.err != nil {
		_ = client.Close()
		server.Close()
		t.Fatal(result.err)
	}

	t.Cleanup(func() {
		_ = client.Close()
		_ = result.conn.Close()
		server.Close()
	})
	return client, result.conn
}

func readVoiceOperation(t *testing.T, conn *websocket.Conn) voiceChannelJoinOp {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var operation voiceChannelJoinOp
	if err := conn.ReadJSON(&operation); err != nil {
		t.Fatal(err)
	}
	return operation
}

func TestVoicePublicAPIsGuardNilAndClosedState(t *testing.T) {
	v := &VoiceConnection{LogLevel: -1}

	if err := v.Speaking(true); !errors.Is(err, ErrVoiceConnectionClosed) {
		t.Fatalf("Speaking error = %v, want ErrVoiceConnectionClosed", err)
	}
	if err := v.ChangeChannel("channel", false, false); !errors.Is(err, ErrVoiceSessionUnavailable) {
		t.Fatalf("ChangeChannel error = %v, want ErrVoiceSessionUnavailable", err)
	}
	if err := v.Disconnect(); !errors.Is(err, ErrVoiceSessionUnavailable) {
		t.Fatalf("Disconnect error = %v, want ErrVoiceSessionUnavailable", err)
	}

	v.Close()
	v.Close()
	v.RekeyDAVE()
	v.AddHandler(nil)
	v.AddClientsConnectHandler(nil)

	if len(v.voiceSpeakingUpdateHandlers) != 0 {
		t.Fatal("AddHandler(nil) registered a handler")
	}
	if len(v.voiceClientsConnectHandlers) != 0 {
		t.Fatal("AddClientsConnectHandler(nil) registered a handler")
	}
}

func TestVoicePublicSendAndModeOperations(t *testing.T) {
	voiceClient, voicePeer := testWebsocketPair(t)
	gatewayClient, gatewayPeer := testWebsocketPair(t)

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.wsConn = gatewayClient
	session.VoiceConnections = make(map[string]*VoiceConnection)

	v := &VoiceConnection{
		LogLevel:  -1,
		GuildID:   "guild",
		ChannelID: "old-channel",
		sessionID: "session",
		session:   session,
		wsConn:    voiceClient,
	}
	session.VoiceConnections[v.GuildID] = v

	if err = v.Speaking(true); err != nil {
		t.Fatal(err)
	}
	if err = voicePeer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var speaking struct {
		Op int `json:"op"`
	}
	if err = voicePeer.ReadJSON(&speaking); err != nil {
		t.Fatal(err)
	}
	if speaking.Op != 5 {
		t.Fatalf("Speaking opcode = %d, want 5", speaking.Op)
	}

	if err = v.ChangeChannel("new-channel", true, false); err != nil {
		t.Fatal(err)
	}
	change := readVoiceOperation(t, gatewayPeer)
	if change.Op != 4 || change.Data.ChannelID == nil || *change.Data.ChannelID != "new-channel" {
		t.Fatalf("ChangeChannel operation = %#v", change)
	}
	v.RLock()
	channelID, mute, deaf, speakingState := v.ChannelID, v.mute, v.deaf, v.speaking
	v.RUnlock()
	if channelID != "new-channel" || !mute || deaf || speakingState {
		t.Fatalf("voice mode = channel %q, mute %t, deaf %t, speaking %t", channelID, mute, deaf, speakingState)
	}

	if err = v.Disconnect(); err != nil {
		t.Fatal(err)
	}
	disconnect := readVoiceOperation(t, gatewayPeer)
	if disconnect.Op != 4 || disconnect.Data.ChannelID != nil {
		t.Fatalf("Disconnect operation = %#v", disconnect)
	}
	session.RLock()
	_, exists := session.VoiceConnections[v.GuildID]
	session.RUnlock()
	if exists {
		t.Fatal("Disconnect left the voice connection registered")
	}
}

func TestVoicePublicAPIsRaceWithClose(t *testing.T) {
	voiceClient, voicePeer := testWebsocketPair(t)
	gatewayClient, gatewayPeer := testWebsocketPair(t)

	session, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	session.wsConn = gatewayClient
	session.VoiceConnections = make(map[string]*VoiceConnection)

	v := &VoiceConnection{
		LogLevel:  -1,
		GuildID:   "guild",
		ChannelID: "channel",
		sessionID: "session",
		session:   session,
		wsConn:    voiceClient,
	}
	session.VoiceConnections[v.GuildID] = v

	drain := func(conn *websocket.Conn) {
		for {
			if _, _, readErr := conn.ReadMessage(); readErr != nil {
				return
			}
		}
	}
	go drain(voicePeer)
	go drain(gatewayPeer)

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(3)
		go func(speaking bool) {
			defer wg.Done()
			<-start
			_ = v.Speaking(speaking)
		}(i%2 == 0)
		go func(channel int) {
			defer wg.Done()
			<-start
			_ = v.ChangeChannel("channel-"+string(rune('a'+channel%26)), channel%2 == 0, channel%3 == 0)
		}(i)
		go func() {
			defer wg.Done()
			<-start
			v.AddHandler(func(*VoiceConnection, *VoiceSpeakingUpdate) {})
			v.AddClientsConnectHandler(func(*VoiceConnection, *VoiceClientsConnect) {})
			v.RekeyDAVE()
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		v.Close()
	}()
	go func() {
		defer wg.Done()
		<-start
		v.Close()
	}()

	close(start)
	wg.Wait()

	v.RLock()
	wsConn := v.wsConn
	v.RUnlock()
	if wsConn != nil {
		t.Fatal("Close left the voice websocket attached")
	}
	if err = v.Speaking(true); !errors.Is(err, ErrVoiceConnectionClosed) {
		t.Fatalf("Speaking after Close error = %v, want ErrVoiceConnectionClosed", err)
	}
}

func TestVoiceReadyBeforeHelloDoesNotStartHeartbeat(t *testing.T) {
	v := &VoiceConnection{LogLevel: -1}
	v.onEvent(false, []byte(`{"op":2,"d":{"ssrc":1,"ip":"127.0.0.1","port":5000,"modes":["aead_aes256_gcm_rtpsize"]}}`))

	if v.udpConn != nil {
		t.Fatal("voice READY before HELLO opened UDP")
	}
	if v.Ready {
		t.Fatal("voice READY before HELLO marked the connection ready")
	}
}

func TestVoiceHeartbeatAckUpdatesLatency(t *testing.T) {
	v := &VoiceConnection{LogLevel: -1}
	v.lastHeartbeatNonce = 1234
	v.awaitingHeartbeat = true
	v.LastHeartbeatSent = time.Now().Add(-10 * time.Millisecond)

	v.onEvent(false, []byte(`{"op":6,"d":{"t":1234}}`))

	if v.awaitingHeartbeat {
		t.Fatal("matching heartbeat ACK did not clear awaiting state")
	}
	if v.LastHeartbeatAck.IsZero() {
		t.Fatal("matching heartbeat ACK did not record acknowledgement time")
	}
	if v.HeartbeatLatency <= 0 {
		t.Fatalf("heartbeat latency = %s, want positive duration", v.HeartbeatLatency)
	}
}

func TestVoiceClientsConnectOpcode(t *testing.T) {
	v := &VoiceConnection{LogLevel: -1}
	handlerCalled := false
	v.AddClientsConnectHandler(func(_ *VoiceConnection, event *VoiceClientsConnect) {
		handlerCalled = len(event.UserIDs) == 2
	})

	v.onEvent(false, []byte(`{"op":11,"d":{"user_ids":["1","2"]}}`))

	if !handlerCalled {
		t.Fatal("Clients Connect handler was not called for opcode 11")
	}
	if len(v.ConnectedUsers) != 2 || v.ConnectedUsers[0] != "1" || v.ConnectedUsers[1] != "2" {
		t.Fatalf("connected users = %v, want [1 2]", v.ConnectedUsers)
	}
}

func TestClassifyVoiceCloseCode(t *testing.T) {
	tests := map[int]voiceCloseAction{
		4014: voiceCloseWaitForServerUpdate,
		4015: voiceCloseReconnect,
		4017: voiceCloseTerminal,
		4021: voiceCloseTerminal,
		4022: voiceCloseTerminal,
	}
	for code, want := range tests {
		if got := classifyVoiceCloseCode(code); got != want {
			t.Errorf("classifyVoiceCloseCode(%d) = %d, want %d", code, got, want)
		}
	}
}

func TestVoiceHeartbeatRejectsInvalidInterval(t *testing.T) {
	v := &VoiceConnection{LogLevel: -1}
	v.wsHeartbeat(nil, nil, 0)
}

func testVoiceAEAD(t testing.TB) cipher.AEAD {
	t.Helper()
	block, err := aes.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	return aead
}

func sealVoicePacket(t testing.TB, aead cipher.AEAD, header, plaintext []byte, nonceValue uint32) []byte {
	t.Helper()
	nonce := make([]byte, aead.NonceSize())
	binary.LittleEndian.PutUint32(nonce, nonceValue)
	packet := append([]byte(nil), header...)
	packet = aead.Seal(packet, nonce, plaintext, header)
	return append(packet, nonce[:4]...)
}

func TestDecodeVoicePacket(t *testing.T) {
	aead := testVoiceAEAD(t)
	header := make([]byte, 12)
	header[0] = 0x80
	header[1] = 0x78
	binary.BigEndian.PutUint16(header[2:4], 7)
	binary.BigEndian.PutUint32(header[4:8], 960)
	binary.BigEndian.PutUint32(header[8:12], 42)

	packet, err := decodeVoicePacket(sealVoicePacket(t, aead, header, []byte("opus"), 1), aead)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Sequence != 7 || packet.Timestamp != 960 || packet.SSRC != 42 || string(packet.Opus) != "opus" {
		t.Fatalf("decoded packet = %#v", packet)
	}
}

func TestDecodeVoicePacketExtension(t *testing.T) {
	aead := testVoiceAEAD(t)
	header := make([]byte, 16)
	header[0] = 0x90
	header[1] = 0x78
	binary.BigEndian.PutUint16(header[14:16], 1)
	extensionData := []byte{1, 2, 3, 4}
	plaintext := append(append([]byte(nil), extensionData...), []byte("opus")...)

	packet, err := decodeVoicePacket(sealVoicePacket(t, aead, header, plaintext, 2), aead)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.Extension) != 8 || string(packet.Extension[4:]) != string(extensionData) {
		t.Fatalf("decoded extension = %x", packet.Extension)
	}
	if string(packet.Opus) != "opus" {
		t.Fatalf("decoded opus = %q", packet.Opus)
	}
}

func TestDecodeVoicePacketRejectsMalformedBounds(t *testing.T) {
	aead := testVoiceAEAD(t)
	tests := [][]byte{
		nil,
		make([]byte, 11),
		append([]byte{0x8f}, make([]byte, 11)...),
		append([]byte{0x90}, make([]byte, 11)...),
		append([]byte{0x80}, make([]byte, 20)...),
	}
	for _, data := range tests {
		if _, err := decodeVoicePacket(data, aead); err == nil {
			t.Errorf("decodeVoicePacket(%x) succeeded", data)
		}
	}
}

func FuzzDecodeVoicePacket(f *testing.F) {
	aead := testVoiceAEAD(f)
	f.Add([]byte{})
	f.Add(make([]byte, 12))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeVoicePacket(data, aead)
	})
}
