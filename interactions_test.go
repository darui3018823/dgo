package dgo

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInteractionCurrentContextFields(t *testing.T) {
	payload := []byte(`{
		"id":"interaction",
		"application_id":"app",
		"type":2,
		"data":{"id":"command","name":"ping","type":1},
		"guild":{"id":"guild","name":"support","locale":"ja"},
		"guild_id":"guild",
		"channel":{
			"id":"channel",
			"guild_id":"guild",
			"name":"commands",
			"type":0,
			"permissions":"3072",
			"app_permissions":"1024"
		},
		"channel_id":"channel",
		"token":"secret",
		"version":1,
		"attachment_size_limit":26214400,
		"entitlements":[],
		"authorizing_integration_owners":{"0":"guild"}
	}`)

	var interaction Interaction
	if err := json.Unmarshal(payload, &interaction); err != nil {
		t.Fatal(err)
	}
	if interaction.Guild == nil || interaction.Guild.ID != "guild" {
		t.Fatalf("unexpected partial guild: %#v", interaction.Guild)
	}
	if interaction.Channel == nil ||
		interaction.Channel.ID != "channel" ||
		interaction.Channel.Permissions != 3072 ||
		interaction.Channel.AppPermissions != 1024 {
		t.Fatalf("unexpected partial channel: %#v", interaction.Channel)
	}
	if interaction.AttachmentSizeLimit != 26214400 {
		t.Fatalf("AttachmentSizeLimit = %d", interaction.AttachmentSizeLimit)
	}
	if interaction.AuthorizingIntegrationOwners[ApplicationIntegrationGuildInstall] != "guild" {
		t.Fatalf("unexpected authorizing owners: %#v", interaction.AuthorizingIntegrationOwners)
	}
}

func TestPingInteractionAllowsMissingData(t *testing.T) {
	var interaction Interaction
	if err := json.Unmarshal([]byte(`{
		"id":"ping",
		"application_id":"app",
		"type":1,
		"token":"secret",
		"version":1,
		"attachment_size_limit":10485760
	}`), &interaction); err != nil {
		t.Fatal(err)
	}
	if interaction.Type != InteractionPing || interaction.Data != nil {
		t.Fatalf("unexpected ping interaction: %#v", interaction)
	}
}

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
