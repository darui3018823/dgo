// dgo - Discord bindings for Go
// Available at https://github.com/darui3018823/dgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dgo

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// WebhookEventName identifies an event in a WebhookEventBody.
type WebhookEventName string

const (
	WebhookEventApplicationAuthorized   WebhookEventName = "APPLICATION_AUTHORIZED"
	WebhookEventApplicationDeauthorized WebhookEventName = "APPLICATION_DEAUTHORIZED"
	WebhookEventEntitlementCreate       WebhookEventName = "ENTITLEMENT_CREATE"
	WebhookEventEntitlementUpdate       WebhookEventName = "ENTITLEMENT_UPDATE"
	WebhookEventEntitlementDelete       WebhookEventName = "ENTITLEMENT_DELETE"
	WebhookEventQuestUserEnrollment     WebhookEventName = "QUEST_USER_ENROLLMENT"
	WebhookEventLobbyMessageCreate      WebhookEventName = "LOBBY_MESSAGE_CREATE"
	WebhookEventLobbyMessageUpdate      WebhookEventName = "LOBBY_MESSAGE_UPDATE"
	WebhookEventLobbyMessageDelete      WebhookEventName = "LOBBY_MESSAGE_DELETE"
	WebhookEventGameDirectMessageCreate WebhookEventName = "GAME_DIRECT_MESSAGE_CREATE"
	WebhookEventGameDirectMessageUpdate WebhookEventName = "GAME_DIRECT_MESSAGE_UPDATE"
	WebhookEventGameDirectMessageDelete WebhookEventName = "GAME_DIRECT_MESSAGE_DELETE"
)

// WebhookEventType is the type of the outer Discord webhook event payload.
type WebhookEventType int

const (
	// WebhookEventTypePing is sent when Discord validates a webhook URL.
	WebhookEventTypePing WebhookEventType = 0
	// WebhookEventTypeEvent contains a webhook event body.
	WebhookEventTypeEvent WebhookEventType = 1
)

// WebhookEvent is the outer payload sent by Discord to an HTTP Webhook Events
// endpoint. Event is nil for PING payloads.
type WebhookEvent struct {
	Version       int               `json:"version"`
	ApplicationID string            `json:"application_id"`
	Type          WebhookEventType  `json:"type"`
	Event         *WebhookEventBody `json:"event,omitempty"`
}

// WebhookEventBody contains the event name, occurrence timestamp, and
// event-specific JSON data. Data is intentionally left raw because its shape
// depends on Type.
type WebhookEventBody struct {
	Type      WebhookEventName `json:"type"`
	Timestamp string           `json:"timestamp"`
	Data      json.RawMessage  `json:"data,omitempty"`
}

// VerifyWebhookEvent verifies Discord's X-Signature-Ed25519 and
// X-Signature-Timestamp headers for a webhook event request. The request body
// is restored so it can be decoded after verification.
func VerifyWebhookEvent(r *http.Request, key ed25519.PublicKey) bool {
	return VerifyInteraction(r, key)
}

// ParseWebhookEvent decodes a webhook event payload from JSON. Unknown fields
// are retained by neither the typed envelope nor the event body, allowing the
// decoder to remain forward compatible with Discord additions.
func ParseWebhookEvent(body []byte) (*WebhookEvent, error) {
	if len(body) == 0 {
		return nil, errors.New("webhook event body is empty")
	}

	event := &WebhookEvent{}
	if err := json.Unmarshal(body, event); err != nil {
		return nil, err
	}
	return event, nil
}

// ParseWebhookEventRequest decodes the request body and restores it for a
// later consumer. Verify the request with VerifyWebhookEvent before parsing.
func ParseWebhookEventRequest(r *http.Request) (*WebhookEvent, error) {
	if r == nil || r.Body == nil {
		return nil, errors.New("webhook event request body is nil")
	}
	if r.ContentLength > MaxInteractionBodySize {
		return nil, errors.New("webhook event body exceeds the maximum size")
	}

	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxInteractionBodySize+1))
	r.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > MaxInteractionBodySize {
		return nil, errors.New("webhook event body exceeds the maximum size")
	}
	return ParseWebhookEvent(body)
}

// AcknowledgeWebhookEvent writes the 204 response Discord expects for both
// PING and successfully received event payloads.
func AcknowledgeWebhookEvent(w http.ResponseWriter) {
	if w == nil {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}
