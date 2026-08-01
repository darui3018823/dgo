package mls

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/credentials"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
	"github.com/thomas-vilte/mls-go/keypackages"
)

type testExternalSender struct {
	raw []byte
	key *ciphersuite.SignaturePrivateKey
}

func TestGroupSessionProposalCommitWelcomeAndReplay(t *testing.T) {
	groupID := testUint64Bytes(9001)
	external := newTestExternalSender(t)

	alice := newTestGroupSession(t, 1, groupID, external.raw, "1", "2")
	bob := newTestGroupSession(t, 2, groupID, external.raw, "1", "2")
	bobKeyPackage := testKeyPackage(t, bob)
	addBob := testAddProposal(t, groupID, 0, bobKeyPackage, external.key)

	// A revoke removes the cached proposal and therefore produces no commit.
	if outgoing, err := alice.ProcessProposals(testAppendOperation(addBob)); err != nil {
		t.Fatalf("ProcessProposals(add Bob): %v", err)
	} else if len(outgoing) == 0 {
		t.Fatal("ProcessProposals(add Bob) returned no commit")
	}
	proposalRef := testProposalRef(t, addBob)
	outgoing, err := alice.ProcessProposals(testRevokeOperation(proposalRef))
	if err != nil {
		t.Fatalf("ProcessProposals(revoke Bob): %v", err)
	}
	if len(outgoing) != 0 {
		t.Fatalf("revoke produced %d bytes, want no commit", len(outgoing))
	}

	outgoing, err = alice.ProcessProposals(testAppendOperation(addBob))
	if err != nil {
		t.Fatalf("ProcessProposals(re-add Bob): %v", err)
	}
	commit, welcome := testSplitCommitWelcome(t, alice, outgoing)
	if len(welcome) == 0 {
		t.Fatal("Add commit did not include a raw Welcome")
	}

	if err := alice.ProcessCommit(commit); err != nil {
		t.Fatalf("alice ProcessCommit: %v", err)
	}
	if err := bob.ProcessWelcome(welcome); err != nil {
		t.Fatalf("bob ProcessWelcome: %v", err)
	}
	testExporterSecretsEqual(t, alice, bob)

	aliceEpoch, err := alice.Epoch()
	if err != nil {
		t.Fatalf("alice Epoch: %v", err)
	}
	if aliceEpoch != 1 {
		t.Fatalf("alice epoch = %d, want 1", aliceEpoch)
	}

	if err := alice.ProcessCommit(commit); err == nil {
		t.Fatal("replayed commit was accepted")
	}
	if err := bob.ProcessWelcome(welcome); err == nil {
		t.Fatal("replayed Welcome was accepted")
	}
	testExporterSecretsEqual(t, alice, bob)
}

func TestGroupSessionPendingGroupUsesSentKeyPackage(t *testing.T) {
	groupID := testUint64Bytes(9000)
	external := newTestExternalSender(t)

	session, err := NewGroupSession(testUint64Bytes(1))
	if err != nil {
		t.Fatalf("NewGroupSession: %v", err)
	}
	if err := session.Reset(groupID, daveProtocolVersion); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	sentKeyPackage := testKeyPackage(t, session)
	if err := session.SetExternalSender(external.raw); err != nil {
		t.Fatalf("SetExternalSender: %v", err)
	}

	session.mu.Lock()
	localGroup, err := session.loadGroupLocked()
	session.mu.Unlock()
	if err != nil {
		t.Fatalf("load pending group: %v", err)
	}
	members := localGroup.GetMembers()
	if len(members) != 1 || members[0] == nil || members[0].KeyPackage == nil {
		t.Fatalf("pending group members = %#v, want one member with a KeyPackage", members)
	}
	if !bytes.Equal(members[0].KeyPackage.Marshal(), sentKeyPackage.Marshal()) {
		t.Fatal("pending group creator KeyPackage differs from the KeyPackage sent to the voice gateway")
	}
}

func TestGroupSessionCompetingCommitRollsBackCandidate(t *testing.T) {
	groupID := testUint64Bytes(9002)
	external := newTestExternalSender(t)

	alice := newTestGroupSession(t, 1, groupID, external.raw, "1", "2")
	bob := newTestGroupSession(t, 2, groupID, external.raw, "1", "2")
	bobKeyPackage := testKeyPackage(t, bob)
	addBob := testAddProposal(t, groupID, 0, bobKeyPackage, external.key)

	initial, err := alice.ProcessProposals(testAppendOperation(addBob))
	if err != nil {
		t.Fatalf("alice add Bob proposal: %v", err)
	}
	initialCommit, bobWelcome := testSplitCommitWelcome(t, alice, initial)
	if err := alice.ProcessCommit(initialCommit); err != nil {
		t.Fatalf("alice initial commit: %v", err)
	}
	if err := bob.ProcessWelcome(bobWelcome); err != nil {
		t.Fatalf("bob initial Welcome: %v", err)
	}

	alice.SetRecognizedUsers([]string{"1", "2", "3"})
	bob.SetRecognizedUsers([]string{"1", "2", "3"})
	charlie := newTestGroupSession(t, 3, groupID, external.raw, "1", "2", "3")
	charlieKeyPackage := testKeyPackage(t, charlie)
	addCharlie := testAddProposal(t, groupID, 1, charlieKeyPackage, external.key)

	aliceOutgoing, err := alice.ProcessProposals(testAppendOperation(addCharlie))
	if err != nil {
		t.Fatalf("alice add Charlie proposal: %v", err)
	}
	aliceCommit, _ := testSplitCommitWelcome(t, alice, aliceOutgoing)

	bobOutgoing, err := bob.ProcessProposals(testAppendOperation(addCharlie))
	if err != nil {
		t.Fatalf("bob add Charlie proposal: %v", err)
	}
	bobCommit, charlieWelcome := testSplitCommitWelcome(t, bob, bobOutgoing)
	if bytes.Equal(aliceCommit, bobCommit) {
		t.Fatal("competing commits are unexpectedly identical")
	}

	// The delivery service chooses Bob's commit. Alice must discard her
	// candidate state and apply Bob's commit to the proposal-cached state.
	if err := alice.ProcessCommit(bobCommit); err != nil {
		t.Fatalf("alice ProcessCommit(Bob wins): %v", err)
	}
	if err := bob.ProcessCommit(bobCommit); err != nil {
		t.Fatalf("bob ProcessCommit(own commit): %v", err)
	}
	if err := charlie.ProcessWelcome(charlieWelcome); err != nil {
		t.Fatalf("charlie ProcessWelcome: %v", err)
	}
	testExporterSecretsEqual(t, alice, bob, charlie)

	for name, session := range map[string]*GroupSession{
		"alice":   alice,
		"bob":     bob,
		"charlie": charlie,
	} {
		epoch, err := session.Epoch()
		if err != nil {
			t.Fatalf("%s Epoch: %v", name, err)
		}
		if epoch != 2 {
			t.Fatalf("%s epoch = %d, want 2", name, epoch)
		}
	}

	if err := alice.ProcessCommit(aliceCommit); err == nil {
		t.Fatal("out-of-order losing commit was accepted")
	}
	testExporterSecretsEqual(t, alice, bob, charlie)
}

func TestGroupSessionRejectsMalformedAndUnrecognizedProposalsAtomically(t *testing.T) {
	groupID := testUint64Bytes(9003)
	external := newTestExternalSender(t)
	alice := newTestGroupSession(t, 1, groupID, external.raw, "1")
	mallory := newTestGroupSession(t, 99, groupID, external.raw, "99")
	addMallory := testAddProposal(t, groupID, 0, testKeyPackage(t, mallory), external.key)

	if _, err := alice.ProcessProposals(testAppendOperation(addMallory)); err == nil {
		t.Fatal("unrecognized Add proposal was accepted")
	}

	alice.SetRecognizedUsers([]string{"1", "99"})
	malformedMessages := append(append([]byte(nil), addMallory...), 0)
	malformed := append([]byte{daveProposalAppend}, testDAVEVector(malformedMessages)...)
	if _, err := alice.ProcessProposals(malformed); err == nil {
		t.Fatal("proposal vector with a valid proposal followed by malformed data was accepted")
	}

	outgoing, err := alice.ProcessProposals(testAppendOperation(addMallory))
	if err != nil {
		t.Fatalf("valid proposal after rejected payloads: %v", err)
	}
	commit, _ := testSplitCommitWelcome(t, alice, outgoing)
	if err := alice.ProcessCommit(commit); err != nil {
		t.Fatalf("valid commit after rejected payloads: %v", err)
	}
}

func newTestGroupSession(
	t *testing.T,
	userID uint64,
	groupID, externalSender []byte,
	recognized ...string,
) *GroupSession {
	t.Helper()
	session, err := NewGroupSession(testUint64Bytes(userID))
	if err != nil {
		t.Fatalf("NewGroupSession(%d): %v", userID, err)
	}
	session.SetRecognizedUsers(recognized)
	if err := session.Reset(groupID, daveProtocolVersion); err != nil {
		t.Fatalf("Reset(%d): %v", userID, err)
	}
	// The gateway receives the KeyPackage before it provides opcode 25. The
	// eventual pending group must retain this exact creator leaf.
	testKeyPackage(t, session)
	if err := session.SetExternalSender(externalSender); err != nil {
		t.Fatalf("SetExternalSender(%d): %v", userID, err)
	}
	return session
}

func newTestExternalSender(t *testing.T) testExternalSender {
	t.Helper()
	credential, key, err := credentials.GenerateCredentialWithKeyForCS(
		[]byte("discord voice delivery service"),
		daveCipherSuite,
	)
	if err != nil {
		t.Fatalf("GenerateCredentialWithKeyForCS: %v", err)
	}
	raw := append(testDAVEVector(key.PublicKey().AsSlice()), credential.Credential.Marshal()...)
	return testExternalSender{raw: raw, key: key}
}

func testKeyPackage(t *testing.T, session *GroupSession) *keypackages.KeyPackage {
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

func testAddProposal(
	t *testing.T,
	groupID []byte,
	epoch uint64,
	keyPackage *keypackages.KeyPackage,
	externalKey *ciphersuite.SignaturePrivateKey,
) []byte {
	t.Helper()
	proposal := group.NewAddProposal(keyPackage)
	content := framing.FramedContent{
		GroupID: groupID,
		Epoch:   epoch,
		Sender: framing.Sender{
			Type:        framing.SenderTypeExternal,
			SenderIndex: 0,
		},
		Body: framing.ProposalBody{Data: group.ProposalMarshal(proposal)},
	}
	publicMessage, err := framing.NewPublicMessage(
		content,
		externalKey,
		nil,
		nil,
		daveCipherSuite,
	)
	if err != nil {
		t.Fatalf("NewPublicMessage: %v", err)
	}
	return framing.NewMLSMessagePublic(publicMessage).Marshal()
}

func testProposalRef(t *testing.T, proposal []byte) []byte {
	t.Helper()
	message, err := framing.UnmarshalMLSMessage(proposal)
	if err != nil {
		t.Fatalf("UnmarshalMLSMessage(proposal): %v", err)
	}
	publicMessage, ok := message.AsPublic()
	if !ok {
		t.Fatal("proposal is not a PublicMessage")
	}
	authenticated := &framing.AuthenticatedContent{
		WireFormat: framing.WireFormatPublicMessage,
		Content:    publicMessage.Content,
		Auth:       publicMessage.Auth,
	}
	return group.ComputeProposalRef(authenticated.Marshal(), daveCipherSuite)
}

func testAppendOperation(proposals ...[]byte) []byte {
	var encoded []byte
	for _, proposal := range proposals {
		encoded = append(encoded, proposal...)
	}
	return append([]byte{daveProposalAppend}, testDAVEVector(encoded)...)
}

func testRevokeOperation(refs ...[]byte) []byte {
	var encoded []byte
	for _, ref := range refs {
		encoded = append(encoded, testDAVEVector(ref)...)
	}
	return append([]byte{daveProposalRevoke}, testDAVEVector(encoded)...)
}

func testDAVEVector(data []byte) []byte {
	length := len(data)
	switch {
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

func testSplitCommitWelcome(
	t *testing.T,
	session *GroupSession,
	outgoing []byte,
) (commit, welcome []byte) {
	t.Helper()
	commit = append([]byte(nil), session.pendingCommit...)
	if len(commit) == 0 || len(commit) > len(outgoing) ||
		!bytes.Equal(commit, outgoing[:len(commit)]) {
		t.Fatal("opcode 28 payload does not begin with a canonical commit")
	}
	return commit, outgoing[len(commit):]
}

func testExporterSecretsEqual(t *testing.T, sessions ...*GroupSession) {
	t.Helper()
	var expected []byte
	for i, session := range sessions {
		exported, err := session.Export("test DAVE exporter", nil, 32)
		if err != nil {
			t.Fatalf("session %d Export: %v", i, err)
		}
		if i == 0 {
			expected = exported
		} else if !bytes.Equal(expected, exported) {
			t.Fatalf("session %d exporter secret differs", i)
		}
	}
}

func testUint64Bytes(value uint64) []byte {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, value)
	return data
}
