package dgo

import (
	"testing"
	"time"
)

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
