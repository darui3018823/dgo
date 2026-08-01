package dgo

import (
	"crypto/aes"
	"math"
	"testing"
)

func TestDAVEReceiverRejectsReplayedNonce(t *testing.T) {
	key := []byte("0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	frameCipher, err := newDAVECipher(key)
	if err != nil {
		t.Fatalf("newDAVECipher: %v", err)
	}

	const ssrc = 42
	dave := NewDAVESession("1")
	dave.active = true
	dave.receivers = map[uint32]*daveReceiver{
		ssrc: {
			userID:      "1",
			baseSecret:  []byte("unused test base secret"),
			key:         key,
			aesBlock:    block,
			frameCipher: frameCipher,
		},
	}

	frameOne := encryptSecureFrame(frameCipher, 1, []byte("first"))
	if plaintext, err := dave.DecryptFrame(ssrc, frameOne); err != nil {
		t.Fatalf("first DecryptFrame: %v", err)
	} else if string(plaintext) != "first" {
		t.Fatalf("first plaintext = %q", plaintext)
	}
	if _, err := dave.DecryptFrame(ssrc, frameOne); err == nil {
		t.Fatal("replayed nonce was accepted")
	}

	frameThree := encryptSecureFrame(frameCipher, 3, []byte("third"))
	frameTwo := encryptSecureFrame(frameCipher, 2, []byte("second"))
	if _, err := dave.DecryptFrame(ssrc, frameThree); err != nil {
		t.Fatalf("newer nonce: %v", err)
	}
	if plaintext, err := dave.DecryptFrame(ssrc, frameTwo); err != nil {
		t.Fatalf("out-of-order nonce inside replay window: %v", err)
	} else if string(plaintext) != "second" {
		t.Fatalf("out-of-order plaintext = %q", plaintext)
	}
	if _, err := dave.DecryptFrame(ssrc, frameTwo); err == nil {
		t.Fatal("replayed out-of-order nonce was accepted")
	}
}

func TestDAVEReceiverRejectsGenerationJump(t *testing.T) {
	key := []byte("0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	frameCipher, err := newDAVECipher(key)
	if err != nil {
		t.Fatalf("newDAVECipher: %v", err)
	}

	const ssrc = 43
	dave := NewDAVESession("1")
	dave.active = true
	dave.receivers = map[uint32]*daveReceiver{
		ssrc: {
			userID:      "1",
			baseSecret:  []byte("unused test base secret"),
			key:         key,
			aesBlock:    block,
			frameCipher: frameCipher,
		},
	}
	jumpedNonce := uint32(2 << 24)
	frame := encryptSecureFrame(frameCipher, jumpedNonce, []byte("future"))
	if _, err := dave.DecryptFrame(ssrc, frame); err == nil {
		t.Fatal("generation jump was accepted")
	}
}

func TestDAVESenderNonceExhaustionFailsClosed(t *testing.T) {
	key := []byte("0123456789abcdef")
	frameCipher, err := newDAVECipher(key)
	if err != nil {
		t.Fatalf("newDAVECipher: %v", err)
	}

	dave := NewDAVESession("1")
	dave.active = true
	dave.frameCipher = frameCipher
	dave.senderNonce = math.MaxUint32

	if _, err := dave.EncryptFrame([]byte("audio")); err == nil {
		t.Fatal("EncryptFrame accepted an exhausted nonce")
	}
	if dave.IsActive() {
		t.Fatal("nonce exhaustion left DAVE active")
	}
}

func TestDAVETransitionOrderFailsClosed(t *testing.T) {
	dave := NewDAVESession("1")
	dave.hasPendingKey = true
	if err := dave.HandlePrepareTransition(7, 1); err != nil {
		t.Fatalf("HandlePrepareTransition: %v", err)
	}
	if err := dave.HandleExecuteTransition(8); err == nil {
		t.Fatal("out-of-order transition was accepted")
	}
	if dave.IsActive() {
		t.Fatal("out-of-order transition activated DAVE")
	}

	// A downgrade is executable without MLS state and must clear all media
	// keys. It also cannot be replayed.
	dave = NewDAVESession("1")
	dave.active = true
	dave.senderKey = []byte("sender key")
	if err := dave.HandlePrepareTransition(9, 0); err != nil {
		t.Fatalf("prepare downgrade: %v", err)
	}
	if err := dave.HandleExecuteTransition(9); err != nil {
		t.Fatalf("execute downgrade: %v", err)
	}
	if dave.IsActive() || dave.senderKey != nil {
		t.Fatal("downgrade retained active media state")
	}
	if err := dave.HandleExecuteTransition(9); err == nil {
		t.Fatal("transition replay was accepted")
	}
	if err := dave.HandlePrepareTransition(10, 1); err == nil {
		t.Fatal("DAVE v1 transition without a pending MLS epoch was accepted")
	}
}
