// Discordgo - Discord bindings for Go
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
	"strings"
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

// OpenWithContext creates a websocket connection to Discord with a context.
func (s *Session) OpenWithContext(ctx context.Context) error {
	s.log(LogInformational, "called")

	var err error

	token := s.Identify.Token
	if token == "" {
		token = s.Token
	}
	if !strings.HasPrefix(token, "Bot ") || strings.TrimSpace(strings.TrimPrefix(token, "Bot ")) == "" {
		return ErrWSInvalidToken
	}

	// Prevent Open or other major Session functions from
	// being called while Open is still running.
	s.Lock()
	defer s.Unlock()

	// If the websock is already open, bail out here.
	if s.wsConn != nil {
		return ErrWSAlreadyOpen
	}

	// Get the initial gateway. Discord's READY event can provide a more
	// specific endpoint for subsequent resume attempts.
	if s.gateway == "" {
		s.gateway, err = s.Gateway()
		if err != nil {
			return err
		}
	}
	connectGateway, usingResumeGateway, err := s.gatewayConnectURL()
	if err != nil {
		return err
	}

	// Connect to the Gateway
	s.log(LogInformational, "connecting to gateway %s", connectGateway)
	header := http.Header{}
	header.Add("accept-encoding", "zlib")

	// Use DialContext if available, otherwise fallback (though gorilla/websocket has it since forever)
	s.wsConn, _, err = s.Dialer.DialContext(ctx, connectGateway, header)
	if err != nil {
		s.log(LogError, "error connecting to gateway %s, %s", connectGateway, err)
		if usingResumeGateway {
			s.gatewaySessionMu.Lock()
			s.resumeGatewayURL = ""
			s.gatewaySessionMu.Unlock()
		} else {
			s.gateway = "" // clear cached initial gateway
		}
		s.wsConn = nil // Just to be safe.
		return err
	}

	s.wsConn.SetCloseHandler(func(code int, text string) error {
		return nil
	})

	defer func() {
		// because of this, all code below must set err to the error
		// when exiting with an error :)  Maybe someone has a better
		// way :)
		if err != nil {
			s.wsConn.Close()
			s.wsConn = nil
		}
	}()

	// The first response from Discord should be an Op 10 (Hello) Packet.
	// When processed by onEvent the heartbeat goroutine will be started.
	mt, m, err := s.wsConn.ReadMessage()
	if err != nil {
		return err
	}
	e, err := s.onEvent(mt, m)
	if err != nil {
		return err
	}
	if e.Operation != 10 {
		err = fmt.Errorf("expecting Op 10, got Op %d instead", e.Operation)
		return err
	}
	s.log(LogInformational, "Op 10 Hello Packet received from Discord")
	s.LastHeartbeatAck = time.Now().UTC()
	var h helloOp
	if err = json.Unmarshal(e.RawData, &h); err != nil {
		err = fmt.Errorf("error unmarshalling helloOp, %s", err)
		return err
	}

	// Now we send either an Op 2 Identity if this is a brand new
	// connection or Op 6 Resume if we are resuming an existing connection.
	sequence := atomic.LoadInt64(s.sequence)
	s.gatewaySessionMu.RLock()
	sessionID := s.sessionID
	s.gatewaySessionMu.RUnlock()
	if sessionID == "" {

		// Send Op 2 Identity Packet
		err = s.identify()
		if err != nil {
			err = fmt.Errorf("error sending identify packet to gateway, %s, %s", connectGateway, err)
			return err
		}

	} else {

		// Send Op 6 Resume Packet
		p := resumePacket{}
		p.Op = 6
		p.Data.Token = token
		p.Data.SessionID = sessionID
		p.Data.Sequence = sequence

		s.log(LogInformational, "sending resume packet to gateway")
		s.wsMutex.Lock()
		err = s.wsConn.WriteJSON(p)
		s.wsMutex.Unlock()
		if err != nil {
			err = fmt.Errorf("error sending gateway resume packet, %s, %s", connectGateway, err)
			return err
		}

	}

	// A basic state is a hard requirement for Voice.
	// We create it here so the below READY/RESUMED packet can populate
	// the state :)
	// XXX: Move to New() func?
	if s.State == nil {
		state := NewState()
		state.TrackChannels = false
		state.TrackEmojis = false
		state.TrackMembers = false
		state.TrackRoles = false
		state.TrackVoice = false
		s.State = state
	}

	// Now Discord should send us a READY or RESUMED packet.
	mt, m, err = s.wsConn.ReadMessage()
	if err != nil {
		return err
	}
	e, err = s.onEvent(mt, m)
	if err != nil {
		return err
	}
	if e.Type != `READY` && e.Type != `RESUMED` {
		// This is not fatal, but it does not follow their API documentation.
		s.log(LogWarning, "expected READY/RESUMED, got operation %d type %s", e.Operation, e.Type)
	}
	s.log(LogInformational, "first gateway packet operation=%d sequence=%d type=%s", e.Operation, e.Sequence, e.Type)

	s.log(LogInformational, "We are now connected to Discord, emitting connect event")
	s.handleEvent(connectEventType, &Connect{})

	// A VoiceConnections map is a hard requirement for Voice.
	// XXX: can this be moved to when opening a voice connection?
	if s.VoiceConnections == nil {
		s.log(LogInformational, "creating new VoiceConnections map")
		s.VoiceConnections = make(map[string]*VoiceConnection)
	}

	// Create listening chan outside of listen, as it needs to happen inside the
	// mutex lock and needs to exist before calling heartbeat and listen
	// go rountines.
	s.listening = make(chan interface{})

	// Start sending heartbeats and reading messages from Discord.
	go s.heartbeat(s.wsConn, s.listening, h.HeartbeatInterval)
	go s.listen(s.wsConn, s.listening)

	s.log(LogInformational, "exiting")
	return nil
}

func (s *Session) gatewayConnectURL() (connectURL string, usingResume bool, err error) {
	baseURL := s.gateway
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
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return "", false, fmt.Errorf("invalid gateway URL scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", false, errors.New("gateway URL must include a host")
	}
	query := parsed.Query()
	query.Set("v", APIVersion)
	query.Set("encoding", "json")
	parsed.RawQuery = query.Encode()
	return parsed.String(), usingResume, nil
}

// listen polls the websocket connection for events, it will stop when the
// listening channel is closed, or an error occurs.
func (s *Session) listen(wsConn *websocket.Conn, listening <-chan interface{}) {

	s.log(LogInformational, "called")

	for {

		messageType, message, err := wsConn.ReadMessage()

		if err != nil {

			// Detect if we have been closed manually. If a Close() has already
			// happened, the websocket we are listening on will be different to
			// the current session.
			s.RLock()
			sameConnection := s.wsConn == wsConn
			s.RUnlock()

			if sameConnection {

				s.log(LogWarning, "error reading from gateway %s websocket, %s", s.gateway, err)
				// There has been an error reading, close the websocket so that
				// OnDisconnect event is emitted.
				err := s.Close()
				if err != nil {
					s.log(LogWarning, "error closing session connection, %s", err)
				}

				s.log(LogInformational, "calling reconnect() now")
				s.reconnect()
			}

			return
		}

		select {

		case <-listening:
			return

		default:
			s.onEvent(messageType, message)

		}
	}
}

type heartbeatOp struct {
	Op   int   `json:"op"`
	Data int64 `json:"d"`
}

type helloOp struct {
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
}

// FailedHeartbeatAcks is the number of heartbeat intervals to wait until forcing a connection restart.
const FailedHeartbeatAcks = 5

// HeartbeatLatency returns the latency between heartbeat acknowledgement and heartbeat send.
func (s *Session) HeartbeatLatency() time.Duration {

	return s.LastHeartbeatAck.Sub(s.LastHeartbeatSent)

}

// heartbeat sends regular heartbeats to Discord so it knows the client
// is still connected.  If you do not send these heartbeats Discord will
// disconnect the websocket connection after a few seconds.
func (s *Session) heartbeat(wsConn *websocket.Conn, listening <-chan interface{}, heartbeatInterval time.Duration) {

	s.log(LogInformational, "called")

	if listening == nil || wsConn == nil {
		return
	}

	var err error
	ticker := time.NewTicker(heartbeatInterval * time.Millisecond)
	defer ticker.Stop()

	for {
		s.RLock()
		last := s.LastHeartbeatAck
		s.RUnlock()
		sequence := atomic.LoadInt64(s.sequence)
		s.log(LogDebug, "sending gateway websocket heartbeat seq %d", sequence)
		s.wsMutex.Lock()
		s.LastHeartbeatSent = time.Now().UTC()
		err = wsConn.WriteJSON(heartbeatOp{1, sequence})
		s.wsMutex.Unlock()
		if err != nil || time.Now().UTC().Sub(last) > (heartbeatInterval*time.Millisecond*time.Duration(FailedHeartbeatAcks)) {
			if err != nil {
				s.log(LogError, "error sending heartbeat to gateway %s, %s", s.gateway, err)
			} else {
				s.log(LogError, "haven't gotten a heartbeat ACK in %v, triggering a reconnection", time.Now().UTC().Sub(last))
			}
			s.Close()
			s.reconnect()
			return
		}
		s.Lock()
		s.DataReady = true
		s.Unlock()

		select {
		case <-ticker.C:
			// continue loop and send heartbeat
		case <-listening:
			return
		}
	}
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
	// The comment does say "untouched by discordgo", but we might need to lie a bit here.
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

	s.RLock()
	defer s.RUnlock()
	if s.wsConn == nil {
		return ErrWSNotFound
	}

	s.wsMutex.Lock()
	err = s.wsConn.WriteJSON(updateStatusOp{3, usd})
	s.wsMutex.Unlock()

	return
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
	s.RLock()
	defer s.RUnlock()
	if s.wsConn == nil {
		return ErrWSNotFound
	}

	s.wsMutex.Lock()
	err = s.wsConn.WriteJSON(data)
	s.wsMutex.Unlock()

	return err
}

func (s *Session) requestGuildMembers(data requestGuildMembersData) (err error) {
	s.log(LogInformational, "called")

	if err = s.validateGuildMembersRequest(data); err != nil {
		return err
	}

	s.RLock()
	defer s.RUnlock()
	if s.wsConn == nil {
		return ErrWSNotFound
	}

	var reservation time.Time
	if requestsAllGuildMembers(data) {
		reservation, err = s.reserveAllGuildMembersRequest(data.GuildID, time.Now())
		if err != nil {
			return err
		}
	}

	s.wsMutex.Lock()
	err = s.wsConn.WriteJSON(requestGuildMembersOp{8, data})
	s.wsMutex.Unlock()
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
		s.wsMutex.Lock()
		err = s.wsConn.WriteJSON(heartbeatOp{1, atomic.LoadInt64(s.sequence)})
		s.wsMutex.Unlock()
		if err != nil {
			s.log(LogError, "error sending heartbeat in response to Op1")
			return e, err
		}

		return e, nil
	}

	// Reconnect
	// Must immediately disconnect from gateway and reconnect to new gateway.
	if e.Operation == 7 {
		s.log(LogInformational, "Closing and reconnecting in response to Op7")
		s.CloseWithCode(websocket.CloseServiceRestart)
		s.reconnect()
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
		go s.reconnectInvalidGatewaySession(resumable)
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

func (s *Session) reconnectInvalidGatewaySession(resumable bool) {
	if err := s.CloseWithCode(websocket.CloseServiceRestart); err != nil {
		s.log(LogWarning, "error closing invalid gateway session, %s", err)
	}

	s.RLock()
	shouldReconnect := s.ShouldReconnectOnError
	s.RUnlock()
	if !shouldReconnect {
		return
	}
	if !resumable {
		delay := invalidSessionBackoff()
		s.log(LogInformational, "waiting %s before identifying a new gateway session", delay)
		timer := time.NewTimer(delay)
		<-timer.C
	}
	s.reconnect()
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

	s.RLock()
	voice = s.VoiceConnections[gID]
	s.RUnlock()

	if voice == nil {
		voice = &VoiceConnection{}
		s.Lock()
		s.VoiceConnections[gID] = voice
		s.Unlock()
	}

	voice.Lock()
	voice.GuildID = gID
	voice.ChannelID = cID
	voice.deaf = deaf
	voice.mute = mute
	voice.session = s
	voice.Unlock()

	err = s.ChannelVoiceJoinManual(gID, cID, mute, deaf)
	if err != nil {
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

	var channelID *string
	if cID == "" {
		channelID = nil
	} else {
		channelID = &cID
	}

	// Send the request to Discord that we want to join the voice channel
	data := voiceChannelJoinOp{4, voiceChannelJoinData{&gID, channelID, mute, deaf}}
	s.wsMutex.Lock()
	err = s.wsConn.WriteJSON(data)
	s.wsMutex.Unlock()
	return
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

	// We only care about events that are about us.
	if s.State.User.ID != st.UserID {
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

// identify sends the identify packet to the gateway
func (s *Session) identify() error {
	s.log(LogDebug, "called")

	// TODO: This is a temporary block of code to help
	// maintain backwards compatibility
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
			return ErrWSShardBounds
		}

		s.Identify.Shard = &[2]int{s.ShardID, s.ShardCount}
	}

	// Send Identify packet to Discord
	op := identifyOp{2, s.Identify}
	s.log(LogDebug, "sending identify packet with intents=%d shard=%v", s.Identify.Intents, s.Identify.Shard)
	s.wsMutex.Lock()
	err := s.wsConn.WriteJSON(op)
	s.wsMutex.Unlock()

	return err
}

func (s *Session) reconnect() {

	s.log(LogInformational, "called")

	var err error

	if s.ShouldReconnectOnError {

		wait := time.Duration(1)

		for {
			s.log(LogInformational, "trying to reconnect to gateway")

			err = s.Open()
			if err == nil {
				s.log(LogInformational, "successfully reconnected to gateway")

				// I'm not sure if this is actually needed.
				// if the gw reconnect works properly, voice should stay alive
				// However, there seems to be cases where something "weird"
				// happens.  So we're doing this for now just to improve
				// stability in those edge cases.
				if s.ShouldReconnectVoiceOnSessionError {
					s.RLock()
					defer s.RUnlock()
					for _, v := range s.VoiceConnections {

						s.log(LogInformational, "reconnecting voice connection to guild %s", v.GuildID)
						go v.reconnect()

						// This is here just to prevent violently spamming the
						// voice reconnects
						time.Sleep(1 * time.Second)
					}
				}
				return
			}

			// Certain race conditions can call reconnect() twice. If this happens, we
			// just break out of the reconnect loop
			if err == ErrWSAlreadyOpen {
				s.log(LogInformational, "Websocket already exists, no need to reconnect")
				return
			}

			s.log(LogError, "error reconnecting to gateway, %s", err)

			<-time.After(wait * time.Second)
			wait *= 2
			if wait > 600 {
				wait = 600
			}
		}
	}
}

// Close closes a websocket and stops all listening/heartbeat goroutines.
// TODO: Add support for Voice WS/UDP
func (s *Session) Close() error {
	return s.CloseWithCode(websocket.CloseNormalClosure)
}

// CloseWithCode closes a websocket using the provided closeCode and stops all
// listening/heartbeat goroutines.
// TODO: Add support for Voice WS/UDP connections
func (s *Session) CloseWithCode(closeCode int) (err error) {

	s.log(LogInformational, "called")
	s.Lock()

	s.DataReady = false

	if s.listening != nil {
		s.log(LogInformational, "closing listening channel")
		close(s.listening)
		s.listening = nil
	}

	// TODO: Close all active Voice Connections too
	// this should force stop any reconnecting voice channels too

	if s.wsConn != nil {

		s.log(LogInformational, "sending close frame")
		// To cleanly close a connection, a client should send a close
		// frame and wait for the server to close the connection.
		s.wsMutex.Lock()
		err := s.wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(closeCode, ""))
		s.wsMutex.Unlock()
		if err != nil {
			s.log(LogInformational, "error closing websocket, %s", err)
		}

		// TODO: Wait for Discord to actually close the connection.
		time.Sleep(1 * time.Second)

		s.log(LogInformational, "closing gateway websocket")
		err = s.wsConn.Close()
		if err != nil {
			s.log(LogInformational, "error closing websocket, %s", err)
		}

		s.wsConn = nil
	}

	s.Unlock()

	s.log(LogInformational, "emit disconnect event")
	s.handleEvent(disconnectEventType, &Disconnect{})

	return
}
