package dgo

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/credentials"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
	"github.com/thomas-vilte/mls-go/keypackages"
)

func TestVoiceDAVEProposalCommitWelcomeLifecycle(t *testing.T) {
	const (
		channelID   = "9004"
		transition  = uint16(12)
		aliceUserID = "1"
		bobUserID   = "2"
	)
	groupID := make([]byte, 8)
	binary.BigEndian.PutUint64(groupID, 9004)
	externalSender, externalKey := testDAVEExternalSender(t)

	alice := NewDAVESession(aliceUserID)
	if err := alice.Configure(channelID, 1, []string{aliceUserID, bobUserID}); err != nil {
		t.Fatalf("alice Configure: %v", err)
	}
	// Generate the packet that the gateway would receive before the external
	// sender arrives. The pending group must use this exact KeyPackage leaf.
	testDAVEKeyPackage(t, alice)
	if err := alice.HandleExternalSenderPackage(externalSender); err != nil {
		t.Fatalf("alice HandleExternalSenderPackage: %v", err)
	}

	bob := NewDAVESession(bobUserID)
	if err := bob.Configure(channelID, 1, []string{aliceUserID, bobUserID}); err != nil {
		t.Fatalf("bob Configure: %v", err)
	}
	if err := bob.HandleExternalSenderPackage(externalSender); err != nil {
		t.Fatalf("bob HandleExternalSenderPackage: %v", err)
	}
	bobKeyPackage := testDAVEKeyPackage(t, bob)
	addBob := testDAVEAddProposal(t, groupID, bobKeyPackage, externalKey)
	proposalOperation := append([]byte{0}, testDAVETLSVector(addBob)...)

	aliceClient, alicePeer := testWebsocketPair(t)
	aliceVoice := &VoiceConnection{
		LogLevel: -1,
		dave:     alice,
		wsConn:   aliceClient,
	}
	aliceVoice.handleDAVEBinary(testDAVEBinaryMessage(1, 27, proposalOperation))

	messageType, opcode28 := readDAVETestMessage(t, alicePeer)
	if messageType != websocket.BinaryMessage || len(opcode28) < 2 || opcode28[0] != 28 {
		t.Fatalf("opcode 27 response type=%d payload=%x, want binary opcode 28", messageType, opcode28)
	}
	commit, welcome := splitDAVECommitWelcome(t, opcode28[1:])
	if len(welcome) == 0 {
		t.Fatal("opcode 28 Add response did not contain a raw Welcome")
	}

	commitPayload := make([]byte, 2, 2+len(commit))
	binary.BigEndian.PutUint16(commitPayload, transition)
	commitPayload = append(commitPayload, commit...)
	aliceVoice.handleDAVEBinary(testDAVEBinaryMessage(2, 29, commitPayload))
	assertDAVEReadyOperation(t, alicePeer, transition)
	aliceVoice.handleDAVEExecuteTransition(
		json.RawMessage(`{"transition_id":12}`),
	)
	if !alice.IsActive() {
		t.Fatal("opcode 29 lifecycle did not activate Alice's DAVE session")
	}

	bobClient, bobPeer := testWebsocketPair(t)
	bobVoice := &VoiceConnection{
		LogLevel: -1,
		dave:     bob,
		wsConn:   bobClient,
	}
	welcomePayload := make([]byte, 2, 2+len(welcome))
	binary.BigEndian.PutUint16(welcomePayload, transition)
	welcomePayload = append(welcomePayload, welcome...)
	bobVoice.handleDAVEBinary(testDAVEBinaryMessage(1, 30, welcomePayload))
	assertDAVEReadyOperation(t, bobPeer, transition)
	bobVoice.handleDAVEExecuteTransition(
		json.RawMessage(`{"transition_id":12}`),
	)
	if !bob.IsActive() {
		t.Fatal("opcode 30 lifecycle did not activate Bob's DAVE session")
	}

	// A replayed Welcome must trigger opcode 31 and a fresh opcode 26 instead
	// of another ready notification or state transition.
	bobVoice.handleDAVEBinary(testDAVEBinaryMessage(2, 30, welcomePayload))
	assertDAVEInvalidOperation(t, bobPeer, transition)
	messageType, keyPackage := readDAVETestMessage(t, bobPeer)
	if messageType != websocket.BinaryMessage || len(keyPackage) < 2 || keyPackage[0] != 26 {
		t.Fatalf("Welcome recovery type=%d payload=%x, want binary opcode 26", messageType, keyPackage)
	}
	if bob.IsActive() {
		t.Fatal("Welcome replay recovery left DAVE active")
	}
}

func testDAVEExternalSender(t *testing.T) ([]byte, *ciphersuite.SignaturePrivateKey) {
	t.Helper()
	credential, key, err := credentials.GenerateCredentialWithKeyForCS(
		[]byte("discord voice delivery service"),
		ciphersuite.MLS128DHKEMP256,
	)
	if err != nil {
		t.Fatalf("GenerateCredentialWithKeyForCS: %v", err)
	}
	raw := append(testDAVETLSVector(key.PublicKey().AsSlice()), credential.Credential.Marshal()...)
	return raw, key
}

func testDAVEKeyPackage(t *testing.T, session *DAVESession) *keypackages.KeyPackage {
	t.Helper()
	raw, err := session.GenerateKeyPackage()
	if err != nil {
		t.Fatalf("GenerateKeyPackage: %v", err)
	}
	keyPackage, err := keypackages.UnmarshalKeyPackage(raw)
	if err != nil {
		t.Fatalf("UnmarshalKeyPackage: %v", err)
	}
	return keyPackage
}

func testDAVEAddProposal(
	t *testing.T,
	groupID []byte,
	keyPackage *keypackages.KeyPackage,
	externalKey *ciphersuite.SignaturePrivateKey,
) []byte {
	t.Helper()
	content := framing.FramedContent{
		GroupID: groupID,
		Epoch:   0,
		Sender: framing.Sender{
			Type:        framing.SenderTypeExternal,
			SenderIndex: 0,
		},
		Body: framing.ProposalBody{
			Data: group.ProposalMarshal(group.NewAddProposal(keyPackage)),
		},
	}
	publicMessage, err := framing.NewPublicMessage(
		content,
		externalKey,
		nil,
		nil,
		ciphersuite.MLS128DHKEMP256,
	)
	if err != nil {
		t.Fatalf("NewPublicMessage: %v", err)
	}
	return framing.NewMLSMessagePublic(publicMessage).Marshal()
}

func testDAVETLSVector(data []byte) []byte {
	switch length := len(data); {
	case length < 64:
		return append([]byte{byte(length)}, data...)
	case length < 16384:
		prefix := make([]byte, 2)
		binary.BigEndian.PutUint16(prefix, uint16(length)|0x4000)
		return append(prefix, data...)
	default:
		prefix := make([]byte, 4)
		binary.BigEndian.PutUint32(prefix, uint32(length)|0x80000000)
		return append(prefix, data...)
	}
}

func testDAVEBinaryMessage(sequence uint16, opcode byte, payload []byte) []byte {
	message := make([]byte, 3, 3+len(payload))
	binary.BigEndian.PutUint16(message, sequence)
	message[2] = opcode
	return append(message, payload...)
}

func splitDAVECommitWelcome(t *testing.T, payload []byte) (commit, welcome []byte) {
	t.Helper()
	for length := 4; length <= len(payload); length++ {
		message, err := framing.UnmarshalMLSMessage(payload[:length])
		if err != nil || message.PublicMessage == nil {
			continue
		}
		encoded := message.Marshal()
		if bytes.Equal(encoded, payload[:length]) {
			return append([]byte(nil), encoded...), append([]byte(nil), payload[length:]...)
		}
	}
	t.Fatal("opcode 28 payload did not contain a canonical commit")
	return nil, nil
}

func readDAVETestMessage(t *testing.T, conn *websocket.Conn) (int, []byte) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return messageType, payload
}

func assertDAVEReadyOperation(t *testing.T, conn *websocket.Conn, transitionID uint16) {
	t.Helper()
	messageType, payload := readDAVETestMessage(t, conn)
	if messageType != websocket.TextMessage {
		t.Fatalf("ready message type = %d, want text", messageType)
	}
	var operation struct {
		Op   int `json:"op"`
		Data struct {
			TransitionID uint16 `json:"transition_id"`
		} `json:"d"`
	}
	if err := json.Unmarshal(payload, &operation); err != nil {
		t.Fatalf("unmarshal ready operation: %v", err)
	}
	if operation.Op != 23 || operation.Data.TransitionID != transitionID {
		t.Fatalf("ready operation = %#v", operation)
	}
}

func assertDAVEInvalidOperation(t *testing.T, conn *websocket.Conn, transitionID uint16) {
	t.Helper()
	messageType, payload := readDAVETestMessage(t, conn)
	if messageType != websocket.TextMessage {
		t.Fatalf("invalid commit/Welcome message type = %d, want text", messageType)
	}
	var operation struct {
		Op   int `json:"op"`
		Data struct {
			TransitionID uint16 `json:"transition_id"`
		} `json:"d"`
	}
	if err := json.Unmarshal(payload, &operation); err != nil {
		t.Fatalf("unmarshal invalid commit/Welcome operation: %v", err)
	}
	if operation.Op != 31 || operation.Data.TransitionID != transitionID {
		t.Fatalf("invalid commit/Welcome operation = %#v", operation)
	}
}
