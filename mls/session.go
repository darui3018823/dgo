package mls

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strconv"
	"sync"

	mlscore "github.com/thomas-vilte/mls-go"
	"github.com/thomas-vilte/mls-go/ciphersuite"
	"github.com/thomas-vilte/mls-go/credentials"
	"github.com/thomas-vilte/mls-go/extensions"
	"github.com/thomas-vilte/mls-go/framing"
	"github.com/thomas-vilte/mls-go/group"
	"github.com/thomas-vilte/mls-go/keypackages"
	"github.com/thomas-vilte/mls-go/storage/memory"
)

const (
	daveProtocolVersion = 1
	daveCipherSuite     = ciphersuite.MLS128DHKEMP256

	daveProposalAppend = 0
	daveProposalRevoke = 1

	maxDAVEControlMessageSize = 16 << 20
)

// GroupSession owns the complete MLS state used by a DAVE voice session.
//
// Discord's delivery service chooses the first commit it receives. A client
// must therefore retain both the state containing the pending proposals and
// the state produced by its own commit until opcode 29 identifies the winning
// commit. GroupSession keeps those two snapshots explicitly.
type GroupSession struct {
	mu sync.Mutex

	identity []byte
	groupID  []byte

	client *mlscore.Client
	store  *memory.Store

	externalSender []byte
	groupCreated   bool
	established    bool

	recognized map[string]struct{}

	pendingBaseState      []byte
	pendingCandidateState []byte
	pendingCommit         []byte
	pendingKeyPackage     []byte
}

// NewGroupSession creates an MLS client using the DAVE v1 cipher suite.
func NewGroupSession(identity []byte) (*GroupSession, error) {
	if len(identity) != 8 {
		return nil, fmt.Errorf("DAVE identity must be an 8-byte user ID")
	}

	s := &GroupSession{
		identity:   append([]byte(nil), identity...),
		recognized: map[string]struct{}{string(identity): {}},
	}
	if err := s.resetClientLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// Reset starts a fresh local epoch-0 group while retaining the current
// external sender and recognized voice roster.
func (s *GroupSession) Reset(groupID []byte, protocolVersion int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if protocolVersion != daveProtocolVersion {
		return fmt.Errorf("unsupported DAVE protocol version %d", protocolVersion)
	}
	if len(groupID) != 8 {
		return fmt.Errorf("DAVE group ID must be 8 bytes")
	}

	s.groupID = append(s.groupID[:0], groupID...)
	if err := s.resetClientLocked(); err != nil {
		return err
	}
	return s.createPendingGroupLocked()
}

func (s *GroupSession) resetClientLocked() error {
	if s.client != nil {
		_ = s.client.Close()
	}

	s.store = memory.NewStore()
	client, err := mlscore.NewClient(
		s.identity,
		daveCipherSuite,
		mlscore.WithStorage(s.store, s.store),
		mlscore.WithCacheStrategy(mlscore.CacheNone),
		mlscore.WithProposalPolicy(s),
	)
	if err != nil {
		return fmt.Errorf("creating MLS client: %w", err)
	}

	s.client = client
	s.groupCreated = false
	s.established = false
	s.clearPendingCommitLocked()
	s.pendingKeyPackage = nil
	return nil
}

// SetRecognizedUsers replaces the voice roster used to authorize Add
// proposals and validate resulting group membership. The local user is always
// retained.
func (s *GroupSession) SetRecognizedUsers(userIDs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	recognized := map[string]struct{}{string(s.identity): {}}
	for _, userID := range userIDs {
		identity, err := userIDIdentity(userID)
		if err == nil {
			recognized[string(identity)] = struct{}{}
		}
	}
	s.recognized = recognized
}

// SetExternalSender validates opcode 25's raw ExternalSender and creates the
// local pending group once both it and the shared group ID are known.
func (s *GroupSession) SetExternalSender(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(data) == 0 || len(data) > maxDAVEControlMessageSize {
		return fmt.Errorf("invalid external sender size %d", len(data))
	}
	if _, err := extensions.ParseSingleExternalSender(data); err != nil {
		return fmt.Errorf("parsing external sender: %w", err)
	}

	if len(s.externalSender) > 0 && !bytes.Equal(s.externalSender, data) {
		return fmt.Errorf("external sender changed within a DAVE session")
	}
	s.externalSender = append(s.externalSender[:0], data...)
	return s.createPendingGroupLocked()
}

func (s *GroupSession) createPendingGroupLocked() error {
	if s.groupCreated || len(s.groupID) == 0 || len(s.externalSender) == 0 || len(s.pendingKeyPackage) == 0 {
		return nil
	}

	if _, err := s.client.CreateGroupWithExternalSender(
		context.Background(),
		s.groupID,
		s.pendingKeyPackage,
		s.externalSender,
	); err != nil {
		return fmt.Errorf("creating pending MLS group: %w", err)
	}

	s.groupCreated = true
	return s.validateGroupLocked(false)
}

// GenerateKeyPackage returns the raw, single-use KeyPackage payload for opcode
// 26. The same KeyPackage is also used to create the local pending group, so
// the initial Commit identifies the same LeafNode accepted by the voice
// gateway.
func (s *GroupSession) GenerateKeyPackage() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.groupID) == 0 {
		return nil, fmt.Errorf("DAVE group ID is not configured")
	}
	if len(s.pendingKeyPackage) == 0 {
		raw, err := s.client.FreshKeyPackageBytes(
			context.Background(),
			keypackages.InfiniteLifetime(),
		)
		if err != nil {
			return nil, fmt.Errorf("generating join key package: %w", err)
		}
		s.pendingKeyPackage = append(s.pendingKeyPackage[:0], raw...)
	}
	if err := s.createPendingGroupLocked(); err != nil {
		return nil, err
	}
	return append([]byte(nil), s.pendingKeyPackage...), nil
}

// ProcessProposals processes opcode 27 and returns the opcode 28 payload:
// MLSMessage(commit) followed by an optional raw Welcome.
func (s *GroupSession) ProcessProposals(data []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.groupCreated {
		return nil, fmt.Errorf("MLS group is not initialized")
	}
	if len(data) < 2 || len(data) > maxDAVEControlMessageSize {
		return nil, fmt.Errorf("invalid proposal payload size %d", len(data))
	}

	initialState, err := s.loadStateLocked()
	if err != nil {
		return nil, err
	}

	rollback := func(processErr error) ([]byte, error) {
		_ = s.saveStateLocked(initialState)
		return nil, processErr
	}

	switch data[0] {
	case daveProposalAppend:
		proposals, rest, err := readDAVEVector(data[1:])
		if err != nil {
			return rollback(fmt.Errorf("reading proposal vector: %w", err))
		}
		if len(rest) != 0 {
			return rollback(fmt.Errorf("trailing bytes after proposal vector"))
		}
		for len(proposals) > 0 {
			msg, err := framing.UnmarshalMLSMessage(proposals)
			if err != nil {
				return rollback(fmt.Errorf("parsing proposal MLSMessage: %w", err))
			}
			encoded := msg.Marshal()
			if len(encoded) == 0 || len(encoded) > len(proposals) ||
				!bytes.Equal(proposals[:len(encoded)], encoded) {
				return rollback(fmt.Errorf("non-canonical proposal MLSMessage"))
			}
			publicMessage, ok := msg.AsPublic()
			if !ok ||
				publicMessage.Content.ContentType() != framing.ContentTypeProposal ||
				publicMessage.Content.Sender.Type != framing.SenderTypeExternal {
				return rollback(fmt.Errorf("DAVE proposals must be public proposals from the external sender"))
			}
			if err := s.client.ProcessPublicMessage(
				context.Background(),
				s.groupID,
				encoded,
			); err != nil {
				return rollback(fmt.Errorf("processing external proposal: %w", err))
			}
			proposals = proposals[len(encoded):]
		}

	case daveProposalRevoke:
		encodedRefs, rest, err := readDAVEVector(data[1:])
		if err != nil {
			return rollback(fmt.Errorf("reading proposal reference vector: %w", err))
		}
		if len(rest) != 0 {
			return rollback(fmt.Errorf("trailing bytes after proposal reference vector"))
		}

		refs := make([][]byte, 0)
		for len(encodedRefs) > 0 {
			ref, remainder, err := readDAVEVector(encodedRefs)
			if err != nil {
				return rollback(fmt.Errorf("reading proposal reference: %w", err))
			}
			if len(ref) == 0 {
				return rollback(fmt.Errorf("empty proposal reference"))
			}
			refs = append(refs, ref)
			encodedRefs = remainder
		}
		if err := s.client.RevokeProposals(context.Background(), s.groupID, refs); err != nil {
			return rollback(fmt.Errorf("revoking proposals: %w", err))
		}

	default:
		return rollback(fmt.Errorf("unknown DAVE proposal operation %d", data[0]))
	}

	groupWithProposals, err := s.loadGroupLocked()
	if err != nil {
		return rollback(err)
	}
	if len(groupWithProposals.StoredProposals()) == 0 {
		s.clearPendingCommitLocked()
		return nil, nil
	}

	baseState, err := s.loadStateLocked()
	if err != nil {
		return rollback(err)
	}
	commit, welcome, err := s.client.CommitPendingProposals(
		context.Background(),
		s.groupID,
		mlscore.WithGroupInfoParams(
			group.WithRatchetTree(true),
			group.WithExternalPub(false),
		),
	)
	if err != nil {
		return rollback(fmt.Errorf("creating pending commit: %w", err))
	}
	if _, err := validateDAVECommit(commit, s.groupID, groupWithProposals.Epoch().AsUint64()); err != nil {
		return rollback(err)
	}

	candidateState, err := s.loadStateLocked()
	if err != nil {
		return rollback(err)
	}
	if err := s.validateGroupLocked(true); err != nil {
		return rollback(fmt.Errorf("validating candidate group: %w", err))
	}
	if err := s.saveStateLocked(baseState); err != nil {
		return rollback(err)
	}

	rawWelcome, err := rawWelcomeBytes(welcome)
	if err != nil {
		return rollback(err)
	}

	s.pendingBaseState = baseState
	s.pendingCandidateState = candidateState
	s.pendingCommit = append(s.pendingCommit[:0], commit...)

	result := make([]byte, 0, len(commit)+len(rawWelcome))
	result = append(result, commit...)
	result = append(result, rawWelcome...)
	return result, nil
}

// ProcessCommit applies opcode 29. An exact match selects the cached state
// produced by our own commit; a competing commit is applied to the retained
// proposal state.
func (s *GroupSession) ProcessCommit(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(data) == 0 || len(data) > maxDAVEControlMessageSize {
		return fmt.Errorf("invalid commit size %d", len(data))
	}
	if len(s.pendingBaseState) == 0 {
		return fmt.Errorf("commit received without pending proposals")
	}

	baseGroup, err := group.UnmarshalGroupState(s.pendingBaseState)
	if err != nil {
		return fmt.Errorf("unmarshaling pending proposal state: %w", err)
	}
	baseEpoch := baseGroup.Epoch().AsUint64()
	if _, err := validateDAVECommit(data, s.groupID, baseEpoch); err != nil {
		return err
	}
	if !s.established && !bytes.Equal(data, s.pendingCommit) {
		return fmt.Errorf("competing commit cannot establish an initial DAVE group")
	}

	if err := s.saveStateLocked(s.pendingBaseState); err != nil {
		return err
	}
	rollback := func(processErr error) error {
		_ = s.saveStateLocked(s.pendingBaseState)
		return processErr
	}

	if bytes.Equal(data, s.pendingCommit) {
		if len(s.pendingCandidateState) == 0 {
			return rollback(fmt.Errorf("missing cached state for our pending commit"))
		}
		if err := s.saveStateLocked(s.pendingCandidateState); err != nil {
			return rollback(err)
		}
	} else if err := s.client.ProcessCommit(context.Background(), s.groupID, data); err != nil {
		return rollback(fmt.Errorf("processing competing commit: %w", err))
	}

	epoch, err := s.client.Epoch(context.Background(), s.groupID)
	if err != nil {
		return rollback(fmt.Errorf("reading committed epoch: %w", err))
	}
	if epoch != baseEpoch+1 {
		return rollback(fmt.Errorf("commit advanced to epoch %d, want %d", epoch, baseEpoch+1))
	}
	if err := s.validateGroupLocked(true); err != nil {
		return rollback(fmt.Errorf("validating committed group: %w", err))
	}

	s.established = true
	s.clearPendingCommitLocked()
	return nil
}

// ProcessWelcome joins an initial group from opcode 30's raw Welcome.
func (s *GroupSession) ProcessWelcome(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.established {
		return fmt.Errorf("welcome replay received for an established group")
	}
	if len(data) == 0 || len(data) > maxDAVEControlMessageSize {
		return fmt.Errorf("invalid Welcome size %d", len(data))
	}

	oldState, _ := s.loadStateLocked()
	joinedGroupID, err := s.client.JoinGroup(context.Background(), data)
	if err != nil {
		return fmt.Errorf("joining MLS group: %w", err)
	}
	rollback := func(processErr error) error {
		if len(oldState) > 0 {
			_ = s.saveStateLocked(oldState)
		}
		return processErr
	}
	if !bytes.Equal(joinedGroupID, s.groupID) {
		return rollback(fmt.Errorf("welcome group ID does not match the voice channel"))
	}
	if err := s.validateGroupLocked(true); err != nil {
		return rollback(fmt.Errorf("validating Welcome group: %w", err))
	}

	epoch, err := s.client.Epoch(context.Background(), s.groupID)
	if err != nil {
		return rollback(fmt.Errorf("reading Welcome epoch: %w", err))
	}
	if epoch == 0 {
		return rollback(fmt.Errorf("welcome did not advance the MLS epoch"))
	}

	s.groupCreated = true
	s.established = true
	s.clearPendingCommitLocked()
	return nil
}

// Export runs MLS-Exporter for the current established epoch.
func (s *GroupSession) Export(label string, exportContext []byte, length int) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.established {
		return nil, fmt.Errorf("MLS group is not established")
	}
	return s.client.Export(context.Background(), s.groupID, label, exportContext, length)
}

// Epoch returns the current local MLS epoch.
func (s *GroupSession) Epoch() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.groupCreated {
		return 0, fmt.Errorf("MLS group is not initialized")
	}
	return s.client.Epoch(context.Background(), s.groupID)
}

// ReviewProposal restricts DAVE external proposals to recognized-user
// Add/Remove operations.
func (s *GroupSession) ReviewProposal(
	_ context.Context,
	_ mlscore.GroupSnapshot,
	proposal mlscore.ReviewableProposal,
) error {
	switch proposal.Type {
	case group.ProposalTypeAdd:
		if proposal.Add == nil {
			return fmt.Errorf("malformed Add proposal")
		}
		if _, ok := s.recognized[string(proposal.Add.Identity)]; !ok {
			return fmt.Errorf("add proposal contains an unrecognized voice user")
		}
		keyPackage, err := keypackages.UnmarshalKeyPackage(proposal.Add.KeyPackage)
		if err != nil {
			return fmt.Errorf("parsing Add key package: %w", err)
		}
		if err := validateDAVEKeyPackage(keyPackage); err != nil {
			return err
		}
		return nil

	case group.ProposalTypeRemove:
		if proposal.Remove == nil {
			return fmt.Errorf("malformed Remove proposal")
		}
		return nil

	default:
		return fmt.Errorf("proposal type %d is not permitted by DAVE v1", proposal.Type)
	}
}

func (s *GroupSession) validateGroupLocked(requireRecognized bool) error {
	g, err := s.loadGroupLocked()
	if err != nil {
		return err
	}
	if g.CipherSuite() != daveCipherSuite {
		return fmt.Errorf("unexpected cipher suite %d", g.CipherSuite())
	}

	groupContext := g.GroupContext()
	if groupContext == nil ||
		groupContext.Version != keypackages.MLS10 ||
		!bytes.Equal(groupContext.GroupID.AsSlice(), s.groupID) {
		return fmt.Errorf("invalid DAVE group context")
	}
	if len(groupContext.Extensions) != 1 ||
		groupContext.Extensions[0].Type != extensions.ExtensionTypeExternalSenders {
		return fmt.Errorf("DAVE group must contain exactly one external_senders extension")
	}

	expectedSender, err := extensions.ParseSingleExternalSender(s.externalSender)
	if err != nil {
		return fmt.Errorf("parsing expected external sender: %w", err)
	}
	expectedExtension := extensions.NewExternalSendersExtension()
	if err := expectedExtension.AddSender(*expectedSender); err != nil {
		return fmt.Errorf("building expected external sender extension: %w", err)
	}
	actualExtension, err := extensions.UnmarshalExternalSendersExtension(groupContext.Extensions[0].Data)
	if err != nil || !actualExtension.Equal(expectedExtension) {
		return fmt.Errorf("DAVE group external sender does not match opcode 25")
	}

	seen := make(map[string]struct{})
	for _, member := range g.GetMembers() {
		if member == nil || member.Credential == nil ||
			member.Credential.Type() != credentials.BasicCredential ||
			len(member.Credential.Identity) != 8 {
			return fmt.Errorf("DAVE group member has an invalid credential")
		}
		leaf := g.GetTreeLeaf(member.LeafIndex)
		if leaf == nil || leaf.LeafData == nil || len(leaf.LeafData.Extensions) != 0 {
			return fmt.Errorf("DAVE group member has invalid leaf extensions")
		}
		if member.KeyPackage != nil {
			if err := validateDAVEKeyPackage(member.KeyPackage); err != nil {
				return err
			}
		}

		identityKey := string(member.Credential.Identity)
		if _, duplicate := seen[identityKey]; duplicate {
			return fmt.Errorf("DAVE group contains duplicate user credentials")
		}
		seen[identityKey] = struct{}{}
		if requireRecognized {
			if _, ok := s.recognized[identityKey]; !ok {
				return fmt.Errorf("DAVE group contains an unrecognized voice user")
			}
		}
	}
	return nil
}

func validateDAVEKeyPackage(keyPackage *keypackages.KeyPackage) error {
	if keyPackage == nil ||
		keyPackage.ProtocolVersion != keypackages.MLS10 ||
		keyPackage.CipherSuite != daveCipherSuite ||
		keyPackage.LeafNode == nil ||
		keyPackage.LeafNode.Credential == nil ||
		keyPackage.LeafNode.Credential.Type() != credentials.BasicCredential ||
		len(keyPackage.LeafNode.Credential.Identity) != 8 {
		return fmt.Errorf("key package is incompatible with DAVE v1")
	}
	if len(keyPackage.Extensions) != 0 || len(keyPackage.LeafNode.Extensions) != 0 {
		return fmt.Errorf("DAVE v1 key packages must not contain extensions")
	}
	return nil
}

func validateDAVECommit(data, groupID []byte, epoch uint64) (*group.Commit, error) {
	msg, err := framing.UnmarshalMLSMessage(data)
	if err != nil {
		return nil, fmt.Errorf("parsing commit MLSMessage: %w", err)
	}
	if !bytes.Equal(msg.Marshal(), data) {
		return nil, fmt.Errorf("commit MLSMessage contains trailing or non-canonical data")
	}
	publicMessage, ok := msg.AsPublic()
	if !ok ||
		publicMessage.Content.ContentType() != framing.ContentTypeCommit ||
		publicMessage.Content.Sender.Type != framing.SenderTypeMember {
		return nil, fmt.Errorf("DAVE commit must be a public member commit")
	}
	if !bytes.Equal(publicMessage.Content.GroupID, groupID) {
		return nil, fmt.Errorf("commit group ID does not match")
	}
	if publicMessage.Content.Epoch != epoch {
		return nil, fmt.Errorf("commit epoch %d does not match current epoch %d", publicMessage.Content.Epoch, epoch)
	}

	commitData, ok := publicMessage.Content.CommitData()
	if !ok {
		return nil, fmt.Errorf("commit has no body")
	}
	commit, err := group.UnmarshalCommit(commitData)
	if err != nil {
		return nil, fmt.Errorf("parsing commit body: %w", err)
	}
	if len(commit.Proposals) == 0 {
		return nil, fmt.Errorf("DAVE commit does not reference any proposals")
	}
	for _, proposal := range commit.Proposals {
		if proposal.Proposal != nil || len(proposal.ProposalRef) == 0 {
			return nil, fmt.Errorf("DAVE commits must contain proposal references only")
		}
	}
	return commit, nil
}

func rawWelcomeBytes(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	msg, err := framing.UnmarshalMLSMessage(data)
	if err != nil {
		return nil, fmt.Errorf("parsing generated Welcome: %w", err)
	}
	if len(msg.Welcome) == 0 || !bytes.Equal(msg.Marshal(), data) {
		return nil, fmt.Errorf("generated Welcome is not canonical")
	}
	return append([]byte(nil), msg.Welcome...), nil
}

func (s *GroupSession) loadStateLocked() ([]byte, error) {
	if len(s.groupID) == 0 {
		return nil, fmt.Errorf("DAVE group ID is not configured")
	}
	state, err := s.store.LoadGroupState(context.Background(), group.NewGroupID(s.groupID))
	if err != nil {
		return nil, fmt.Errorf("loading MLS group state: %w", err)
	}
	return state, nil
}

func (s *GroupSession) saveStateLocked(state []byte) error {
	if len(state) == 0 {
		return fmt.Errorf("cannot restore an empty MLS group state")
	}
	if err := s.store.SaveGroupState(
		context.Background(),
		group.NewGroupID(s.groupID),
		state,
	); err != nil {
		return fmt.Errorf("restoring MLS group state: %w", err)
	}
	return nil
}

func (s *GroupSession) loadGroupLocked() (*group.Group, error) {
	state, err := s.loadStateLocked()
	if err != nil {
		return nil, err
	}
	g, err := group.UnmarshalGroupState(state)
	if err != nil {
		return nil, fmt.Errorf("unmarshaling MLS group state: %w", err)
	}
	return g, nil
}

func (s *GroupSession) clearPendingCommitLocked() {
	s.pendingBaseState = nil
	s.pendingCandidateState = nil
	s.pendingCommit = nil
}

func userIDIdentity(userID string) ([]byte, error) {
	id, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing user ID: %w", err)
	}
	identity := make([]byte, 8)
	binary.BigEndian.PutUint64(identity, id)
	return identity, nil
}

func readDAVEVector(data []byte) (value, rest []byte, err error) {
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("missing vector length")
	}

	width := 1 << (data[0] >> 6)
	if width > len(data) {
		return nil, nil, fmt.Errorf("truncated vector length")
	}

	var length uint64
	switch width {
	case 1:
		length = uint64(data[0] & 0x3f)
	case 2:
		length = uint64(binary.BigEndian.Uint16(data[:2]) & 0x3fff)
		if length < 64 {
			return nil, nil, fmt.Errorf("non-canonical vector length")
		}
	case 4:
		length = uint64(binary.BigEndian.Uint32(data[:4]) & 0x3fffffff)
		if length < 16384 {
			return nil, nil, fmt.Errorf("non-canonical vector length")
		}
	case 8:
		length = binary.BigEndian.Uint64(data[:8]) & 0x3fffffffffffffff
		if length < 1073741824 {
			return nil, nil, fmt.Errorf("non-canonical vector length")
		}
	}
	if length > maxDAVEControlMessageSize || length > uint64(len(data)-width) {
		return nil, nil, fmt.Errorf("invalid vector length %d", length)
	}

	end := width + int(length)
	return data[width:end], data[end:], nil
}
