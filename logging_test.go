package dgo

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSessionLog_NoDeadlockUnderWriteLock(t *testing.T) {
	s := &Session{}
	var logs bytes.Buffer
	s.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))

	done := make(chan struct{})
	go func() {
		s.Lock()
		defer s.Unlock()
		s.log(LogInformational, "locked log call")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Session.log deadlocked while session write lock was held")
	}
	if !strings.Contains(logs.String(), "locked log call") {
		t.Fatalf("log entry was dropped while session lock was held: %q", logs.String())
	}
}

func TestSessionLoggerCanBeReplacedConcurrently(t *testing.T) {
	s := &Session{}
	s.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			s.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
		}
	}()
	for i := 0; i < 100; i++ {
		s.log(LogInformational, "message %d", i)
	}
	<-done
}

func TestSanitizeURLRedactsPathAndQueryCredentials(t *testing.T) {
	tests := []string{
		"https://discord.com/api/v10/webhooks/123/webhook-secret/messages/1?wait=true",
		"https://discord.com/api/v10/interactions/123/interaction-secret/callback",
		"https://example.com/callback?access_token=query-secret",
	}
	for _, rawURL := range tests {
		sanitized := sanitizeURL(rawURL)
		for _, secret := range []string{"webhook-secret", "interaction-secret", "query-secret"} {
			if strings.Contains(sanitized, secret) {
				t.Fatalf("sanitizeURL(%q) leaked %q in %q", rawURL, secret, sanitized)
			}
		}
		if !strings.Contains(sanitized, "REDACTED") {
			t.Fatalf("sanitizeURL(%q) = %q, want redaction marker", rawURL, sanitized)
		}
	}
}

func TestRedactJSONRedactsNestedCredentials(t *testing.T) {
	payload := []byte(`{"token":"bot-secret","nested":{"secret_key":[1,2,3],"session_id":"voice-session"},"url":"https://discord.com/api/v10/webhooks/1/webhook-secret"}`)
	redacted := redactJSON(payload)

	for _, secret := range []string{"bot-secret", "voice-session", "webhook-secret", "[1,2,3]"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redactJSON leaked %q in %s", secret, redacted)
		}
	}
}

func TestGatewayDebugLogRedactsInteractionToken(t *testing.T) {
	s, err := New("Bot bot-secret")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	_, err = s.onEvent(websocket.TextMessage, []byte(`{"op":0,"s":1,"t":"INTERACTION_CREATE","d":{"id":"1","token":"interaction-secret","type":1}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := logs.String(); strings.Contains(got, "interaction-secret") || !strings.Contains(got, "REDACTED") {
		t.Fatalf("gateway debug log was not redacted: %s", got)
	}
}

func TestVoiceDebugLogRedactsSecretKey(t *testing.T) {
	previousLogger := Logger
	defer func() { Logger = previousLogger }()

	var logs bytes.Buffer
	Logger = func(_ int, _ int, format string, args ...interface{}) {
		fmt.Fprintf(&logs, format, args...)
	}
	v := &VoiceConnection{LogLevel: LogDebug}
	v.onEvent(false, []byte(`{"op":4,"d":{"secret_key":"voice-secret","mode":"invalid"}}`))

	if got := logs.String(); strings.Contains(got, "voice-secret") || !strings.Contains(got, "REDACTED") {
		t.Fatalf("voice debug log was not redacted: %s", got)
	}
}
