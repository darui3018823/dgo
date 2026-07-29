package dgo

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
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
