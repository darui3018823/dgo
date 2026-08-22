// dgo - Discord bindings for Go
// Available at https://github.com/darui3018823/dgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains low level functions for interacting with the Discord
// data websocket interface.

package dgo

import (
	"bytes"
	"compress/zlib"
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// ErrWSAlreadyOpen is thrown when you attempt to open
// a websocket that already is open.
var ErrWSAlreadyOpen = errors.New("web socket already opened")

// ErrWSNotFound is thrown when you attempt to use a websocket
// that doesn't exist
var ErrWSNotFound = errors.New("no websocket connection exists")

// ErrWSShardBounds is thrown when you try to use a shard ID that is
// more than the total shard count
var ErrWSShardBounds = errors.New("ShardID must be less than ShardCount")

// ErrWSInvalidToken is returned when a non-bot credential is used with the
// Gateway. OAuth2 bearer tokens are supported by REST endpoints only.
var ErrWSInvalidToken = errors.New("gateway connections require a token prefixed with \"Bot \"")

const discordHostnameLabel = `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`

var (
	discordGatewayHostname = regexp.MustCompile(`(?i)^(?:` + discordHostnameLabel + `\.)*discord\.gg$`)
	discordGatewayURL      = regexp.MustCompile(`(?i)^wss://(?:` + discordHostnameLabel + `\.)*discord\.gg(?::443)?(?:[/?][^#\r\n]*)?$`)
)

// GuildMembersRequestRateLimitError is returned before sending a request for
// all guild members when Discord's per-guild, per-bot cooldown is still active.
type GuildMembersRequestRateLimitError struct {
	GuildID    string
	RetryAfter time.Duration
}

// Error implements error.
func (e *GuildMembersRequestRateLimitError) Error() string {
	return fmt.Sprintf("requesting all members for guild %s is rate limited for %s", e.GuildID, e.RetryAfter)
}

type resumePacket struct {
	Op   int `json:"op"`
	Data struct {
		Token     string `json:"token"`
		SessionID string `json:"session_id"`
		Sequence  int64  `json:"seq"`
	} `json:"d"`
}

// Open creates a websocket connection to Discord.
// See: https://discord.com/developers/docs/topics/gateway#connecting
func (s *Session) Open() error {
	return s.OpenWithContext(context.Background())
}

type gatewayConnectionState uint8

const (
	gatewayStateClosed gatewayConnectionState = iota
	gatewayStateConnecting
	gatewayStateConnected
	gatewayStateClosing
)

type gatewaySessionLifecycle struct {
	id     uint64
	ctx    context.Context
	cancel context.CancelFunc
}

type gatewayConnectionLifecycle struct {
	id        uint64
	lifecycle *gatewaySessionLifecycle
	ctx       context.Context
	cancel    context.CancelFunc
	ws        *websocket.Conn
	writes    *gatewayWriteQueue
	stop      chan interface{}
	ready     chan gatewayAttemptResult
	finish    sync.Once
	connected bool
}

type gatewayAttemptResult struct {
	err        error
	action     GatewayCloseRecovery
	retry      bool
	retryAfter time.Duration
}

type gatewayQueuedEvent struct {
	eventType string
	event     interface{}
}

var closedGatewayContext = func() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}()

// OpenWithContext opens the Gateway and uses ctx as the Session lifetime.
// Cancelling ctx stops the active connection and every automatic reconnect
// generation created from it.
func (s *Session) OpenWithContext(ctx context.Context) error {
	s.log(LogInformational, "called")
	if ctx == nil {
		return errors.New("gateway context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	token := s.gatewayToken()
	if !strings.HasPrefix(token, "Bot ") || strings.TrimSpace(strings.TrimPrefix(token, "Bot ")) == "" {
		return ErrWSInvalidToken
	}

	lifecycle, err := s.beginGatewayLifecycle(ctx)
	if err != nil {
		return err
	}
	if err = s.connectGatewayUntilReady(lifecycle); err != nil {
		s.stopGatewayLifecycle(lifecycle, websocket.CloseNormalClosure, false)
		return err
	}
	s.log(LogInformational, "gateway connection is ready")
	return nil
}

func (s *Session) gatewayToken() string {
	s.RLock()
	token := s.Identify.Token
	if token == "" {
		token = s.Token
	}
	s.RUnlock()
	return token
}

func (s *Session) beginGatewayLifecycle(parent context.Context) (*gatewaySessionLifecycle, error) {
	s.gatewayLifecycleMu.Lock()
	defer s.gatewayLifecycleMu.Unlock()

	s.RLock()
	hasLegacyConnection := s.wsConn != nil || s.listening != nil
	s.RUnlock()
	if s.gatewayLifecycle != nil || s.gatewayConnection != nil || hasLegacyConnection {
		return nil, ErrWSAlreadyOpen
	}

	ctx, cancel := context.WithCancel(parent)
	s.gatewayLifecycleCounter++
	lifecycle := &gatewaySessionLifecycle{
		id:     s.gatewayLifecycleCounter,
		ctx:    ctx,
		cancel: cancel,
	}
	s.gatewayLifecycle = lifecycle
	s.gatewayState = gatewayStateConnecting
	return lifecycle, nil
}

func (s *Session) connectGatewayUntilReady(lifecycle *gatewaySessionLifecycle) error {
	for {
		result := s.openGatewayGeneration(lifecycle)
		if result.err == nil {
			return nil
		}
		if err := lifecycle.ctx.Err(); err != nil {
			return err
		}
		if !result.retry || result.action == GatewayCloseRecoveryStop {
			return result.err
		}
		if result.action == GatewayCloseRecoveryIdentify {
			s.invalidateGatewaySession()
		}
		if err := waitGatewayContext(lifecycle.ctx, result.retryAfter); err != nil {
			return err
		}
	}
}

func (s *Session) openGatewayGeneration(lifecycle *gatewaySessionLifecycle) gatewayAttemptResult {
	conn, err := s.beginGatewayGeneration(lifecycle)
	if err != nil {
		return gatewayAttemptResult{err: err, action: GatewayCloseRecoveryStop}
	}

	gateway, err := s.initialGateway(conn.ctx)
	if err != nil {
		s.abortGatewayGeneration(conn)
		return gatewayAttemptResult{err: err, action: GatewayCloseRecoveryResume}
	}
	connectGateway, usingResumeGateway, err := s.gatewayConnectURL()
	if err != nil {
		s.abortGatewayGeneration(conn)
		return gatewayAttemptResult{err: err, action: GatewayCloseRecoveryStop}
	}
	// Validate the normalized URL itself at the network boundary. Hostname-only
	// checks do not prove that the complete value passed to the dialer is safe.
	if !discordGatewayURL.MatchString(connectGateway) {
		s.abortGatewayGeneration(conn)
		return gatewayAttemptResult{
			err:    errors.New("gateway URL could not be normalized safely"),
			action: GatewayCloseRecoveryStop,
		}
	}

	s.log(LogInformational, "connecting to gateway %s", connectGateway)
	header := http.Header{}
	header.Add("accept-encoding", "zlib")
	wsConn, _, err := s.Dialer.DialContext(conn.ctx, connectGateway, header)
	if err != nil {
		s.log(LogError, "error connecting to gateway %s, %s", connectGateway, err)
		if usingResumeGateway {
			s.gatewaySessionMu.Lock()
			s.resumeGatewayURL = ""
			s.gatewaySessionMu.Unlock()
		} else {
			s.Lock()
			if s.gateway == gateway {
				s.gateway = ""
			}
			s.Unlock()
		}
		s.abortGatewayGeneration(conn)
		return gatewayAttemptResult{err: err, action: GatewayCloseRecoveryResume}
	}
	if err = s.attachGatewayWebsocket(conn, wsConn); err != nil {
		_ = wsConn.Close()
		s.abortGatewayGeneration(conn)
		return gatewayAttemptResult{err: err, action: GatewayCloseRecoveryStop}
	}

	s.startGatewayRoutine(func() {
		select {
		case <-conn.ctx.Done():
			s.finishGatewayGeneration(conn, gatewayAttemptResult{
				err:    conn.ctx.Err(),
				action: GatewayCloseRecoveryStop,
			}, websocket.CloseNormalClosure, false)
		case <-conn.stop:
		}
	})

	messageType, message, err := wsConn.ReadMessage()
	if err != nil {
		result := gatewayAttemptResult{
			err:    err,
			action: gatewayReconnectActionForError(err),
			retry:  true,
		}
		s.finishGatewayGeneration(conn, result, websocket.CloseNormalClosure, false)
		return result
	}
	event, err := s.onEvent(messageType, message)
	if err != nil {
		result := gatewayAttemptResult{err: err, action: GatewayCloseRecoveryStop}
		s.finishGatewayGeneration(conn, result, websocket.CloseUnsupportedData, true)
		return result
	}
	if event.Operation != 10 {
		result := gatewayAttemptResult{
			err:    fmt.Errorf("expecting Op 10, got Op %d instead", event.Operation),
			action: GatewayCloseRecoveryStop,
		}
		s.finishGatewayGeneration(conn, result, websocket.CloseProtocolError, true)
		return result
	}

	var hello helloOp
	if err = json.Unmarshal(event.RawData, &hello); err != nil {
		result := gatewayAttemptResult{
			err:    fmt.Errorf("error unmarshalling helloOp: %w", err),
			action: GatewayCloseRecoveryStop,
		}
		s.finishGatewayGeneration(conn, result, websocket.CloseUnsupportedData, true)
		return result
	}
	if hello.HeartbeatInterval <= 0 {
		result := gatewayAttemptResult{
			err:    fmt.Errorf("invalid gateway heartbeat interval %s", hello.HeartbeatInterval),
			action: GatewayCloseRecoveryStop,
		}
		s.finishGatewayGeneration(conn, result, websocket.CloseProtocolError, true)
		return result
	}

	s.Lock()
	s.LastHeartbeatAck = time.Now().UTC()
	s.LastHeartbeatSent = time.Time{}
	s.Unlock()
	s.startGatewayRoutine(func() { s.heartbeat(conn, hello.HeartbeatInterval) })
	s.startGatewayRoutine(func() { s.listen(conn) })

	if err = s.authenticateGatewayGeneration(conn, connectGateway); err != nil {
		result := gatewayAttemptResult{err: err, action: GatewayCloseRecoveryResume}
		s.finishGatewayGeneration(conn, result, websocket.CloseProtocolError, true)
		return result
	}

	select {
	case result := <-conn.ready:
		if result.err != nil {
			return result
		}
		if err = s.activateGatewayGeneration(conn); err != nil {
			result.err = err
			result.action = GatewayCloseRecoveryStop
			s.finishGatewayGeneration(conn, result, websocket.CloseNormalClosure, false)
			return result
		}
		return gatewayAttemptResult{}
	case <-conn.ctx.Done():
		select {
		case result := <-conn.ready:
			return result
		default:
		}
		result := gatewayAttemptResult{
			err:    conn.ctx.Err(),
			action: GatewayCloseRecoveryStop,
		}
		s.finishGatewayGeneration(conn, result, websocket.CloseNormalClosure, false)
		return result
	}
}

func (s *Session) beginGatewayGeneration(lifecycle *gatewaySessionLifecycle) (*gatewayConnectionLifecycle, error) {
	s.gatewayLifecycleMu.Lock()
	defer s.gatewayLifecycleMu.Unlock()
	if s.gatewayLifecycle != lifecycle || lifecycle.ctx.Err() != nil {
		if err := lifecycle.ctx.Err(); err != nil {
			return nil, err
		}
		return nil, context.Canceled
	}
	if s.gatewayConnection != nil {
		return nil, ErrWSAlreadyOpen
	}

	ctx, cancel := context.WithCancel(lifecycle.ctx)
	s.gatewayConnectionCounter++
	conn := &gatewayConnectionLifecycle{
		id:        s.gatewayConnectionCounter,
		lifecycle: lifecycle,
		ctx:       ctx,
		cancel:    cancel,
		stop:      make(chan interface{}),
		ready:     make(chan gatewayAttemptResult, 1),
	}
	s.gatewayConnection = conn
	s.gatewayState = gatewayStateConnecting
	return conn, nil
}

func (s *Session) initialGateway(ctx context.Context) (string, error) {
	s.RLock()
	gateway := s.gateway
	s.RUnlock()
	if gateway != "" {
		return gateway, nil
	}

	fetched, err := s.Gateway(WithContext(ctx))
	if err != nil {
		return "", err
	}
	s.Lock()
	if s.gateway == "" {
		s.gateway = fetched
	}
	gateway = s.gateway
	s.Unlock()
	return gateway, nil
}

func (s *Session) attachGatewayWebsocket(conn *gatewayConnectionLifecycle, wsConn *websocket.Conn) error {
	s.gatewayLifecycleMu.Lock()
	defer s.gatewayLifecycleMu.Unlock()
	if s.gatewayLifecycle != conn.lifecycle || s.gatewayConnection != conn || conn.ctx.Err() != nil {
		return context.Canceled
	}

	conn.ws = wsConn
	conn.writes = newGatewayWriteQueue(conn.ctx, wsConn, &s.wsMutex, nil)
	wsConn.SetCloseHandler(func(code int, text string) error { return nil })
	s.Lock()
	s.wsConn = wsConn
	s.listening = conn.stop
	s.DataReady = false
	s.Unlock()
	s.startGatewayRoutine(conn.writes.run)
	return nil
}

func (s *Session) writeGatewayGeneration(conn *gatewayConnectionLifecycle, data interface{}) error {
	if conn == nil || conn.ws == nil || conn.writes == nil {
		return ErrWSNotFound
	}
	return conn.writes.enqueue(conn.ctx, data)
}

func (s *Session) writeGatewayCurrent(data interface{}) error {
	s.gatewayLifecycleMu.Lock()
	conn := s.gatewayConnection
	s.gatewayLifecycleMu.Unlock()
	if conn != nil {
		return s.writeGatewayGeneration(conn, data)
	}

	s.RLock()
	wsConn := s.wsConn
	s.RUnlock()
	if wsConn == nil {
		return ErrWSNotFound
	}
	s.wsMutex.Lock()
	err := wsConn.WriteJSON(data)
	s.wsMutex.Unlock()
	return err
}

// writeGatewayPriorityCurrent writes a Gateway control packet without waiting
// behind application traffic already rate-limited by the outbound queue. The
// Gateway requires server-requested heartbeats to be sent within five seconds,
// so this path only bypasses the queue for protocol-critical control traffic.
func (s *Session) writeGatewayPriorityCurrent(data interface{}) error {
	s.gatewayLifecycleMu.Lock()
	conn := s.gatewayConnection
	s.gatewayLifecycleMu.Unlock()
	if conn != nil {
		if conn.ws == nil {
			return ErrWSNotFound
		}
		if err := conn.ctx.Err(); err != nil {
			return err
		}
		s.wsMutex.Lock()
		defer s.wsMutex.Unlock()
		if err := conn.ctx.Err(); err != nil {
			return err
		}
		return conn.ws.WriteJSON(data)
	}

	s.RLock()
	wsConn := s.wsConn
	s.RUnlock()
	if wsConn == nil {
		return ErrWSNotFound
	}
	s.wsMutex.Lock()
	err := wsConn.WriteJSON(data)
	s.wsMutex.Unlock()
	return err
}

func (s *Session) authenticateGatewayGeneration(conn *gatewayConnectionLifecycle, connectGateway string) error {
	token := s.gatewayToken()
	sequence := atomic.LoadInt64(s.sequence)
	s.gatewaySessionMu.RLock()
	sessionID := s.sessionID
	s.gatewaySessionMu.RUnlock()
	if sessionID == "" {
		s.RLock()
		coordinator := s.IdentifyCoordinator
		shardID := s.ShardID
		if s.Identify.Shard != nil {
			shardID = s.Identify.Shard[0]
		}
		s.RUnlock()
		if coordinator != nil {
			if err := coordinator.Acquire(conn.ctx, shardID); err != nil {
				return fmt.Errorf(
					"error waiting to identify shard %d with gateway %s: %w",
					shardID,
					connectGateway,
					err,
				)
			}
		}
		if err := s.identify(conn); err != nil {
			return fmt.Errorf("error sending identify packet to gateway %s: %w", connectGateway, err)
		}
		return nil
	}

	packet := resumePacket{}
	packet.Op = 6
	packet.Data.Token = token
	packet.Data.SessionID = sessionID
	packet.Data.Sequence = sequence
	s.log(LogInformational, "sending resume packet to gateway")
	err := s.writeGatewayGeneration(conn, packet)
	if err != nil {
		return fmt.Errorf("error sending gateway resume packet to %s: %w", connectGateway, err)
	}
	return nil
}

func (s *Session) activateGatewayGeneration(conn *gatewayConnectionLifecycle) error {
	s.gatewayLifecycleMu.Lock()
	if s.gatewayLifecycle != conn.lifecycle || s.gatewayConnection != conn || conn.ctx.Err() != nil {
		s.gatewayLifecycleMu.Unlock()
		return context.Canceled
	}
	conn.connected = true
	s.gatewayState = gatewayStateConnected
	s.Lock()
	s.DataReady = true
	if s.State == nil {
		state := NewState()
		state.TrackChannels = false
		state.TrackEmojis = false
		state.TrackMembers = false
		state.TrackRoles = false
		state.TrackSoundboardSounds = false
		state.TrackVoice = false
		s.State = state
	}
	if s.VoiceConnections == nil {
		s.VoiceConnections = make(map[string]*VoiceConnection)
	}
	s.Unlock()
	s.enqueueGatewayEvent(connectEventType, &Connect{})
	s.gatewayLifecycleMu.Unlock()
	s.drainGatewayEvents()
	return nil
}

func (s *Session) gatewayConnectURL() (connectURL string, usingResume bool, err error) {
	s.RLock()
	baseURL := s.gateway
	s.RUnlock()
	s.gatewaySessionMu.RLock()
	if s.sessionID != "" && s.resumeGatewayURL != "" {
		baseURL = s.resumeGatewayURL
		usingResume = true
	}
	s.gatewaySessionMu.RUnlock()

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", false, fmt.Errorf("invalid gateway URL: %w", err)
	}
	if parsed.Scheme != "wss" {
		return "", false, fmt.Errorf("invalid gateway URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", false, errors.New("gateway URL must include a host")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return "", false, errors.New("gateway URL must not include user information or a fragment")
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return "", false, errors.New("gateway URL must use the default secure WebSocket port")
	}
	if !discordGatewayHostname.MatchString(parsed.Hostname()) {
		return "", false, errors.New("gateway URL must use an approved Discord host")
	}
	query := parsed.Query()
	query.Set("v", APIVersion)
	query.Set("encoding", "json")
	parsed.RawQuery = query.Encode()
	return parsed.String(), usingResume, nil
}

// listen is the only reader for one Gateway connection generation.
func (s *Session) listen(conn *gatewayConnectionLifecycle) {
	s.log(LogInformational, "called")
	for {
		messageType, message, err := conn.ws.ReadMessage()
		if err != nil {
			if conn.ctx.Err() == nil {
				s.RLock()
				gateway := s.gateway
				s.RUnlock()
				s.log(LogWarning, "error reading from gateway %s websocket, %s", gateway, err)
				action := gatewayReconnectActionForError(err)
				s.handleEvent(gatewayCloseEventType, newGatewayCloseEvent(err, action))
				if action != GatewayCloseRecoveryResume {
					s.invalidateGatewaySession()
				}
				s.finishGatewayGeneration(conn, gatewayAttemptResult{
					err:    err,
					action: action,
					retry:  action != GatewayCloseRecoveryStop,
				}, websocket.CloseNormalClosure, false)
			}
			return
		}

		select {
		case <-conn.ctx.Done():
			return
		default:
		}

		event, eventErr := s.onEvent(messageType, message)
		if eventErr != nil {
			s.log(LogWarning, "error processing gateway event: %s", eventErr)
			continue
		}
		if event == nil {
			continue
		}
		switch event.Operation {
		case 7:
			s.finishGatewayGeneration(conn, gatewayAttemptResult{
				err:    errors.New("gateway requested reconnect"),
				action: GatewayCloseRecoveryResume,
				retry:  true,
			}, websocket.CloseServiceRestart, true)
			return
		case 9:
			var resumable bool
			if err = json.Unmarshal(event.RawData, &resumable); err != nil {
				s.finishGatewayGeneration(conn, gatewayAttemptResult{
					err:    fmt.Errorf("invalid session payload: %w", err),
					action: GatewayCloseRecoveryStop,
				}, websocket.CloseUnsupportedData, true)
				return
			}
			action := GatewayCloseRecoveryResume
			retryAfter := time.Duration(0)
			if !resumable {
				action = GatewayCloseRecoveryIdentify
				retryAfter = invalidSessionBackoff()
			}
			s.finishGatewayGeneration(conn, gatewayAttemptResult{
				err:        fmt.Errorf("gateway session invalidated; resumable=%t", resumable),
				action:     action,
				retry:      true,
				retryAfter: retryAfter,
			}, websocket.CloseServiceRestart, true)
			return
		}
		if event.Type == readyEventType || event.Type == resumedEventType {
			select {
			case conn.ready <- gatewayAttemptResult{}:
			default:
			}
		}
	}
}

func gatewayReconnectActionForError(err error) GatewayCloseRecovery {
	var closeError *websocket.CloseError
	if !errors.As(err, &closeError) {
		return GatewayCloseRecoveryResume
	}
	switch closeError.Code {
	case 4004, 4010, 4011, 4012, 4013, 4014:
		return GatewayCloseRecoveryStop
	case websocket.CloseNormalClosure, websocket.CloseGoingAway, 4007, 4009:
		return GatewayCloseRecoveryIdentify
	default:
		return GatewayCloseRecoveryResume
	}
}

func newGatewayCloseEvent(err error, recovery GatewayCloseRecovery) *GatewayClose {
	event := &GatewayClose{Recovery: recovery, Err: err}
	var closeError *websocket.CloseError
	if errors.As(err, &closeError) {
		event.Code = closeError.Code
		event.Reason = closeError.Text
	}
	return event
}

type heartbeatOp struct {
	Op   int   `json:"op"`
	Data int64 `json:"d"`
}

type helloOp struct {
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
}

// FailedHeartbeatAcks is retained for compatibility. Discord requires clients
// to restart the connection when one heartbeat remains unacknowledged at the
// next interval.
const FailedHeartbeatAcks = 1

// HeartbeatLatency returns the latency between heartbeat acknowledgement and heartbeat send.
// It returns zero until a heartbeat has been sent and acknowledged.
func (s *Session) HeartbeatLatency() time.Duration {
	s.RLock()
	sent := s.LastHeartbeatSent
	ack := s.LastHeartbeatAck
	s.RUnlock()
	if sent.IsZero() || ack.IsZero() || ack.Before(sent) {
		return 0
	}
	return ack.Sub(sent)
}

// MissedHeartbeatAcks returns the cumulative number of Gateway reconnects
// caused by a missing heartbeat acknowledgement.
func (s *Session) MissedHeartbeatAcks() uint64 {
	return atomic.LoadUint64(&s.missedHeartbeatAcks)
}

// heartbeat sends regular heartbeats to Discord so it knows the client
// is still connected.  If you do not send these heartbeats Discord will
// disconnect the websocket connection after a few seconds.
func (s *Session) heartbeat(conn *gatewayConnectionLifecycle, heartbeatInterval time.Duration) {
	s.log(LogInformational, "called")
	if conn == nil || conn.ws == nil || heartbeatInterval <= 0 {
		return
	}

	interval := heartbeatInterval * time.Millisecond
	initialTimer := time.NewTimer(heartbeatJitter(interval))
	select {
	case <-initialTimer.C:
	case <-conn.ctx.Done():
		if !initialTimer.Stop() {
			select {
			case <-initialTimer.C:
			default:
			}
		}
		return
	}

	var err error
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		s.RLock()
		lastAck := s.LastHeartbeatAck
		lastSent := s.LastHeartbeatSent
		s.RUnlock()
		if heartbeatAckPending(lastAck, lastSent) {
			atomic.AddUint64(&s.missedHeartbeatAcks, 1)
			s.log(LogError, "previous gateway heartbeat was not acknowledged; reconnecting")
			s.finishGatewayGeneration(conn, gatewayAttemptResult{
				err:    errors.New("gateway heartbeat was not acknowledged"),
				action: GatewayCloseRecoveryResume,
				retry:  true,
			}, 4000, true)
			return
		}
		sequence := atomic.LoadInt64(s.sequence)
		s.log(LogDebug, "sending gateway websocket heartbeat seq %d", sequence)
		s.Lock()
		s.LastHeartbeatSent = time.Now().UTC()
		s.Unlock()
		err = s.writeGatewayGeneration(conn, heartbeatOp{1, sequence})
		if err != nil {
			s.log(LogError, "error sending heartbeat to gateway: %s", err)
			s.finishGatewayGeneration(conn, gatewayAttemptResult{
				err:    err,
				action: GatewayCloseRecoveryResume,
				retry:  true,
			}, 4000, true)
			return
		}

		select {
		case <-ticker.C:
			// continue loop and send heartbeat
		case <-conn.ctx.Done():
			return
		}
	}
}

func (s *Session) abortGatewayGeneration(conn *gatewayConnectionLifecycle) {
	conn.finish.Do(func() {
		s.gatewayLifecycleMu.Lock()
		if s.gatewayConnection == conn {
			s.gatewayConnection = nil
			s.gatewayState = gatewayStateClosed
		}
		if s.gatewayLifecycle == conn.lifecycle && conn.lifecycle.ctx.Err() != nil {
			s.gatewayLifecycle = nil
			s.gatewayReconnectRunning = false
		}
		s.gatewayLifecycleMu.Unlock()
		conn.cancel()
		close(conn.stop)
	})
}

func (s *Session) finishGatewayGeneration(
	conn *gatewayConnectionLifecycle,
	result gatewayAttemptResult,
	closeCode int,
	sendCloseFrame bool,
) {
	if conn == nil {
		return
	}
	conn.finish.Do(func() {
		s.gatewayLifecycleMu.Lock()
		wasCurrent := s.gatewayConnection == conn
		wasConnected := conn.connected
		conn.connected = false
		if wasCurrent {
			s.gatewayConnection = nil
			s.gatewayState = gatewayStateClosed
			s.Lock()
			if s.wsConn == conn.ws {
				s.wsConn = nil
				s.listening = nil
				s.DataReady = false
			}
			s.Unlock()
		}
		lifecycleActive := s.gatewayLifecycle == conn.lifecycle && conn.lifecycle.ctx.Err() == nil
		if wasConnected && result.action == GatewayCloseRecoveryStop && s.gatewayLifecycle == conn.lifecycle {
			conn.lifecycle.cancel()
			s.gatewayLifecycle = nil
			s.gatewayReconnectRunning = false
			lifecycleActive = false
		}
		if !lifecycleActive && s.gatewayLifecycle == conn.lifecycle {
			s.gatewayLifecycle = nil
			s.gatewayReconnectRunning = false
		}
		if wasConnected {
			s.enqueueGatewayEvent(disconnectEventType, &Disconnect{})
		}
		s.gatewayLifecycleMu.Unlock()

		if !wasConnected {
			select {
			case conn.ready <- result:
			default:
			}
		}
		conn.cancel()
		close(conn.stop)
		if conn.ws != nil {
			if sendCloseFrame {
				s.wsMutex.Lock()
				if err := conn.ws.WriteMessage(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(closeCode, ""),
				); err != nil {
					s.log(LogDebug, "error sending gateway close frame: %s", err)
				}
				s.wsMutex.Unlock()
			}
			if err := conn.ws.Close(); err != nil {
				s.log(LogDebug, "error closing gateway websocket: %s", err)
			}
		}

		s.drainGatewayEvents()
		if wasConnected && wasCurrent && lifecycleActive && result.retry {
			s.startGatewayReconnect(conn.lifecycle, result)
		}
	})
}

func (s *Session) startGatewayReconnect(
	lifecycle *gatewaySessionLifecycle,
	initial gatewayAttemptResult,
) {
	s.RLock()
	shouldReconnect := s.ShouldReconnectOnError
	s.RUnlock()
	if !shouldReconnect || initial.action == GatewayCloseRecoveryStop {
		s.stopGatewayLifecycle(lifecycle, websocket.CloseNormalClosure, false)
		return
	}

	s.gatewayLifecycleMu.Lock()
	if s.gatewayLifecycle != lifecycle ||
		lifecycle.ctx.Err() != nil ||
		s.gatewayReconnectRunning ||
		s.gatewayConnection != nil {
		s.gatewayLifecycleMu.Unlock()
		return
	}
	s.gatewayReconnectRunning = true
	s.gatewayState = gatewayStateConnecting
	s.gatewayLifecycleMu.Unlock()

	s.startGatewayRoutine(func() {
		defer func() {
			s.gatewayLifecycleMu.Lock()
			if s.gatewayLifecycle == lifecycle {
				s.gatewayReconnectRunning = false
				if s.gatewayConnection == nil {
					s.gatewayState = gatewayStateClosed
				}
			}
			s.gatewayLifecycleMu.Unlock()
		}()

		action := initial.action
		delay := initial.retryAfter
		backoff := time.Second
		for {
			if action == GatewayCloseRecoveryIdentify {
				s.invalidateGatewaySession()
			}
			if err := waitGatewayContext(lifecycle.ctx, delay); err != nil {
				return
			}

			s.log(LogInformational, "trying to reconnect to gateway")
			result := s.openGatewayGeneration(lifecycle)
			if result.err == nil {
				s.log(LogInformational, "successfully reconnected to gateway")
				s.reconnectVoiceConnections(lifecycle.ctx)
				return
			}
			if lifecycle.ctx.Err() != nil {
				return
			}
			if result.action == GatewayCloseRecoveryStop {
				s.stopGatewayLifecycle(lifecycle, websocket.CloseNormalClosure, false)
				return
			}

			action = result.action
			delay = result.retryAfter
			if delay <= 0 {
				delay = backoff
				if backoff < 10*time.Minute {
					backoff *= 2
					if backoff > 10*time.Minute {
						backoff = 10 * time.Minute
					}
				}
			}
		}
	})
}

func (s *Session) reconnectVoiceConnections(ctx context.Context) {
	s.RLock()
	if !s.ShouldReconnectVoiceOnSessionError {
		s.RUnlock()
		return
	}
	voices := make([]*VoiceConnection, 0, len(s.VoiceConnections))
	for _, voice := range s.VoiceConnections {
		if voice != nil {
			voices = append(voices, voice)
		}
	}
	s.RUnlock()

	for _, voice := range voices {
		if ctx.Err() != nil {
			return
		}
		s.log(LogInformational, "reconnecting voice connection to guild %s", voice.GuildID)
		voice := voice
		s.startVoiceRoutine(func() { voice.reconnect() })
	}
}

func (s *Session) stopGatewayLifecycle(
	lifecycle *gatewaySessionLifecycle,
	closeCode int,
	sendCloseFrame bool,
) {
	if lifecycle == nil {
		return
	}
	s.gatewayLifecycleMu.Lock()
	if s.gatewayLifecycle != lifecycle {
		s.gatewayLifecycleMu.Unlock()
		return
	}
	s.gatewayLifecycle = nil
	s.gatewayReconnectRunning = false
	s.gatewayState = gatewayStateClosing
	conn := s.gatewayConnection
	lifecycle.cancel()
	s.gatewayLifecycleMu.Unlock()

	if conn != nil {
		s.finishGatewayGeneration(conn, gatewayAttemptResult{
			err:    context.Canceled,
			action: GatewayCloseRecoveryStop,
		}, closeCode, sendCloseFrame)
	}
	s.gatewayLifecycleMu.Lock()
	if s.gatewayConnection == nil {
		s.gatewayState = gatewayStateClosed
	}
	s.gatewayLifecycleMu.Unlock()
}

func (s *Session) startGatewayRoutine(fn func()) {
	atomic.AddInt64(&s.gatewayRoutineCount, 1)
	go func() {
		defer atomic.AddInt64(&s.gatewayRoutineCount, -1)
		fn()
	}()
}

// gatewayContext returns the active Session lifetime. A closed context is
// returned when the Session has no active Gateway lifetime.
func (s *Session) gatewayContext() context.Context {
	s.gatewayLifecycleMu.Lock()
	defer s.gatewayLifecycleMu.Unlock()
	if s.gatewayLifecycle == nil {
		return closedGatewayContext
	}
	return s.gatewayLifecycle.ctx
}

// startVoiceRoutine starts and tracks work only while a Gateway lifetime is
// active. Voice loops should separately observe gatewayContext for cancellation.
func (s *Session) startVoiceRoutine(fn func()) bool {
	if fn == nil {
		return false
	}
	s.gatewayLifecycleMu.Lock()
	if s.gatewayLifecycle == nil || s.gatewayLifecycle.ctx.Err() != nil {
		s.gatewayLifecycleMu.Unlock()
		return false
	}
	s.voiceRoutineWG.Add(1)
	s.gatewayLifecycleMu.Unlock()
	go func() {
		defer s.voiceRoutineWG.Done()
		fn()
	}()
	return true
}

func (s *Session) enqueueGatewayEvent(eventType string, event interface{}) {
	s.gatewayEventMu.Lock()
	s.gatewayEvents = append(s.gatewayEvents, gatewayQueuedEvent{
		eventType: eventType,
		event:     event,
	})
	s.gatewayEventMu.Unlock()
}

func (s *Session) drainGatewayEvents() {
	s.gatewayEventMu.Lock()
	if s.gatewayEventDispatching {
		s.gatewayEventMu.Unlock()
		return
	}
	s.gatewayEventDispatching = true
	for len(s.gatewayEvents) > 0 {
		event := s.gatewayEvents[0]
		s.gatewayEvents = s.gatewayEvents[1:]
		s.gatewayEventMu.Unlock()
		s.handleEvent(event.eventType, event.event)
		s.gatewayEventMu.Lock()
	}
	s.gatewayEventDispatching = false
	s.gatewayEventMu.Unlock()
}

func waitGatewayContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func heartbeatJitter(interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	jitter, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(interval)))
	if err != nil {
		return interval / 2
	}
	return time.Duration(jitter.Int64())
}

func heartbeatAckPending(lastAck, lastSent time.Time) bool {
	return !lastSent.IsZero() && lastAck.Before(lastSent)
}

// UpdateStatusData is provided to UpdateStatusComplex()
type UpdateStatusData struct {
	IdleSince  *int        `json:"since"`
	Activities []*Activity `json:"activities"`
	AFK        bool        `json:"afk"`
	Status     string      `json:"status"`
}

type updateStatusOp struct {
	Op   int              `json:"op"`
	Data UpdateStatusData `json:"d"`
}

func newUpdateStatusData(idle int, activityType ActivityType, name, url string) *UpdateStatusData {
	usd := &UpdateStatusData{
		Status: "online",
	}

	if idle > 0 {
		usd.IdleSince = &idle
	}

	if name != "" {
		usd.Activities = []*Activity{{
			Name: name,
			Type: activityType,
			URL:  url,
		}}
	}

	return usd
}

// UpdateGameStatus is used to update the user's status.
// If idle>0 then set status to idle.
// If name!="" then set game.
// if otherwise, set status to active, and no activity.
func (s *Session) UpdateGameStatus(idle int, name string) (err error) {
	return s.UpdateStatusComplex(*newUpdateStatusData(idle, ActivityTypeGame, name, ""))
}

// UpdateWatchStatus is used to update the user's watch status.
// If idle>0 then set status to idle.
// If name!="" then set movie/stream.
// if otherwise, set status to active, and no activity.
func (s *Session) UpdateWatchStatus(idle int, name string) (err error) {
	return s.UpdateStatusComplex(*newUpdateStatusData(idle, ActivityTypeWatching, name, ""))
}

// UpdateStreamingStatus is used to update the user's streaming status.
// If idle>0 then set status to idle.
// If name!="" then set game.
// If name!="" and url!="" then set the status type to streaming with the URL set.
// if otherwise, set status to active, and no game.
func (s *Session) UpdateStreamingStatus(idle int, name string, url string) (err error) {
	gameType := ActivityTypeGame
	if url != "" {
		gameType = ActivityTypeStreaming
	}
	return s.UpdateStatusComplex(*newUpdateStatusData(idle, gameType, name, url))
}

// UpdateListeningStatus is used to set the user to "Listening to..."
// If name!="" then set to what user is listening to
// Else, set user to active and no activity.
func (s *Session) UpdateListeningStatus(name string) (err error) {
	return s.UpdateStatusComplex(*newUpdateStatusData(0, ActivityTypeListening, name, ""))
}

// UpdateCustomStatus is used to update the user's custom status.
// If state!="" then set the custom status.
// Else, set user to active and remove the custom status.
func (s *Session) UpdateCustomStatus(state string) (err error) {
	data := UpdateStatusData{
		Status: "online",
	}

	if state != "" {
		// Discord requires a non-empty activity name, therefore we provide "Custom Status" as a placeholder.
		data.Activities = []*Activity{{
			Name:  "Custom Status",
			Type:  ActivityTypeCustom,
			State: state,
		}}
	}

	return s.UpdateStatusComplex(data)
}

// UpdateStatusComplex allows for sending the raw status update data untouched by dgo.
func (s *Session) UpdateStatusComplex(usd UpdateStatusData) (err error) {
	// The protocol comment says "untouched by the client", but we need to normalize it here.
	// The Discord documentation lists `activities` as being nullable, but in practice this
	// doesn't seem to be the case. I had filed an issue about this at
	// https://github.com/discord/discord-api-docs/issues/2559, but as of writing this
	// haven't had any movement on it, so at this point I'm assuming this is an error,
	// and am fixing this bug accordingly. Because sending `null` for `activities` instantly
	// disconnects us, I think that disallowing it from being sent in `UpdateStatusComplex`
	// isn't that big of an issue.
	if usd.Activities == nil {
		usd.Activities = make([]*Activity, 0)
	}

	return s.writeGatewayCurrent(updateStatusOp{3, usd})
}

type requestGuildMembersData struct {
	GuildID   string    `json:"guild_id"`
	Query     *string   `json:"query,omitempty"`
	UserIDs   *[]string `json:"user_ids,omitempty"`
	Limit     int       `json:"limit"`
	Nonce     string    `json:"nonce,omitempty"`
	Presences bool      `json:"presences"`
}

type requestGuildMembersOp struct {
	Op   int                     `json:"op"`
	Data requestGuildMembersData `json:"d"`
}

// ChannelInfoField identifies an ephemeral channel field that can be requested
// from the Gateway.
type ChannelInfoField string

// Fields supported by RequestChannelInfo.
const (
	ChannelInfoFieldStatus         ChannelInfoField = "status"
	ChannelInfoFieldVoiceStartTime ChannelInfoField = "voice_start_time"
)

type requestChannelInfoData struct {
	GuildID string             `json:"guild_id"`
	Fields  []ChannelInfoField `json:"fields"`
}

type requestChannelInfoOp struct {
	Op   int                    `json:"op"`
	Data requestChannelInfoData `json:"d"`
}

type requestSoundboardSoundsData struct {
	GuildIDs []string `json:"guild_ids"`
}

type requestSoundboardSoundsOp struct {
	Op   int                         `json:"op"`
	Data requestSoundboardSoundsData `json:"d"`
}

// RequestSoundboardSounds requests the soundboard sounds for one or more
// guilds. Discord responds with one SoundboardSounds event per guild.
func (s *Session) RequestSoundboardSounds(guildIDs []string) error {
	if len(guildIDs) == 0 {
		return errors.New("at least one guild ID is required")
	}

	seen := make(map[string]struct{}, len(guildIDs))
	for _, guildID := range guildIDs {
		if strings.TrimSpace(guildID) == "" {
			return errors.New("soundboard sound request guild IDs must not be empty")
		}
		if _, exists := seen[guildID]; exists {
			return fmt.Errorf("soundboard sound request guild ID %q is duplicated", guildID)
		}
		seen[guildID] = struct{}{}
	}

	return s.GatewayWriteStruct(requestSoundboardSoundsOp{
		Op: 31,
		Data: requestSoundboardSoundsData{
			GuildIDs: append([]string(nil), guildIDs...),
		},
	})
}

// RequestChannelInfo requests ephemeral voice channel information for a guild.
// The Gateway responds with a ChannelInfo event.
func (s *Session) RequestChannelInfo(guildID string, fields ...ChannelInfoField) error {
	if guildID == "" {
		return errors.New("guild ID must not be empty")
	}
	if len(fields) == 0 {
		return errors.New("at least one channel info field is required")
	}
	seen := make(map[ChannelInfoField]struct{}, len(fields))
	for _, field := range fields {
		switch field {
		case ChannelInfoFieldStatus, ChannelInfoFieldVoiceStartTime:
		default:
			return fmt.Errorf("unsupported channel info field %q", field)
		}
		if _, exists := seen[field]; exists {
			return fmt.Errorf("channel info field %q is duplicated", field)
		}
		seen[field] = struct{}{}
	}
	return s.GatewayWriteStruct(requestChannelInfoOp{
		Op: 43,
		Data: requestChannelInfoData{
			GuildID: guildID,
			Fields:  append([]ChannelInfoField(nil), fields...),
		},
	})
}

// RequestGuildMembers requests guild members from the gateway
// The gateway responds with GuildMembersChunk events
// guildID   : Single Guild ID to request members of
// query     : String that username starts with, leave empty to return all members
// limit     : Max number of items to return, or 0 to request all members matched
// nonce     : Nonce to identify the Guild Members Chunk response
// presences : Whether to request presences of guild members
func (s *Session) RequestGuildMembers(guildID, query string, limit int, nonce string, presences bool) error {
	data := requestGuildMembersData{
		GuildID:   guildID,
		Query:     &query,
		Limit:     limit,
		Nonce:     nonce,
		Presences: presences,
	}
	return s.requestGuildMembers(data)
}

// RequestGuildMembersList requests guild members from the gateway
// The gateway responds with GuildMembersChunk events
// guildID   : Single Guild ID to request members of
// userIDs   : IDs of users to fetch
// limit     : Max number of items to return, or 0 to request all members matched
// nonce     : Nonce to identify the Guild Members Chunk response
// presences : Whether to request presences of guild members
func (s *Session) RequestGuildMembersList(guildID string, userIDs []string, limit int, nonce string, presences bool) error {
	data := requestGuildMembersData{
		GuildID:   guildID,
		UserIDs:   &userIDs,
		Limit:     limit,
		Nonce:     nonce,
		Presences: presences,
	}
	return s.requestGuildMembers(data)
}

// RequestGuildMembersBatch requests guild members from the gateway
// The gateway responds with GuildMembersChunk events
// guildID   : Slice of guild IDs to request members of
// query     : String that username starts with, leave empty to return all members
// limit     : Max number of items to return, or 0 to request all members matched
// nonce     : Nonce to identify the Guild Members Chunk response
// presences : Whether to request presences of guild members
//
// NOTE: this function is deprecated, please use RequestGuildMembers instead
func (s *Session) RequestGuildMembersBatch(guildIDs []string, query string, limit int, nonce string, presences bool) (err error) {
	if len(guildIDs) != 1 {
		return fmt.Errorf("request guild members accepts exactly one guild ID, got %d", len(guildIDs))
	}
	return s.RequestGuildMembers(guildIDs[0], query, limit, nonce, presences)
}

// RequestGuildMembersBatchList requests guild members from the gateway
// The gateway responds with GuildMembersChunk events
// guildID   : Slice of guild IDs to request members of
// userIDs   : IDs of users to fetch
// limit     : Max number of items to return, or 0 to request all members matched
// nonce     : Nonce to identify the Guild Members Chunk response
// presences : Whether to request presences of guild members
//
// NOTE: this function is deprecated, please use RequestGuildMembersList instead
func (s *Session) RequestGuildMembersBatchList(guildIDs []string, userIDs []string, limit int, nonce string, presences bool) (err error) {
	if len(guildIDs) != 1 {
		return fmt.Errorf("request guild members accepts exactly one guild ID, got %d", len(guildIDs))
	}
	return s.RequestGuildMembersList(guildIDs[0], userIDs, limit, nonce, presences)
}

// GatewayWriteStruct allows for sending raw gateway structs over the gateway.
func (s *Session) GatewayWriteStruct(data interface{}) (err error) {
	return s.writeGatewayCurrent(data)
}

func (s *Session) requestGuildMembers(data requestGuildMembersData) (err error) {
	s.log(LogInformational, "called")

	if err = s.validateGuildMembersRequest(data); err != nil {
		return err
	}

	var reservation time.Time
	if requestsAllGuildMembers(data) {
		reservation, err = s.reserveAllGuildMembersRequest(data.GuildID, time.Now())
		if err != nil {
			return err
		}
	}

	err = s.writeGatewayCurrent(requestGuildMembersOp{8, data})
	if err != nil && !reservation.IsZero() {
		s.releaseAllGuildMembersRequest(data.GuildID, reservation)
	}

	return
}

func (s *Session) validateGuildMembersRequest(data requestGuildMembersData) error {
	if data.GuildID == "" {
		return errors.New("guild ID is required")
	}
	if (data.Query == nil) == (data.UserIDs == nil) {
		return errors.New("exactly one of query or user IDs is required")
	}
	if data.Limit < 0 || data.Limit > 100 {
		return fmt.Errorf("member request limit must be between 0 and 100, got %d", data.Limit)
	}
	if len(data.Nonce) > 32 {
		return fmt.Errorf("member request nonce must not exceed 32 bytes, got %d", len(data.Nonce))
	}
	if data.UserIDs != nil {
		if len(*data.UserIDs) == 0 || len(*data.UserIDs) > 100 {
			return fmt.Errorf("member request must include between 1 and 100 user IDs, got %d", len(*data.UserIDs))
		}
		for _, userID := range *data.UserIDs {
			if userID == "" {
				return errors.New("member request user IDs must not be empty")
			}
		}
	}

	s.RLock()
	intents := s.Identify.Intents
	s.RUnlock()
	if data.Presences && intents&IntentGuildPresences == 0 {
		return errors.New("requesting member presences requires the GUILD_PRESENCES intent")
	}
	if requestsAllGuildMembers(data) && intents&IntentGuildMembers == 0 {
		return errors.New("requesting all guild members requires the GUILD_MEMBERS intent")
	}
	return nil
}

func requestsAllGuildMembers(data requestGuildMembersData) bool {
	return data.Query != nil && *data.Query == "" && data.Limit == 0
}

func (s *Session) reserveAllGuildMembersRequest(guildID string, now time.Time) (time.Time, error) {
	const cooldown = 30 * time.Second

	s.guildMembersRequestMu.Lock()
	defer s.guildMembersRequestMu.Unlock()
	if last, ok := s.guildMembersRequests[guildID]; ok {
		if retryAfter := cooldown - now.Sub(last); retryAfter > 0 {
			return time.Time{}, &GuildMembersRequestRateLimitError{
				GuildID:    guildID,
				RetryAfter: retryAfter,
			}
		}
	}
	if s.guildMembersRequests == nil {
		s.guildMembersRequests = make(map[string]time.Time)
	}
	for id, requestedAt := range s.guildMembersRequests {
		if now.Sub(requestedAt) >= cooldown {
			delete(s.guildMembersRequests, id)
		}
	}
	s.guildMembersRequests[guildID] = now
	return now, nil
}

func (s *Session) releaseAllGuildMembersRequest(guildID string, reservation time.Time) {
	s.guildMembersRequestMu.Lock()
	if s.guildMembersRequests[guildID] == reservation {
		delete(s.guildMembersRequests, guildID)
	}
	s.guildMembersRequestMu.Unlock()
}

// onEvent is the "event handler" for all messages received on the
// Discord Gateway API websocket connection.
//
// If you use the AddHandler() function to register a handler for a
// specific event this function will pass the event along to that handler.
//
// If you use the AddHandler() function to register a handler for the
// "OnEvent" event then all events will be passed to that handler.
func (s *Session) onEvent(messageType int, message []byte) (*Event, error) {

	var err error
	var reader io.Reader
	reader = bytes.NewBuffer(message)

	// If this is a compressed message, uncompress it.
	if messageType == websocket.BinaryMessage {

		z, err2 := zlib.NewReader(reader)
		if err2 != nil {
			s.log(LogError, "error uncompressing websocket message, %s", err2)
			return nil, err2
		}

		defer func() {
			err3 := z.Close()
			if err3 != nil {
				s.log(LogWarning, "error closing zlib, %s", err3)
			}
		}()

		reader = z
	}

	// Decode the event into an Event struct.
	var e *Event
	decoder := json.NewDecoder(reader)
	if err = decoder.Decode(&e); err != nil {
		s.log(LogError, "error decoding websocket message, %s", err)
		return e, err
	}

	s.log(LogDebug, "Op: %d, Seq: %d, Type: %s, Data: %s", e.Operation, e.Sequence, e.Type, redactJSON(e.RawData))

	// Ping request.
	// Must respond with a heartbeat packet within 5 seconds
	if e.Operation == 1 {
		s.log(LogInformational, "sending heartbeat in response to Op1")
		err = s.writeGatewayPriorityCurrent(heartbeatOp{1, atomic.LoadInt64(s.sequence)})
		if err != nil {
			s.log(LogError, "error sending heartbeat in response to Op1")
			return e, err
		}

		return e, nil
	}

	// Reconnect
	// The generation reader owns the connection transition. Keeping onEvent
	// side-effect free here also makes READY-before reconnect packets safe while
	// OpenWithContext is waiting for the handshake result.
	if e.Operation == 7 {
		s.log(LogInformational, "gateway requested reconnect")
		return e, nil
	}

	// Invalid Session
	// Discord tells us whether the existing session may be resumed. A new
	// Identify must never be sent over the already-authenticated connection.
	if e.Operation == 9 {
		var resumable bool
		if err = json.Unmarshal(e.RawData, &resumable); err != nil {
			return e, fmt.Errorf("error unmarshalling invalid session payload: %w", err)
		}
		if !resumable {
			s.invalidateGatewaySession()
		}
		s.log(LogInformational, "gateway session invalidated; resumable=%t", resumable)
		return e, nil
	}

	if e.Operation == 10 {
		// Op10 is handled by Open()
		return e, nil
	}

	if e.Operation == 11 {
		s.Lock()
		s.LastHeartbeatAck = time.Now().UTC()
		s.Unlock()
		s.log(LogDebug, "got heartbeat ACK")
		return e, nil
	}

	// Do not try to Dispatch a non-Dispatch Message
	if e.Operation != 0 {
		// But we probably should be doing something with them.
		// TEMP
		s.log(LogWarning, "unknown Op: %d, Seq: %d, Type: %s, DataLength: %d", e.Operation, e.Sequence, e.Type, len(e.RawData))
		return e, nil
	}

	// Store the message sequence
	atomic.StoreInt64(s.sequence, e.Sequence)

	// Map event to registered event handlers and pass it along to any registered handlers.
	if eh, ok := registeredInterfaceProviders[e.Type]; ok {
		e.Struct = eh.New()

		// Attempt to unmarshal our event.
		if err = json.Unmarshal(e.RawData, e.Struct); err != nil {
			s.log(LogError, "error unmarshalling %s event, %s", e.Type, err)
		}

		// Send event to any registered event handlers for it's type.
		// Because the above doesn't cancel this, in case of an error
		// the struct could be partially populated or at default values.
		// However, most errors are due to a single field and I feel
		// it's better to pass along what we received than nothing at all.
		// TODO: Think about that decision :)
		// Either way, READY events must fire, even with errors.
		s.handleEvent(e.Type, e.Struct)
	} else {
		s.log(LogDebug, "unknown event: Op: %d, Seq: %d, Type: %s, Data: %s", e.Operation, e.Sequence, e.Type, redactJSON(e.RawData))
	}

	// For legacy reasons, we send the raw event also, this could be useful for handling unknown events.
	s.handleEvent(eventEventType, e)

	return e, nil
}

func (s *Session) invalidateGatewaySession() {
	s.gatewaySessionMu.Lock()
	s.sessionID = ""
	s.resumeGatewayURL = ""
	s.gatewaySessionMu.Unlock()
	atomic.StoreInt64(s.sequence, 0)
}

func invalidSessionBackoff() time.Duration {
	const (
		minimum = time.Second
		spread  = 4 * time.Second
	)
	offset, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(spread)))
	if err != nil {
		return minimum + spread/2
	}
	return minimum + time.Duration(offset.Int64())
}

// ------------------------------------------------------------------------------------------------
// Code related to voice connections that initiate over the data websocket
// ------------------------------------------------------------------------------------------------

type voiceChannelJoinData struct {
	GuildID   *string `json:"guild_id"`
	ChannelID *string `json:"channel_id"`
	SelfMute  bool    `json:"self_mute"`
	SelfDeaf  bool    `json:"self_deaf"`
}

type voiceChannelJoinOp struct {
	Op   int                  `json:"op"`
	Data voiceChannelJoinData `json:"d"`
}

// ChannelVoiceJoin joins the session user to a voice channel.
//
//	gID     : Guild ID of the channel to join.
//	cID     : Channel ID of the channel to join.
//	mute    : If true, you will be set to muted upon joining.
//	deaf    : If true, you will be set to deafened upon joining.
func (s *Session) ChannelVoiceJoin(gID, cID string, mute, deaf bool) (voice *VoiceConnection, err error) {

	s.log(LogInformational, "called")

	var created bool
	s.Lock()
	if s.VoiceConnections == nil {
		s.VoiceConnections = make(map[string]*VoiceConnection)
	}
	voice = s.VoiceConnections[gID]
	if voice == nil {
		voice = &VoiceConnection{}
		s.VoiceConnections[gID] = voice
		created = true
	}
	s.Unlock()

	voice.Lock()
	voice.GuildID = gID
	voice.ChannelID = cID
	voice.deaf = deaf
	voice.mute = mute
	voice.session = s
	voice.Unlock()

	err = s.ChannelVoiceJoinManual(gID, cID, mute, deaf)
	if err != nil {
		if created {
			s.Lock()
			if s.VoiceConnections[gID] == voice {
				delete(s.VoiceConnections, gID)
			}
			s.Unlock()
		}
		return
	}

	// doesn't exactly work perfect yet.. TODO
	err = voice.waitUntilConnected()
	if err != nil {
		s.log(LogWarning, "error waiting for voice to connect, %s", err)
		voice.Close()
		return
	}

	return
}

// ChannelVoiceJoinManual initiates a voice session to a voice channel, but does not complete it.
//
// This should only be used when the VoiceServerUpdate will be intercepted and used elsewhere.
//
//	gID     : Guild ID of the channel to join.
//	cID     : Channel ID of the channel to join, leave empty to disconnect.
//	mute    : If true, you will be set to muted upon joining.
//	deaf    : If true, you will be set to deafened upon joining.
func (s *Session) ChannelVoiceJoinManual(gID, cID string, mute, deaf bool) (err error) {

	s.log(LogInformational, "called")
	if gID == "" {
		return errors.New("guild ID is required")
	}

	var channelID *string
	if cID == "" {
		channelID = nil
	} else {
		channelID = &cID
	}

	// Send the request to Discord that we want to join the voice channel
	data := voiceChannelJoinOp{4, voiceChannelJoinData{&gID, channelID, mute, deaf}}
	return s.GatewayWriteStruct(data)
}

// onVoiceStateUpdate handles Voice State Update events on the data websocket.
func (s *Session) onVoiceStateUpdate(st *VoiceStateUpdate) {

	// If we don't have a connection for the channel, don't bother
	if st.ChannelID == "" {
		return
	}

	// Check if we have a voice connection to update
	s.RLock()
	voice, exists := s.VoiceConnections[st.GuildID]
	s.RUnlock()
	if !exists {
		return
	}

	s.RLock()
	state := s.State
	s.RUnlock()
	if state == nil {
		return
	}
	state.RLock()
	currentUser := state.User
	state.RUnlock()

	// We only care about events that are about us.
	if currentUser == nil || currentUser.ID != st.UserID {
		return
	}

	// Store the SessionID for later use.
	voice.Lock()
	voice.UserID = st.UserID
	voice.sessionID = st.SessionID
	voice.ChannelID = st.ChannelID
	voice.Unlock()
}

// onVoiceServerUpdate handles the Voice Server Update data websocket event.
//
// This is also fired if the Guild's voice region changes while connected
// to a voice channel.  In that case, need to re-establish connection to
// the new region endpoint.
func (s *Session) onVoiceServerUpdate(st *VoiceServerUpdate) {

	s.log(LogInformational, "called")

	s.RLock()
	voice, exists := s.VoiceConnections[st.GuildID]
	s.RUnlock()

	// If no VoiceConnection exists, just skip this
	if !exists {
		return
	}

	// If currently connected to voice ws/udp, then disconnect.
	// Has no effect if not connected.
	voice.Close()

	// Store values for later use
	voice.Lock()
	voice.token = st.Token
	voice.endpoint = st.Endpoint
	voice.GuildID = st.GuildID
	voice.Unlock()

	// Open a connection to the voice server
	err := voice.open()
	if err != nil {
		s.log(LogError, "onVoiceServerUpdate voice.open, %s", err)
	}
}

type identifyOp struct {
	Op   int      `json:"op"`
	Data Identify `json:"d"`
}

// identify sends the identify packet to one exact Gateway generation.
func (s *Session) identify(conn *gatewayConnectionLifecycle) error {
	s.log(LogDebug, "called")
	if conn == nil || conn.ws == nil {
		return ErrWSNotFound
	}

	// TODO: This is a temporary block of code to help
	// maintain backwards compatibility
	s.Lock()
	if !s.Compress {
		s.Identify.Compress = false
	}

	// TODO: This is a temporary block of code to help
	// maintain backwards compatibility
	if s.Token != "" && s.Identify.Token == "" {
		s.Identify.Token = s.Token
	}

	// TODO: Below block should be refactored so ShardID and ShardCount
	// can be deprecated and their usage moved to the Session.Identify
	// struct
	if s.ShardCount > 1 {

		if s.ShardID >= s.ShardCount {
			s.Unlock()
			return ErrWSShardBounds
		}

		s.Identify.Shard = &[2]int{s.ShardID, s.ShardCount}
	}

	// Send Identify packet to Discord
	op := identifyOp{2, s.Identify}
	s.Unlock()
	s.log(LogDebug, "sending identify packet with intents=%d shard=%v", op.Data.Intents, op.Data.Shard)
	return s.writeGatewayGeneration(conn, op)
}

// Close closes the Gateway and every active Voice connection. It is
// idempotent: an already closed Session does not emit another Disconnect
// event.
func (s *Session) Close() error {
	err := s.CloseWithCode(websocket.CloseNormalClosure)
	s.closeVoiceConnections()
	s.voiceRoutineWG.Wait()
	return err
}

// CloseWithCode closes a websocket using the provided closeCode and stops all
// listening/heartbeat goroutines.
func (s *Session) CloseWithCode(closeCode int) (err error) {
	err = s.closeGateway(closeCode, true)
	if closeCode == websocket.CloseNormalClosure || closeCode == websocket.CloseGoingAway {
		s.invalidateGatewaySession()
	}
	return err
}

func (s *Session) closeGateway(closeCode int, sendCloseFrame bool) (err error) {
	s.log(LogInformational, "called")
	s.gatewayLifecycleMu.Lock()
	lifecycle := s.gatewayLifecycle
	conn := s.gatewayConnection
	if lifecycle != nil {
		s.gatewayLifecycle = nil
		s.gatewayReconnectRunning = false
		s.gatewayState = gatewayStateClosing
		lifecycle.cancel()
	}
	s.gatewayLifecycleMu.Unlock()

	if conn != nil {
		s.finishGatewayGeneration(conn, gatewayAttemptResult{
			err:    context.Canceled,
			action: GatewayCloseRecoveryStop,
		}, closeCode, sendCloseFrame)
		s.gatewayLifecycleMu.Lock()
		if s.gatewayConnection == nil {
			s.gatewayState = gatewayStateClosed
		}
		s.gatewayLifecycleMu.Unlock()
		return nil
	}

	// Compatibility cleanup for Sessions assembled manually by callers or
	// tests without a managed Gateway generation.
	s.Lock()
	hadConnection := s.DataReady || s.listening != nil || s.wsConn != nil
	listening := s.listening
	wsConn := s.wsConn
	s.DataReady = false
	s.listening = nil
	s.wsConn = nil
	s.Unlock()

	if listening != nil {
		close(listening)
	}
	if wsConn != nil {
		if sendCloseFrame {
			s.wsMutex.Lock()
			err = wsConn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(closeCode, ""),
			)
			s.wsMutex.Unlock()
		}
		if closeErr := wsConn.Close(); err == nil {
			err = closeErr
		}
	}
	if hadConnection {
		s.enqueueGatewayEvent(disconnectEventType, &Disconnect{})
		s.drainGatewayEvents()
	}
	s.gatewayLifecycleMu.Lock()
	if s.gatewayConnection == nil {
		s.gatewayState = gatewayStateClosed
	}
	s.gatewayLifecycleMu.Unlock()
	return err
}

func (s *Session) closeVoiceConnections() {
	s.Lock()
	voices := make([]*VoiceConnection, 0, len(s.VoiceConnections))
	for _, voice := range s.VoiceConnections {
		if voice != nil {
			voices = append(voices, voice)
		}
	}
	if s.VoiceConnections != nil {
		s.VoiceConnections = make(map[string]*VoiceConnection)
	}
	s.Unlock()

	for _, voice := range voices {
		voice.Close()
	}
}
