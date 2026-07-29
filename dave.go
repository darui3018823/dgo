package dgo

// DAVE support adapted from bwmarrin/discordgo PRs #1701 and #1704.

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/darui3018823/dgo/mls"
)

var errUnencryptedDAVEFrame = errors.New("received an unencrypted frame while DAVE is active")

type daveReceiver struct {
	userID            string
	baseSecret        []byte
	currentGeneration uint32
	key               []byte
	aesBlock          cipher.Block
	frameCipher       cipher.AEAD
	highestNonce      uint32
	replayWindow      uint64
	hasNonce          bool
}

type DAVESession struct {
	mu                  sync.Mutex
	epoch               uint64
	pendingTransitionID uint16
	pendingVersion      int
	pendingTransition   bool
	lastTransitionID    uint16
	hasLastTransition   bool

	senderKey         []byte
	senderNonce       uint32
	frameCipher       cipher.AEAD
	userID            string
	active            bool
	ratchetBaseSecret []byte
	currentGeneration uint32
	hasPendingKey     bool

	ssrcToUserID map[uint32]string
	receivers    map[uint32]*daveReceiver

	groupID         []byte
	protocolVersion int
	group           *mls.GroupSession
	groupErr        error
}

func NewDAVESession(userID string) *DAVESession {
	d := &DAVESession{userID: userID}
	identity, err := daveUserIDIdentity(userID)
	if err != nil {
		d.groupErr = err
		return d
	}
	d.group, d.groupErr = mls.NewGroupSession(identity)
	return d
}

func (d *DAVESession) Configure(groupID string, protocolVersion int, recognizedUsers []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.groupErr != nil {
		return d.groupErr
	}
	groupIDNum, err := strconv.ParseUint(groupID, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing voice channel ID for MLS group: %w", err)
	}
	encodedGroupID := make([]byte, 8)
	binary.BigEndian.PutUint64(encodedGroupID, groupIDNum)

	d.groupID = encodedGroupID
	d.protocolVersion = protocolVersion
	d.group.SetRecognizedUsers(recognizedUsers)
	if err := d.group.Reset(encodedGroupID, protocolVersion); err != nil {
		return err
	}
	d.clearMediaStateLocked()
	d.clearPendingTransitionLocked()
	return nil
}

func (d *DAVESession) SetRecognizedUsers(userIDs []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.group != nil {
		d.group.SetRecognizedUsers(userIDs)
	}
}

func (d *DAVESession) GenerateKeyPackage() ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.generateKeyPackageLocked()
}

func (d *DAVESession) ResetForReWelcome() ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.groupErr != nil {
		return nil, d.groupErr
	}
	if len(d.groupID) == 0 {
		return nil, fmt.Errorf("DAVE group ID is not configured")
	}
	if err := d.group.Reset(d.groupID, d.protocolVersion); err != nil {
		return nil, fmt.Errorf("resetting MLS group: %w", err)
	}
	d.clearMediaStateLocked()
	d.clearPendingTransitionLocked()
	return d.generateKeyPackageLocked()
}

func (d *DAVESession) generateKeyPackageLocked() ([]byte, error) {
	if d.groupErr != nil {
		return nil, d.groupErr
	}
	if d.group == nil {
		return nil, fmt.Errorf("MLS group session is unavailable")
	}
	keyPackage, err := d.group.GenerateKeyPackage()
	if err != nil {
		return nil, fmt.Errorf("generating key package: %w", err)
	}
	return keyPackage, nil
}

func (d *DAVESession) HandleExternalSenderPackage(data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.groupErr != nil {
		return d.groupErr
	}
	return d.group.SetExternalSender(data)
}

func (d *DAVESession) HandleProposals(data []byte) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.groupErr != nil {
		return nil, d.groupErr
	}
	return d.group.ProcessProposals(data)
}

func (d *DAVESession) HandleWelcome(data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.groupErr != nil {
		return d.groupErr
	}
	if err := d.group.ProcessWelcome(data); err != nil {
		return fmt.Errorf("processing welcome: %w", err)
	}
	epoch, err := d.group.Epoch()
	if err != nil {
		return err
	}
	d.epoch = epoch
	d.hasPendingKey = true
	d.receivers = nil
	return nil
}

func (d *DAVESession) HandleCommit(data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.groupErr != nil {
		return d.groupErr
	}
	if err := d.group.ProcessCommit(data); err != nil {
		return fmt.Errorf("processing commit: %w", err)
	}
	epoch, err := d.group.Epoch()
	if err != nil {
		return err
	}
	d.epoch = epoch
	d.hasPendingKey = true
	d.receivers = nil
	return nil
}

func (d *DAVESession) HandlePrepareTransition(transitionID uint16, protocolVersion int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if protocolVersion != 0 && protocolVersion != 1 {
		return fmt.Errorf("unsupported DAVE protocol version %d", protocolVersion)
	}
	if d.hasLastTransition && transitionID == d.lastTransitionID {
		return fmt.Errorf("transition %d was already executed", transitionID)
	}
	if d.pendingTransition && transitionID != d.pendingTransitionID {
		return fmt.Errorf("transition %d is already pending", d.pendingTransitionID)
	}
	if protocolVersion > 0 && !d.hasPendingKey {
		return fmt.Errorf("transition %d has no pending MLS epoch", transitionID)
	}

	d.pendingTransitionID = transitionID
	d.pendingVersion = protocolVersion
	d.pendingTransition = true
	return nil
}

func (d *DAVESession) HandleExecuteTransition(transitionID uint16) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.pendingTransition || transitionID != d.pendingTransitionID {
		return fmt.Errorf("unexpected DAVE transition %d", transitionID)
	}

	if d.pendingVersion > 0 {
		if !d.hasPendingKey {
			return fmt.Errorf("transition %d has no pending MLS key", transitionID)
		}
		if err := d.deriveSenderKeyLocked(); err != nil {
			return err
		}
		d.hasPendingKey = false
		d.active = true
	} else {
		d.clearMediaStateLocked()
	}

	d.protocolVersion = d.pendingVersion
	d.pendingTransition = false
	d.hasLastTransition = true
	d.lastTransitionID = transitionID
	return nil
}

func (d *DAVESession) HandlePrepareEpoch(epoch uint64, protocolVersion int) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if epoch == 0 {
		return nil, fmt.Errorf("DAVE epoch must be positive")
	}
	if protocolVersion != 0 && protocolVersion != 1 {
		return nil, fmt.Errorf("unsupported DAVE protocol version %d", protocolVersion)
	}

	d.epoch = epoch
	d.protocolVersion = protocolVersion
	if epoch != 1 {
		return nil, nil
	}
	if protocolVersion == 0 {
		d.clearMediaStateLocked()
		d.clearPendingTransitionLocked()
		return nil, nil
	}
	if len(d.groupID) == 0 {
		return nil, fmt.Errorf("DAVE group ID is not configured")
	}

	if err := d.group.Reset(d.groupID, protocolVersion); err != nil {
		return nil, fmt.Errorf("resetting MLS group for epoch 1: %w", err)
	}
	d.clearMediaStateLocked()
	d.clearPendingTransitionLocked()
	return d.generateKeyPackageLocked()
}

func (d *DAVESession) DeriveSenderKey() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deriveSenderKeyLocked()
}

func (d *DAVESession) deriveSenderKeyLocked() error {
	if d.group == nil {
		return fmt.Errorf("no MLS group")
	}

	userIDNum, err := strconv.ParseUint(d.userID, 10, 64)
	if err != nil {
		return fmt.Errorf("parsing user ID: %w", err)
	}
	context := make([]byte, 8)
	binary.LittleEndian.PutUint64(context, userIDNum)

	baseSecret, err := d.group.Export(daveExportLabel, context, daveKeySize)
	if err != nil {
		return fmt.Errorf("exporting base secret: %w", err)
	}

	d.ratchetBaseSecret = baseSecret
	d.currentGeneration = 0
	d.senderNonce = 0

	key, err := hashRatchetGetKey(baseSecret, 0)
	if err != nil {
		return fmt.Errorf("deriving ratchet key: %w", err)
	}
	d.senderKey = key

	frameCipher, err := newDAVECipher(key)
	if err != nil {
		return fmt.Errorf("creating frame cipher: %w", err)
	}
	d.frameCipher = frameCipher
	return nil
}

func (d *DAVESession) EncryptFrame(opusData []byte) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.active {
		return nil, fmt.Errorf("DAVE session is not active")
	}
	if d.frameCipher == nil {
		return nil, fmt.Errorf("no frame cipher")
	}
	if d.senderNonce == math.MaxUint32 {
		d.clearMediaStateLocked()
		return nil, fmt.Errorf("DAVE sender nonce exhausted")
	}

	d.senderNonce++

	generation := d.senderNonce >> 24
	if generation != d.currentGeneration {
		d.currentGeneration = generation
		key, err := hashRatchetGetKey(d.ratchetBaseSecret, generation)
		if err != nil {
			return nil, fmt.Errorf("ratcheting key for generation %d: %w", generation, err)
		}
		d.senderKey = key
		frameCipher, err := newDAVECipher(key)
		if err != nil {
			return nil, fmt.Errorf("creating cipher for generation %d: %w", generation, err)
		}
		d.frameCipher = frameCipher
	}

	encrypted := encryptSecureFrame(d.frameCipher, d.senderNonce, opusData)
	return encrypted, nil
}

func (d *DAVESession) SetSSRC(ssrc uint32, userID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ssrcToUserID == nil {
		d.ssrcToUserID = make(map[uint32]string)
	}
	d.ssrcToUserID[ssrc] = userID
}

func (d *DAVESession) DecryptFrame(ssrc uint32, data []byte) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.active {
		return data, nil
	}

	ciphertext, truncatedTag, nonce, err := parseSecureFrame(data)
	if err == errNotDAVEFrame {
		return nil, errUnencryptedDAVEFrame
	}
	if err != nil {
		return nil, err
	}

	recv := d.receivers[ssrc]
	if recv == nil {
		userID, ok := d.ssrcToUserID[ssrc]
		if !ok {
			return nil, fmt.Errorf("unknown SSRC %d", ssrc)
		}
		recv, err = d.createReceiverLocked(ssrc, userID)
		if err != nil {
			return nil, err
		}
	}

	generation := nonce >> 24
	if recv.hasNonce {
		if nonce <= recv.highestNonce {
			delta := recv.highestNonce - nonce
			if delta >= 64 || recv.replayWindow&(uint64(1)<<delta) != 0 {
				return nil, fmt.Errorf("DAVE frame nonce %d was replayed or is too old", nonce)
			}
		}
	}
	if generation != recv.currentGeneration {
		if generation < recv.currentGeneration || generation > recv.currentGeneration+1 {
			return nil, fmt.Errorf(
				"DAVE receiver generation changed from %d to invalid generation %d",
				recv.currentGeneration,
				generation,
			)
		}
		key, err := hashRatchetGetKey(recv.baseSecret, generation)
		if err != nil {
			return nil, fmt.Errorf("ratcheting receiver key for generation %d: %w", generation, err)
		}
		recv.key = key
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		recv.aesBlock = block
		fc, err := newDAVECipher(key)
		if err != nil {
			return nil, err
		}
		recv.frameCipher = fc
		recv.currentGeneration = generation
	}

	plaintext, err := decryptSecureFrame(recv.aesBlock, recv.frameCipher, nonce, ciphertext, truncatedTag)
	if err != nil {
		return nil, err
	}
	recv.acceptNonce(nonce)
	return plaintext, nil
}

func (d *DAVESession) createReceiverLocked(ssrc uint32, userID string) (*daveReceiver, error) {
	if d.group == nil {
		return nil, fmt.Errorf("no MLS group")
	}

	userIDNum, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing user ID: %w", err)
	}
	context := make([]byte, 8)
	binary.LittleEndian.PutUint64(context, userIDNum)

	baseSecret, err := d.group.Export(daveExportLabel, context, daveKeySize)
	if err != nil {
		return nil, fmt.Errorf("exporting receiver base secret: %w", err)
	}

	key, err := hashRatchetGetKey(baseSecret, 0)
	if err != nil {
		return nil, fmt.Errorf("deriving receiver ratchet key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	fc, err := newDAVECipher(key)
	if err != nil {
		return nil, err
	}

	recv := &daveReceiver{
		userID:      userID,
		baseSecret:  baseSecret,
		key:         key,
		aesBlock:    block,
		frameCipher: fc,
	}

	if d.receivers == nil {
		d.receivers = make(map[uint32]*daveReceiver)
	}
	d.receivers[ssrc] = recv
	return recv, nil
}

func (d *DAVESession) clearReceiversLocked() {
	d.receivers = nil
}

func (d *DAVESession) clearMediaStateLocked() {
	d.senderKey = nil
	d.senderNonce = 0
	d.frameCipher = nil
	d.active = false
	d.ratchetBaseSecret = nil
	d.currentGeneration = 0
	d.hasPendingKey = false
	d.clearReceiversLocked()
}

func (d *DAVESession) clearPendingTransitionLocked() {
	d.pendingTransitionID = 0
	d.pendingVersion = 0
	d.pendingTransition = false
}

func (r *daveReceiver) acceptNonce(nonce uint32) {
	if !r.hasNonce {
		r.highestNonce = nonce
		r.replayWindow = 1
		r.hasNonce = true
		return
	}
	if nonce > r.highestNonce {
		delta := nonce - r.highestNonce
		if delta >= 64 {
			r.replayWindow = 1
		} else {
			r.replayWindow = r.replayWindow<<delta | 1
		}
		r.highestNonce = nonce
		return
	}
	r.replayWindow |= uint64(1) << (r.highestNonce - nonce)
}

func (d *DAVESession) CanEncrypt() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active && d.frameCipher != nil
}

func (d *DAVESession) IsActive() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.active
}

func (d *DAVESession) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.clearMediaStateLocked()
	d.clearPendingTransitionLocked()
	d.lastTransitionID = 0
	d.hasLastTransition = false
	d.ssrcToUserID = nil
}

func daveUserIDIdentity(userID string) ([]byte, error) {
	userIDNum, err := strconv.ParseUint(userID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parsing user ID for credential: %w", err)
	}
	identity := make([]byte, 8)
	binary.BigEndian.PutUint64(identity, userIDNum)
	return identity, nil
}
