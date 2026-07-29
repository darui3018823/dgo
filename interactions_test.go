package dgo

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestVerifyInteraction(t *testing.T) {
	pubkey, privkey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Errorf("error generating signing keypair: %s", err)
	}
	timestamp := "1608597133"

	t.Run("success", func(t *testing.T) {
		body := "body"
		request := httptest.NewRequest("POST", "http://localhost/interaction", strings.NewReader(body))
		request.Header.Set("X-Signature-Timestamp", timestamp)

		var msg bytes.Buffer
		msg.WriteString(timestamp)
		msg.WriteString(body)
		signature := ed25519.Sign(privkey, msg.Bytes())
		request.Header.Set("X-Signature-Ed25519", hex.EncodeToString(signature[:ed25519.SignatureSize]))

		if !VerifyInteraction(request, pubkey) {
			t.Error("expected true, got false")
		}
		restored, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(restored) != body {
			t.Fatalf("restored body = %q, want %q", restored, body)
		}
	})

	t.Run("failure/modified body", func(t *testing.T) {
		body := "body"
		request := httptest.NewRequest("POST", "http://localhost/interaction", strings.NewReader("WRONG"))
		request.Header.Set("X-Signature-Timestamp", timestamp)

		var msg bytes.Buffer
		msg.WriteString(timestamp)
		msg.WriteString(body)
		signature := ed25519.Sign(privkey, msg.Bytes())
		request.Header.Set("X-Signature-Ed25519", hex.EncodeToString(signature[:ed25519.SignatureSize]))

		if VerifyInteraction(request, pubkey) {
			t.Error("expected false, got true")
		}
	})

	t.Run("failure/modified timestamp", func(t *testing.T) {
		body := "body"
		request := httptest.NewRequest("POST", "http://localhost/interaction", strings.NewReader("WRONG"))
		request.Header.Set("X-Signature-Timestamp", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))

		var msg bytes.Buffer
		msg.WriteString(timestamp)
		msg.WriteString(body)
		signature := ed25519.Sign(privkey, msg.Bytes())
		request.Header.Set("X-Signature-Ed25519", hex.EncodeToString(signature[:ed25519.SignatureSize]))

		if VerifyInteraction(request, pubkey) {
			t.Error("expected false, got true")
		}
	})

	t.Run("failure/body over limit", func(t *testing.T) {
		body := strings.Repeat("x", int(MaxInteractionBodySize)+1)
		request := httptest.NewRequest("POST", "http://localhost/interaction", strings.NewReader(body))
		request.Header.Set("X-Signature-Timestamp", timestamp)
		request.Header.Set("X-Signature-Ed25519", strings.Repeat("00", ed25519.SignatureSize))

		if VerifyInteraction(request, pubkey) {
			t.Error("oversized interaction body was accepted")
		}
	})

	t.Run("failure/chunked body over limit", func(t *testing.T) {
		body := strings.Repeat("x", int(MaxInteractionBodySize)+1)
		request := httptest.NewRequest("POST", "http://localhost/interaction", strings.NewReader(body))
		request.ContentLength = -1
		request.Header.Set("X-Signature-Timestamp", timestamp)
		request.Header.Set("X-Signature-Ed25519", strings.Repeat("00", ed25519.SignatureSize))

		if VerifyInteraction(request, pubkey) {
			t.Error("oversized chunked interaction body was accepted")
		}
	})
}
