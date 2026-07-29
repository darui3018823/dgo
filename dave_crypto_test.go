package dgo

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestULEB128RoundTrip(t *testing.T) {
	values := []uint32{0, 1, 127, 128, 16384, math.MaxUint32}
	for _, value := range values {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			encoded := encodeULEB128(value)
			decoded, consumed, err := decodeULEB128(encoded)
			if err != nil {
				t.Fatalf("decodeULEB128(%x): %v", encoded, err)
			}
			if decoded != value {
				t.Fatalf("decoded = %d, want %d", decoded, value)
			}
			if consumed != len(encoded) {
				t.Fatalf("consumed = %d, want %d", consumed, len(encoded))
			}
		})
	}
}

func TestDecodeULEB128RejectsMalformedValues(t *testing.T) {
	tests := map[string][]byte{
		"empty":           {},
		"unterminated":    {0x80},
		"too long":        {0x81, 0x81, 0x81, 0x81, 0x81, 0x00},
		"uint32 overflow": {0xff, 0xff, 0xff, 0xff, 0x10},
		"non canonical":   {0x80, 0x00},
	}

	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := decodeULEB128(data); err == nil {
				t.Fatalf("decodeULEB128(%x) succeeded", data)
			}
		})
	}
}

func TestParseSecureFrameRejectsTrailingNonceBytes(t *testing.T) {
	frame := make([]byte, daveTagSize)
	frame = append(frame, 0x01, 0x00)
	frame = append(frame, byte(daveTagSize+2+1+2), 0xFA, 0xFA)

	if _, _, _, err := parseSecureFrame(frame); err == nil {
		t.Fatal("parseSecureFrame accepted trailing nonce bytes")
	}
}

func TestDAVERejectsPlaintextOnlyWhileActive(t *testing.T) {
	dave := NewDAVESession("1")
	plaintext := []byte("unencrypted audio")

	got, err := dave.DecryptFrame(1, plaintext)
	if err != nil {
		t.Fatalf("inactive DecryptFrame returned error: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("inactive DecryptFrame = %x, want %x", got, plaintext)
	}

	dave.active = true
	if _, err = dave.DecryptFrame(1, plaintext); !errors.Is(err, errUnencryptedDAVEFrame) {
		t.Fatalf("active DecryptFrame error = %v, want %v", err, errUnencryptedDAVEFrame)
	}
}

func TestDAVEEncryptionFailsClosed(t *testing.T) {
	dave := NewDAVESession("1")
	dave.active = true

	encrypted, err := dave.EncryptFrame([]byte("sensitive audio"))
	if err == nil {
		t.Fatal("EncryptFrame succeeded without an active frame cipher")
	}
	if encrypted != nil {
		t.Fatalf("EncryptFrame returned plaintext fallback: %x", encrypted)
	}
}
