package dgo

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseWebhookEvent(t *testing.T) {
	event, err := ParseWebhookEvent([]byte(`{
		"version":1,
		"application_id":"123",
		"type":1,
		"event":{
			"type":"ENTITLEMENT_CREATE",
			"timestamp":"2024-10-18T18:41:21.109604",
			"data":{"id":"456","consumed":false}
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Version != 1 || event.ApplicationID != "123" || event.Type != WebhookEventTypeEvent || event.Event == nil {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event.Event.Type != WebhookEventEntitlementCreate || event.Event.Timestamp == "" {
		t.Fatalf("unexpected event body: %#v", event.Event)
	}
	if got := string(event.Event.Data); got != `{"id":"456","consumed":false}` {
		t.Fatalf("event data = %q", got)
	}

	ping, err := ParseWebhookEvent([]byte(`{"version":1,"application_id":"123","type":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if ping.Type != WebhookEventTypePing || ping.Event != nil {
		t.Fatalf("unexpected ping: %#v", ping)
	}
}

func TestParseWebhookEventRequestRestoresBody(t *testing.T) {
	body := `{"version":1,"application_id":"123","type":0}`
	request := httptest.NewRequest(http.MethodPost, "http://localhost/webhook", strings.NewReader(body))

	if _, err := ParseWebhookEventRequest(request); err != nil {
		t.Fatal(err)
	}
	restored, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != body {
		t.Fatalf("restored body = %q, want %q", restored, body)
	}
}

func TestVerifyWebhookEvent(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	timestamp := "1608597133"
	body := `{"version":1,"application_id":"123","type":0}`
	request := httptest.NewRequest(http.MethodPost, "http://localhost/webhook", strings.NewReader(body))
	request.Header.Set("X-Signature-Timestamp", timestamp)
	message := append([]byte(timestamp), []byte(body)...)
	request.Header.Set("X-Signature-Ed25519", hex.EncodeToString(ed25519.Sign(privateKey, message)))

	if !VerifyWebhookEvent(request, publicKey) {
		t.Fatal("valid webhook signature was rejected")
	}
	restored, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored, []byte(body)) {
		t.Fatalf("restored body = %q, want %q", restored, body)
	}
}

func TestAcknowledgeWebhookEvent(t *testing.T) {
	recorder := httptest.NewRecorder()
	AcknowledgeWebhookEvent(recorder)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("response body is not empty: %q", recorder.Body.String())
	}
}
