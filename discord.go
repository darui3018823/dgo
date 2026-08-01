// dgo - Discord bindings for Go
// Available at https://github.com/darui3018823/dgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains high level helper functions and easy entry points for the
// entire dgo package. These functions are being developed and are very
// experimental at this point.  They will most likely change so please use the
// low level functions if that's a problem.

// package dgo provides Discord binding for Go
package dgo

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ErrInvalidSessionToken is returned when a session credential is empty,
// malformed, or does not use an explicitly supported authorization scheme.
var ErrInvalidSessionToken = errors.New("invalid session token")

// New creates a new Discord session with provided token.
// If the token is for a bot, it must be prefixed with "Bot "
//
//	e.g. "Bot ..."
//
// Or if it is an OAuth2 token, it must be prefixed with "Bearer "
//
//	e.g. "Bearer ..."
func New(token string) (s *Session, err error) {
	if token != "" {
		switch {
		case strings.HasPrefix(token, "Bot "):
			if !validRawCredential(strings.TrimPrefix(token, "Bot ")) {
				return nil, ErrInvalidSessionToken
			}
		case strings.HasPrefix(token, "Bearer "):
			if !validRawCredential(strings.TrimPrefix(token, "Bearer ")) {
				return nil, ErrInvalidSessionToken
			}
		default:
			return nil, ErrInvalidSessionToken
		}
	}

	return newSession(token), nil
}

// NewBot creates a session from a raw bot token. It adds the required Bot
// authorization scheme so callers cannot accidentally construct a user-token
// session.
func NewBot(token string) (*Session, error) {
	if !validRawCredential(token) {
		return nil, ErrInvalidSessionToken
	}
	return newSession("Bot " + token), nil
}

// NewOAuth2 creates a REST session from a raw OAuth2 bearer token.
func NewOAuth2(token string) (*Session, error) {
	if !validRawCredential(token) {
		return nil, ErrInvalidSessionToken
	}
	return newSession("Bearer " + token), nil
}

func validRawCredential(token string) bool {
	return token != "" &&
		token == strings.TrimSpace(token) &&
		!strings.ContainsAny(token, " \t\r\n")
}

func newSession(token string) *Session {
	versionLabel := formattedVersion(VERSION)

	// Create an empty Session interface.
	s := &Session{
		State:                              NewState(),
		Ratelimiter:                        NewRatelimiter(),
		StateEnabled:                       true,
		Compress:                           true,
		ShouldReconnectOnError:             true,
		ShouldReconnectVoiceOnSessionError: true,
		ShouldRetryOnRateLimit:             true,
		AllowedMentions:                    &MessageAllowedMentions{Parse: []AllowedMentionType{}},
		ShardID:                            0,
		ShardCount:                         1,
		MaxRestRetries:                     3,
		MaxRestResponseSize:                32 << 20,
		MaxRestRateLimitWait:               time.Minute,
		MaxRestRetryWait:                   5 * time.Minute,
		Client:                             &http.Client{Timeout: (20 * time.Second)},
		Dialer:                             websocket.DefaultDialer,
		UserAgent:                          "DiscordBot (https://github.com/darui3018823/dgo, " + versionLabel + ")",
		sequence:                           new(int64),
		LastHeartbeatAck:                   time.Now().UTC(),
		Logger:                             slog.Default(),
	}

	// Initialize the Identify Package with defaults
	// These can be modified prior to calling Open()
	s.Identify.Compress = true
	s.Identify.LargeThreshold = 250
	s.Identify.Properties.OS = runtime.GOOS
	s.Identify.Properties.Browser = "dgo " + versionLabel
	s.Identify.Intents = IntentsNone
	s.Identify.Token = token
	s.Token = token

	return s
}
