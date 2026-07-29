// Discordgo - Discord bindings for Go
// Available at https://github.com/bwmarrin/discordgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains code related to Discord voice suppport

package dgo

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/chacha20poly1305"
)

// ------------------------------------------------------------------------------------------------
// Code related to both VoiceConnection Websocket and UDP connections.
// ------------------------------------------------------------------------------------------------

// A VoiceConnection struct holds all the data and functions related to a Discord Voice Connection.
type VoiceConnection struct {
	sync.RWMutex

	Debug bool // If true, print extra logging -- DEPRECATED
	// LogLevel filters the legacy package logger used only by standalone voice
	// connections without an owning Session.
	// Deprecated: configure the Session logger's slog.Handler level instead.
	LogLevel     int
	Ready        bool // If true, voice is ready to send/receive audio
	UserID       string
	GuildID      string
	ChannelID    string
	deaf         bool
	mute         bool
	speaking     bool
	reconnecting bool // If true, voice connection is trying to reconnect

	OpusSend chan []byte  // Chan for sending opus audio
	OpusRecv chan *Packet // Chan for receiving opus audio

	wsConn  *websocket.Conn
	wsMutex sync.Mutex
	udpConn *net.UDPConn
	session *Session

	sessionID string
	token     string
	endpoint  string

	// Used to send a close signal to goroutines
	close chan struct{}

	aead cipher.AEAD

	dave *DAVESession

	ssrcToUserID map[uint32]string

	seqAck int

	LastHeartbeatSent time.Time
	LastHeartbeatAck  time.Time
	HeartbeatLatency  time.Duration
	ConnectedUsers    []string

	lastHeartbeatNonce int64
	awaitingHeartbeat  bool
	lastDAVESequence   uint16
	hasDAVESequence    bool

	op4 voiceOP4
	op2 voiceOP2
	op8 voiceOP8

	voiceSpeakingUpdateHandlers []VoiceSpeakingUpdateHandler
	voiceClientsConnectHandlers []VoiceClientsConnectHandler
}

// VoiceSpeakingUpdateHandler type provides a function definition for the
// VoiceSpeakingUpdate event
type VoiceSpeakingUpdateHandler func(vc *VoiceConnection, vs *VoiceSpeakingUpdate)

// VoiceClientsConnectHandler handles VoiceClientsConnect events.
type VoiceClientsConnectHandler func(vc *VoiceConnection, event *VoiceClientsConnect)

var (
	// ErrVoiceConnectionClosed is returned when an operation requires an active
	// Voice Gateway connection but the connection has already been closed.
	ErrVoiceConnectionClosed = errors.New("voice connection is closed")
	// ErrVoiceSessionUnavailable is returned when a VoiceConnection is not
	// associated with a Discord session.
	ErrVoiceSessionUnavailable = errors.New("voice connection has no session")
)

func (v *VoiceConnection) writeVoiceJSON(data interface{}) (*websocket.Conn, error) {
	v.RLock()
	wsConn := v.wsConn
	v.RUnlock()
	if wsConn == nil {
		return nil, ErrVoiceConnectionClosed
	}

	v.wsMutex.Lock()
	err := wsConn.WriteJSON(data)
	v.wsMutex.Unlock()
	return wsConn, err
}

func (v *VoiceConnection) writeVoiceMessage(messageType int, data []byte) (*websocket.Conn, error) {
	v.RLock()
	wsConn := v.wsConn
	v.RUnlock()
	if wsConn == nil {
		return nil, ErrVoiceConnectionClosed
	}

	v.wsMutex.Lock()
	err := wsConn.WriteMessage(messageType, data)
	v.wsMutex.Unlock()
	return wsConn, err
}

// Speaking sends a speaking notification to Discord over the voice websocket.
// This must be sent as true prior to sending audio and should be set to false
// once finished sending audio.
// b : Send true if speaking, false if not.
func (v *VoiceConnection) Speaking(b bool) (err error) {

	v.log(LogDebug, "called (%t)", b)

	type voiceSpeakingData struct {
		Speaking bool `json:"speaking"`
		Delay    int  `json:"delay"`
	}

	type voiceSpeakingOp struct {
		Op   int               `json:"op"` // Always 5
		Data voiceSpeakingData `json:"d"`
	}

	data := voiceSpeakingOp{5, voiceSpeakingData{b, 0}}
	wsConn, err := v.writeVoiceJSON(data)

	v.Lock()
	if err != nil {
		v.speaking = false
		v.Unlock()
		v.log(LogError, "Speaking() write json error, %s", err)
		return err
	}
	if v.wsConn != wsConn {
		v.speaking = false
		v.Unlock()
		return ErrVoiceConnectionClosed
	}
	v.speaking = b
	v.Unlock()
	return nil
}

// ChangeChannel sends Discord a request to change channels within a Guild
// !!! NOTE !!! This function may be removed in favour of just using ChannelVoiceJoin
func (v *VoiceConnection) ChangeChannel(channelID string, mute, deaf bool) (err error) {

	v.log(LogInformational, "called")

	v.RLock()
	session := v.session
	guildID := v.GuildID
	sessionID := v.sessionID
	voiceWS := v.wsConn
	v.RUnlock()
	if session == nil {
		return ErrVoiceSessionUnavailable
	}
	if sessionID == "" || voiceWS == nil {
		return ErrVoiceConnectionClosed
	}

	data := voiceChannelJoinOp{4, voiceChannelJoinData{&guildID, &channelID, mute, deaf}}
	if err = session.GatewayWriteStruct(data); err != nil {
		return err
	}

	v.Lock()
	if v.session != session || v.sessionID == "" || v.wsConn != voiceWS {
		v.Unlock()
		return ErrVoiceConnectionClosed
	}
	v.ChannelID = channelID
	v.deaf = deaf
	v.mute = mute
	v.speaking = false
	v.Unlock()

	return nil
}

// Disconnect disconnects from this voice channel and closes the websocket
// and udp connections to Discord.
func (v *VoiceConnection) Disconnect() (err error) {

	v.RLock()
	session := v.session
	sessionID := v.sessionID
	guildID := v.GuildID
	v.RUnlock()

	if session == nil {
		v.Close()
		return ErrVoiceSessionUnavailable
	}

	// Send an OP4 with a nil channel to disconnect.
	if sessionID != "" {
		data := voiceChannelJoinOp{4, voiceChannelJoinData{&guildID, nil, true, true}}
		err = session.GatewayWriteStruct(data)

		v.Lock()
		if v.sessionID == sessionID {
			v.sessionID = ""
		}
		v.Unlock()
	}

	v.Close()

	v.log(LogInformational, "Deleting VoiceConnection %s", guildID)

	session.Lock()
	if session.VoiceConnections[guildID] == v {
		delete(session.VoiceConnections, guildID)
	}
	session.Unlock()

	return
}

// Close closes the voice ws and udp connections
func (v *VoiceConnection) Close() {

	v.log(LogInformational, "called")

	v.Lock()
	v.Ready = false
	v.speaking = false
	v.dave = nil
	v.hasDAVESequence = false
	v.lastDAVESequence = 0
	closeChannel := v.close
	v.close = nil
	udpConn := v.udpConn
	v.udpConn = nil
	wsConn := v.wsConn
	v.wsConn = nil
	v.Unlock()

	if closeChannel != nil {
		v.log(LogInformational, "closing voice connection signal")
		close(closeChannel)
	}

	if udpConn != nil {
		v.log(LogInformational, "closing udp")
		err := udpConn.Close()
		if err != nil {
			v.log(LogError, "error closing udp connection, %s", err)
		}
	}

	if wsConn != nil {
		v.log(LogInformational, "sending close frame")

		// To cleanly close a connection, a client should send a close
		// frame and wait for the server to close the connection.
		v.wsMutex.Lock()
		err := wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			v.log(LogError, "error closing websocket, %s", err)
		}

		// TODO: Wait for Discord to actually close the connection.
		time.Sleep(1 * time.Second)

		v.log(LogInformational, "closing websocket")
		err = wsConn.Close()
		v.wsMutex.Unlock()
		if err != nil {
			v.log(LogError, "error closing websocket, %s", err)
		}
	}
}

// AddHandler adds a Handler for VoiceSpeakingUpdate events.
func (v *VoiceConnection) AddHandler(h VoiceSpeakingUpdateHandler) {
	if h == nil {
		return
	}
	v.Lock()
	defer v.Unlock()

	v.voiceSpeakingUpdateHandlers = append(v.voiceSpeakingUpdateHandlers, h)
}

// AddClientsConnectHandler adds a handler for Voice Clients Connect events.
func (v *VoiceConnection) AddClientsConnectHandler(h VoiceClientsConnectHandler) {
	if h == nil {
		return
	}
	v.Lock()
	defer v.Unlock()

	v.voiceClientsConnectHandlers = append(v.voiceClientsConnectHandlers, h)
}

// VoiceSpeakingUpdate is a struct for a VoiceSpeakingUpdate event.
type VoiceSpeakingUpdate struct {
	UserID   string `json:"user_id"`
	SSRC     int    `json:"ssrc"`
	Speaking int    `json:"speaking"`
}

// VoiceClientsConnect is sent when one or more users join the voice channel.
type VoiceClientsConnect struct {
	UserIDs []string `json:"user_ids"`
}

// ------------------------------------------------------------------------------------------------
// Unexported Internal Functions Below.
// ------------------------------------------------------------------------------------------------

type voiceWebsocketMessage struct {
	Operation int             `json:"op"`
	RawData   json.RawMessage `json:"d"`
	Sequence  *int            `json:"seq"`
}

// A voiceOP4 stores the data for the voice operation 4 websocket event
// which provides us with the NaCl SecretBox encryption key
type voiceOP4 struct {
	SecretKey           []byte `json:"secret_key"`
	Mode                string `json:"mode"`
	DAVEProtocolVersion int    `json:"dave_protocol_version"`
}

// A voiceOP2 stores the data for the voice operation 2 websocket event
// which is sort of like the voice READY packet
type voiceOP2 struct {
	SSRC  uint32   `json:"ssrc"`
	Port  int      `json:"port"`
	Modes []string `json:"modes"`
	IP    string   `json:"ip"`
}

// A voiceOP8 stores the data for the voice operation 8 websocket event HELLO
type voiceOP8 struct {
	HeartbeatInterval int `json:"heartbeat_interval"`
}

// WaitUntilConnected waits for the Voice Connection to
// become ready, if it does not become ready it returns an err
func (v *VoiceConnection) waitUntilConnected() error {

	v.log(LogInformational, "called")

	i := 0
	for {
		v.RLock()
		ready := v.Ready
		v.RUnlock()
		if ready {
			return nil
		}

		if i > 10 {
			return fmt.Errorf("timeout waiting for voice")
		}

		time.Sleep(1 * time.Second)
		i++
	}
}

// Open opens a voice connection.  This should be called
// after VoiceChannelJoin is used and the data VOICE websocket events
// are captured.
func (v *VoiceConnection) open() (err error) {

	v.log(LogInformational, "called")

	v.Lock()
	defer v.Unlock()

	// Don't open a websocket if one is already open
	if v.wsConn != nil {
		v.log(LogWarning, "refusing to overwrite non-nil websocket")
		return
	}

	// TODO temp? loop to wait for the SessionID
	i := 0
	for {
		if v.sessionID != "" {
			break
		}

		if i > 20 { // only loop for up to 1 second total
			return fmt.Errorf("did not receive voice Session ID in time")
		}
		// Release the lock, so sessionID can be populated upon receiving a VoiceStateUpdate event.
		v.Unlock()
		time.Sleep(50 * time.Millisecond)
		i++
		v.Lock()
	}

	// Connect to VoiceConnection Websocket
	// modified by darui3018823
	// because Uncontrolled data used in network request (v.session.Dialer.Dial(vg, nil))
	allowedDomains := []string{
		".discord.media",          // Voice servers
		".discord.gg",             // Invite shortlinks
		".discordapp.com",         // Old domain
		".discord.com",            // Main domain
		".discordpartygames.com",  // Voice channels
		".discord-activities.com", // Voice channels
		".discordactivities.com",  // Voice channels
		".discordsays.com",        // Voice channels
	}

	endpointHost := v.endpoint
	if host, _, err := net.SplitHostPort(v.endpoint); err == nil {
		endpointHost = host
	}

	isValid := false
	for _, domain := range allowedDomains {
		if strings.HasSuffix(endpointHost, domain) {
			isValid = true
			break
		}
	}

	if !isValid {
		return fmt.Errorf("invalid voice endpoint: %s", v.endpoint)
	}

	vg := "wss://" + strings.TrimSuffix(v.endpoint, ":80") + "?v=8"
	v.log(LogInformational, "connecting to voice endpoint %s", vg)
	v.wsConn, _, err = v.session.Dialer.Dial(vg, nil)
	if err != nil {
		v.log(LogWarning, "error connecting to voice endpoint %s, %s", vg, err)
		return
	}

	type voiceHandshakeData struct {
		ServerID               string `json:"server_id"`
		UserID                 string `json:"user_id"`
		SessionID              string `json:"session_id"`
		Token                  string `json:"token"`
		MaxDAVEProtocolVersion int    `json:"max_dave_protocol_version"`
	}
	type voiceHandshakeOp struct {
		Op   int                `json:"op"` // Always 0
		Data voiceHandshakeData `json:"d"`
	}
	data := voiceHandshakeOp{0, voiceHandshakeData{v.GuildID, v.UserID, v.sessionID, v.token, 1}}

	v.wsMutex.Lock()
	err = v.wsConn.WriteJSON(data)
	v.wsMutex.Unlock()
	if err != nil {
		v.log(LogWarning, "error sending init packet, %s", err)
		return
	}

	v.close = make(chan struct{})
	go v.wsListen(v.wsConn, v.close)

	// add loop/check for Ready bool here?
	// then return false if not ready?
	// but then wsListen will also err.

	return
}

// wsListen listens on the voice websocket for messages and passes them
// to the voice event handler.  This is automatically called by the Open func
func (v *VoiceConnection) wsListen(wsConn *websocket.Conn, close <-chan struct{}) {

	v.log(LogInformational, "called")

	for {
		messageType, message, err := wsConn.ReadMessage()
		if err != nil {
			// 4014 indicates a manual disconnection by someone in the guild;
			// 4017 indicates DAVE protocol required but not supported;
			// we shouldn't reconnect.
			if websocket.IsCloseError(err, 4014) {
				v.log(LogInformational, "received 4014 voice disconnection")

				// Abandon the voice WS connection
				v.Lock()
				if v.wsConn != wsConn {
					v.Unlock()
					return
				}
				v.wsConn = nil
				v.Unlock()

				// Wait for VOICE_SERVER_UPDATE.
				// When the bot is moved by the user to another voice channel,
				// VOICE_SERVER_UPDATE is received after the code 4014.
				for i := 0; i < 5; i++ { // TODO: temp, wait for VoiceServerUpdate.
					<-time.After(1 * time.Second)

					v.RLock()
					reconnected := v.wsConn != nil
					v.RUnlock()
					if !reconnected {
						continue
					}
					v.log(LogInformational, "successfully reconnected after 4014 manual disconnection")
					return
				}

				// When VOICE_SERVER_UPDATE is not received, disconnect as usual.
				v.log(LogInformational, "disconnect due to 4014 manual disconnection")

				v.session.Lock()
				delete(v.session.VoiceConnections, v.GuildID)
				v.session.Unlock()

				v.Close()

				return
			}

			if closeCode, ok := voiceWebsocketCloseCode(err); ok && classifyVoiceCloseCode(closeCode) == voiceCloseTerminal {
				v.log(LogInformational, "voice websocket closed with terminal code %d", closeCode)
				v.Lock()
				if v.wsConn == wsConn {
					v.wsConn = nil
				}
				v.Unlock()
				v.Close()
				if v.session != nil {
					v.session.Lock()
					delete(v.session.VoiceConnections, v.GuildID)
					v.session.Unlock()
				}
				return
			}

			// Detect if we have been closed manually. If a Close() has already
			// happened, the websocket we are listening on will be different to the
			// current session.
			v.RLock()
			sameConnection := v.wsConn == wsConn
			v.RUnlock()
			if sameConnection {

				v.log(LogError, "voice endpoint %s websocket closed unexpectedly, %s", v.endpoint, err)

				// Start reconnect goroutine then exit.
				go v.reconnect()
			}
			return
		}

		// Pass received message to voice event handler
		select {
		case <-close:
			return
		default:
			v.onEvent(messageType == websocket.BinaryMessage, message)
		}
	}
}

type voiceCloseAction uint8

const (
	voiceCloseReconnect voiceCloseAction = iota
	voiceCloseWaitForServerUpdate
	voiceCloseTerminal
)

func voiceWebsocketCloseCode(err error) (int, bool) {
	closeError, ok := err.(*websocket.CloseError)
	if !ok {
		return 0, false
	}
	return closeError.Code, true
}

func classifyVoiceCloseCode(code int) voiceCloseAction {
	switch code {
	case 4014:
		return voiceCloseWaitForServerUpdate
	case 4017, 4021, 4022:
		return voiceCloseTerminal
	default:
		return voiceCloseReconnect
	}
}

// wsEvent handles any voice websocket events. This is only called by the
// wsListen() function.
func (v *VoiceConnection) onEvent(isBinary bool, message []byte) {

	if isBinary {
		v.log(LogDebug, "received voice binary payload: len=%d", len(message))
		v.handleDAVEBinary(message)
		return
	}

	v.log(LogDebug, "received voice payload: %s", redactJSON(message))

	var e voiceWebsocketMessage
	if err := json.Unmarshal(message, &e); err != nil {
		v.log(LogError, "unmarshall error, %s", err)
		return
	}

	if e.Sequence != nil {
		v.Lock()
		v.seqAck = *e.Sequence
		v.Unlock()
	}

	switch e.Operation {

	case 2: // READY

		if err := json.Unmarshal(e.RawData, &v.op2); err != nil {
			v.log(LogError, "OP2 unmarshal error: %s; data_length=%d", err, len(e.RawData))
			return
		}
		if v.op8.HeartbeatInterval <= 0 {
			v.log(LogError, "received voice READY before a valid HELLO")
			v.RLock()
			wsConn := v.wsConn
			v.RUnlock()
			if wsConn != nil {
				_ = wsConn.Close()
			}
			return
		}

		// Start the voice websocket heartbeat to keep the connection alive
		v.RLock()
		wsConn := v.wsConn
		closeChannel := v.close
		heartbeatInterval := v.op8.HeartbeatInterval
		v.RUnlock()
		go v.wsHeartbeat(wsConn, closeChannel, time.Duration(heartbeatInterval))
		// TODO monitor a chan/bool to verify this was successful

		// Start the UDP connection
		err := v.udpOpen()
		if err != nil {
			v.log(LogError, "error opening udp connection, %s", err)
			return
		}

		return

	case 6: // HEARTBEAT ACK
		var ack struct {
			T int64 `json:"t"`
		}
		if err := json.Unmarshal(e.RawData, &ack); err != nil {
			v.log(LogError, "OP6 unmarshal error, %s", err)
			return
		}
		now := time.Now()
		v.Lock()
		if ack.T == v.lastHeartbeatNonce {
			v.LastHeartbeatAck = now
			v.HeartbeatLatency = now.Sub(v.LastHeartbeatSent)
			v.awaitingHeartbeat = false
		}
		v.Unlock()
		return

	case 4: // udp encryption secret key
		v.Lock()

		v.op4 = voiceOP4{}
		if err := json.Unmarshal(e.RawData, &v.op4); err != nil {
			v.Unlock()
			v.log(LogError, "OP4 unmarshal error: %s; data_length=%d", err, len(e.RawData))
			return
		}

		v.log(LogInformational, "OP4 received: mode=%s, dave_version=%d",
			v.op4.Mode, v.op4.DAVEProtocolVersion)

		switch v.op4.Mode {
		case "aead_aes256_gcm_rtpsize":
			block, err := aes.NewCipher(v.op4.SecretKey)
			if err != nil {
				v.Unlock()
				v.log(LogError, "error creating AES cipher, %s", err)
				return
			}
			v.aead, err = cipher.NewGCM(block)
			if err != nil {
				v.Unlock()
				v.log(LogError, "error creating GCM, %s", err)
				return
			}
		case "aead_xchacha20_poly1305_rtpsize":
			var err error
			v.aead, err = chacha20poly1305.NewX(v.op4.SecretKey)
			if err != nil {
				v.Unlock()
				v.log(LogError, "error creating XChaCha20 cipher, %s", err)
				return
			}
		default:
			v.Unlock()
			v.log(LogError, "unknown encryption mode: %s", v.op4.Mode)
			return
		}

		var daveKPData []byte
		v.log(LogInformational, "DAVE protocol version %d", v.op4.DAVEProtocolVersion)
		if v.op4.DAVEProtocolVersion > 0 {
			dave := NewDAVESession(v.UserID)
			if err := dave.Configure(
				v.ChannelID,
				v.op4.DAVEProtocolVersion,
				v.ConnectedUsers,
			); err != nil {
				v.Unlock()
				v.log(LogError, "DAVE MLS initialization failed: %s", err)
				return
			}
			for ssrc, userID := range v.ssrcToUserID {
				dave.SetSSRC(ssrc, userID)
			}

			var err error
			daveKPData, err = dave.GenerateKeyPackage()
			if err != nil {
				v.Unlock()
				v.log(LogError, "DAVE key package generation failed: %s", err)
				return
			}
			v.dave = dave
		}

		if v.OpusSend == nil {
			v.OpusSend = make(chan []byte, 16)
		}
		go v.opusSender(v.udpConn, v.close, v.OpusSend, 48000, 960)

		if !v.deaf {
			if v.OpusRecv == nil {
				v.OpusRecv = make(chan *Packet, 2)
			}
			go v.opusReceiver(v.udpConn, v.close, v.OpusRecv)
		}

		v.Ready = true
		v.Unlock()

		if daveKPData != nil {
			v.sendDAVEKeyPackageBinary(daveKPData)
		}

		return

	case 5:
		voiceSpeakingUpdate := &VoiceSpeakingUpdate{}
		if err := json.Unmarshal(e.RawData, voiceSpeakingUpdate); err != nil {
			v.log(LogError, "OP5 unmarshal error: %s; data_length=%d", err, len(e.RawData))
			return
		}

		v.Lock()
		if v.ssrcToUserID == nil {
			v.ssrcToUserID = make(map[uint32]string)
		}
		v.ssrcToUserID[uint32(voiceSpeakingUpdate.SSRC)] = voiceSpeakingUpdate.UserID
		dave := v.dave
		handlers := append([]VoiceSpeakingUpdateHandler(nil), v.voiceSpeakingUpdateHandlers...)
		v.Unlock()
		if dave != nil {
			dave.SetSSRC(uint32(voiceSpeakingUpdate.SSRC), voiceSpeakingUpdate.UserID)
		}

		for _, h := range handlers {
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						if v.session != nil {
							v.session.reportHandlerPanic(voiceSpeakingUpdate, recovered)
						} else {
							v.log(LogError, "voice event handler panicked for %T", voiceSpeakingUpdate)
						}
					}
				}()
				h(v, voiceSpeakingUpdate)
			}()
		}

	case 11: // CLIENTS CONNECT
		clients := &VoiceClientsConnect{}
		if err := json.Unmarshal(e.RawData, clients); err != nil {
			v.log(LogError, "OP11 unmarshal error, %s", err)
			return
		}
		v.Lock()
		v.ConnectedUsers = append(v.ConnectedUsers[:0], clients.UserIDs...)
		connectedUsers := append([]string(nil), v.ConnectedUsers...)
		dave := v.dave
		handlers := append([]VoiceClientsConnectHandler(nil), v.voiceClientsConnectHandlers...)
		v.Unlock()
		if dave != nil {
			dave.SetRecognizedUsers(connectedUsers)
		}
		for _, handler := range handlers {
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						if v.session != nil {
							v.session.reportHandlerPanic(clients, recovered)
						} else {
							v.log(LogError, "voice event handler panicked for %T", clients)
						}
					}
				}()
				handler(v, clients)
			}()
		}
		return

	case 13: // Client Disconnect
		var disconnected struct {
			UserID string `json:"user_id"`
		}
		if err := json.Unmarshal(e.RawData, &disconnected); err != nil {
			v.log(LogError, "OP13 unmarshal error, %s", err)
			return
		}
		v.Lock()
		users := v.ConnectedUsers[:0]
		for _, userID := range v.ConnectedUsers {
			if userID != disconnected.UserID {
				users = append(users, userID)
			}
		}
		v.ConnectedUsers = users
		connectedUsers := append([]string(nil), users...)
		dave := v.dave
		v.Unlock()
		if dave != nil {
			dave.SetRecognizedUsers(connectedUsers)
		}
		v.log(LogDebug, "user disconnected from voice")
		return

	case 21: // DAVE prepare_transition
		v.handleDAVEPrepareTransition(e.RawData)
		return

	case 22: // DAVE execute_transition
		v.handleDAVEExecuteTransition(e.RawData)
		return

	case 24: // DAVE prepare_epoch
		v.handleDAVEPrepareEpoch(e.RawData)
		return

	case 8: // HELLO
		if err := json.Unmarshal(e.RawData, &v.op8); err != nil {
			v.log(LogError, "OP8 unmarshal error: %s; data_length=%d", err, len(e.RawData))
			return
		}
		return

	default:
		v.log(LogDebug, "unknown voice operation, %d, %s", e.Operation, redactJSON(e.RawData))
	}
}

type voiceHeartbeatOp struct {
	Op   int                `json:"op"` // Always 3
	Data voiceHeartbeatData `json:"d"`
}

type voiceHeartbeatData struct {
	T      int64 `json:"t"`
	SeqAck int   `json:"seq_ack"`
}

// NOTE :: When a guild voice server changes how do we shut this down
// properly, so a new connection can be setup without fuss?
//
// wsHeartbeat sends regular heartbeats to voice Discord so it knows the client
// is still connected.  If you do not send these heartbeats Discord will
// disconnect the websocket connection after a few seconds.
func (v *VoiceConnection) wsHeartbeat(wsConn *websocket.Conn, close <-chan struct{}, i time.Duration) {

	if close == nil || wsConn == nil || i <= 0 {
		if i <= 0 {
			v.log(LogError, "invalid voice heartbeat interval %s", i)
		}
		return
	}

	ticker := time.NewTicker(i * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			v.Lock()
			if v.awaitingHeartbeat {
				v.Unlock()
				v.log(LogError, "voice heartbeat ACK not received before next interval")
				_ = wsConn.Close()
				return
			}
			nonce := time.Now().UnixMilli()
			seqAck := v.seqAck
			v.LastHeartbeatSent = time.Now()
			v.lastHeartbeatNonce = nonce
			v.awaitingHeartbeat = true
			v.Unlock()

			v.log(LogDebug, "sending heartbeat packet")
			v.wsMutex.Lock()
			err := wsConn.WriteJSON(voiceHeartbeatOp{3, voiceHeartbeatData{nonce, seqAck}})
			v.wsMutex.Unlock()
			if err != nil {
				v.Lock()
				v.awaitingHeartbeat = false
				v.Unlock()
				v.log(LogError, "error sending heartbeat to voice endpoint %s, %s", v.endpoint, err)
				return
			}
		case <-close:
			return
		}
	}
}

// ------------------------------------------------------------------------------------------------
// Code related to the VoiceConnection UDP connection
// ------------------------------------------------------------------------------------------------

type voiceUDPData struct {
	Address string `json:"address"` // Public IP of machine running this code
	Port    uint16 `json:"port"`    // UDP Port of machine running this code
	Mode    string `json:"mode"`    // always "xsalsa20_poly1305"
}

type voiceUDPD struct {
	Protocol string       `json:"protocol"` // Always "udp" ?
	Data     voiceUDPData `json:"data"`
}

type voiceUDPOp struct {
	Op   int       `json:"op"` // Always 1
	Data voiceUDPD `json:"d"`
}

// udpOpen opens a UDP connection to the voice server and completes the
// initial required handshake.  This connection is left open in the session
// and can be used to send or receive audio.  This should only be called
// from voice.wsEvent OP2
func (v *VoiceConnection) udpOpen() (err error) {

	v.Lock()
	defer v.Unlock()

	if v.wsConn == nil {
		return fmt.Errorf("nil voice websocket")
	}

	if v.udpConn != nil {
		return fmt.Errorf("udp connection already open")
	}

	if v.close == nil {
		return fmt.Errorf("nil close channel")
	}

	if v.endpoint == "" {
		return fmt.Errorf("empty endpoint")
	}

	host := v.op2.IP + ":" + strconv.Itoa(v.op2.Port)
	addr, err := net.ResolveUDPAddr("udp", host)
	if err != nil {
		v.log(LogWarning, "error resolving udp host %s, %s", host, err)
		return
	}

	v.log(LogInformational, "connecting to udp addr %s", addr.String())
	v.udpConn, err = net.DialUDP("udp", nil, addr)
	if err != nil {
		v.log(LogWarning, "error connecting to udp addr %s, %s", addr.String(), err)
		return
	}

	// Create a 74 byte array to store the packet data
	sb := make([]byte, 74)
	binary.BigEndian.PutUint16(sb, 1)              // Packet type (0x1 is request, 0x2 is response)
	binary.BigEndian.PutUint16(sb[2:], 70)         // Packet length (excluding type and length fields)
	binary.BigEndian.PutUint32(sb[4:], v.op2.SSRC) // The SSRC code from the Op 2 VoiceConnection event

	// And send that data over the UDP connection to Discord.
	_, err = v.udpConn.Write(sb)
	if err != nil {
		v.log(LogWarning, "udp write error to %s, %s", addr.String(), err)
		return
	}

	// Create a 74-byte array and listen for the initial handshake response
	// from Discord.  Once we get it parse the IP and PORT information out
	// of the response.  This should be our public IP and PORT as Discord
	// saw us.
	rb := make([]byte, 74)
	rlen, _, err := v.udpConn.ReadFromUDP(rb)
	if err != nil {
		v.log(LogWarning, "udp read error, %s, %s", addr.String(), err)
		return
	}

	if rlen < 74 {
		v.log(LogWarning, "received udp packet too small")
		return fmt.Errorf("received udp packet too small")
	}

	// Loop over position 8 through 71 to grab the IP address.
	var ip string
	for i := 8; i < len(rb)-2; i++ {
		if rb[i] == 0 {
			break
		}
		ip += string(rb[i])
	}

	// Grab port from position 72 and 73
	port := binary.BigEndian.Uint16(rb[len(rb)-2:])

	// Take the data from above and send it back to Discord to finalize
	// the UDP connection handshake.

	encryptionMode := ""
	for _, mode := range v.op2.Modes {
		switch mode {
		case "aead_aes256_gcm_rtpsize":
			encryptionMode = mode
		case "aead_xchacha20_poly1305_rtpsize":
			if encryptionMode == "" {
				encryptionMode = mode
			}
		}
	}
	data := voiceUDPOp{1, voiceUDPD{"udp", voiceUDPData{ip, port, encryptionMode}}}

	v.wsMutex.Lock()
	err = v.wsConn.WriteJSON(data)
	v.wsMutex.Unlock()
	if err != nil {
		v.log(LogWarning, "udp write error, %#v, %s", data, err)
		return
	}

	// start udpKeepAlive
	go v.udpKeepAlive(v.udpConn, v.close, 5*time.Second)
	// TODO: find a way to check that it fired off okay

	return
}

// udpKeepAlive sends a udp packet to keep the udp connection open
// This is still a bit of a "proof of concept"
func (v *VoiceConnection) udpKeepAlive(udpConn *net.UDPConn, close <-chan struct{}, i time.Duration) {

	if udpConn == nil || close == nil {
		return
	}

	var err error
	var sequence uint64

	packet := make([]byte, 8)

	ticker := time.NewTicker(i)
	defer ticker.Stop()
	for {

		binary.LittleEndian.PutUint64(packet, sequence)
		sequence++

		_, err = udpConn.Write(packet)
		if err != nil {
			v.log(LogError, "write error, %s", err)
			return
		}

		select {
		case <-ticker.C:
			// continue loop and send keepalive
		case <-close:
			return
		}
	}
}

// opusSender will listen on the given channel and send any
// pre-encoded opus audio to Discord.  Supposedly.
func (v *VoiceConnection) opusSender(udpConn *net.UDPConn, close <-chan struct{}, opus <-chan []byte, rate, size int) {

	if udpConn == nil || close == nil {
		return
	}

	var sequence uint16
	var timestamp uint32
	var recvbuf []byte
	var ok bool
	udpHeader := make([]byte, 12)
	nonce := make([]byte, v.aead.NonceSize())

	// build the parts that don't change in the udpHeader
	udpHeader[0] = 0x80
	udpHeader[1] = 0x78
	binary.BigEndian.PutUint32(udpHeader[8:], v.op2.SSRC)

	// start a send loop that loops until buf chan is closed
	ticker := time.NewTicker(time.Millisecond * time.Duration(size/(rate/1000)))
	defer ticker.Stop()
	for i := uint32(0); ; i++ {

		// Get data from chan.  If chan is closed, return.
		select {
		case <-close:
			return
		case recvbuf, ok = <-opus:
			if !ok {
				return
			}
			// else, continue loop
		}

		v.RLock()
		dave := v.dave
		speaking := v.speaking
		v.RUnlock()

		if !speaking {
			err := v.Speaking(true)
			if err != nil {
				v.log(LogError, "error sending speaking packet, %s", err)
			}
		}

		// Add sequence and timestamp to udpPacket
		binary.BigEndian.PutUint16(udpHeader[2:], sequence)
		binary.BigEndian.PutUint32(udpHeader[4:], timestamp)

		if dave != nil && dave.IsActive() {
			encrypted, err := dave.EncryptFrame(recvbuf)
			if err != nil {
				v.log(LogError, "DAVE encrypt error: %s", err)
				continue
			}
			recvbuf = encrypted
		}

		binary.LittleEndian.PutUint32(nonce, i)
		sendbuf := make([]byte, len(udpHeader), len(udpHeader)+len(nonce)+len(recvbuf)+v.aead.Overhead())
		copy(sendbuf, udpHeader)
		v.RLock()
		sendbuf = v.aead.Seal(sendbuf, nonce, recvbuf, udpHeader)
		v.RUnlock()
		sendbuf = append(sendbuf, nonce[:4]...)

		// block here until we're exactly at the right time :)
		// Then send rtp audio packet to Discord over UDP
		select {
		case <-close:
			return
		case <-ticker.C:
			// continue
		}
		_, err := udpConn.Write(sendbuf)

		if err != nil {
			v.log(LogError, "udp write error, %s", err)
			return
		}

		// don't care if it overflows because it is already defined in Go spec
		// https://go.dev/ref/spec#Integer_overflow
		sequence++
		timestamp += uint32(size)
	}
}

// A Packet contains the headers and content of a received voice packet.
type Packet struct {
	Flags       byte // first byte of RTP header
	PayloadType byte // second byte of RTP header
	Sequence    uint16
	Timestamp   uint32
	SSRC        uint32
	CSRC        []uint32
	Extension   []byte // RTP header extension with extension header, can be nil
	Opus        []byte
}

// opusReceiver listens on the UDP socket for incoming packets
// and sends them across the given channel
// NOTE :: This function may change names later.
func (v *VoiceConnection) opusReceiver(udpConn *net.UDPConn, close <-chan struct{}, c chan *Packet) {

	if udpConn == nil || close == nil {
		return
	}

	recvbuf := make([]byte, 2048)

	for {
		rlen, err := udpConn.Read(recvbuf)
		if err != nil {
			// Detect if we have been closed manually. If a Close() has already
			// happened, the udp connection we are listening on will be different
			// to the current session.
			v.RLock()
			sameConnection := v.udpConn == udpConn
			v.RUnlock()
			if sameConnection {

				v.log(LogError, "udp read error, %s, %s", v.endpoint, err)

				go v.reconnect()
			}
			return
		}

		select {
		case <-close:
			return
		default:
			// continue loop
		}

		v.RLock()
		aead := v.aead
		v.RUnlock()
		p, err := decodeVoicePacket(recvbuf[:rlen], aead)
		if err != nil {
			v.log(LogInformational, "dropping invalid voice UDP packet: %v", err)
			continue
		}

		v.RLock()
		dave := v.dave
		v.RUnlock()
		if dave != nil {
			decrypted, err := dave.DecryptFrame(p.SSRC, p.Opus)
			if err != nil {
				v.log(LogDebug, "DAVE decrypt error for SSRC %d: %s", p.SSRC, err)
				continue
			}
			p.Opus = decrypted
		}

		if c != nil {
			select {
			case c <- p:
			case <-close:
				return
			}
		}
	}
}

func decodeVoicePacket(data []byte, aead cipher.AEAD) (*Packet, error) {
	const (
		rtpFixedHeaderSize = 12
		nonceSuffixSize    = 4
	)

	if aead == nil {
		return nil, fmt.Errorf("voice transport cipher is not initialized")
	}
	if aead.NonceSize() < nonceSuffixSize {
		return nil, fmt.Errorf("voice transport nonce size %d is too small", aead.NonceSize())
	}
	if len(data) < rtpFixedHeaderSize {
		return nil, fmt.Errorf("RTP packet too short: %d bytes", len(data))
	}
	if data[0]&0xC0 != 0x80 {
		return nil, fmt.Errorf("unsupported RTP version flags %#x", data[0])
	}

	csrcCount := int(data[0] & 0x0F)
	headerLength := rtpFixedHeaderSize + 4*csrcCount
	if len(data) < headerLength {
		return nil, fmt.Errorf("RTP packet truncated in CSRC list")
	}

	hasExtension := data[0]&0x10 != 0
	extensionStart := headerLength
	if hasExtension {
		headerLength += 4
		if len(data) < headerLength {
			return nil, fmt.Errorf("RTP packet truncated in extension header")
		}
	}

	minimumLength := headerLength + aead.Overhead() + nonceSuffixSize
	if len(data) < minimumLength {
		return nil, fmt.Errorf("RTP packet lacks ciphertext, authentication tag, or nonce")
	}

	nonce := make([]byte, aead.NonceSize())
	copy(nonce, data[len(data)-nonceSuffixSize:])
	plaintext, err := aead.Open(nil, nonce, data[headerLength:len(data)-nonceSuffixSize], data[:headerLength])
	if err != nil {
		return nil, fmt.Errorf("opening voice transport packet: %w", err)
	}

	packet := &Packet{
		Flags:       data[0],
		PayloadType: data[1],
		Sequence:    binary.BigEndian.Uint16(data[2:4]),
		Timestamp:   binary.BigEndian.Uint32(data[4:8]),
		SSRC:        binary.BigEndian.Uint32(data[8:12]),
		CSRC:        make([]uint32, csrcCount),
	}
	for i := range packet.CSRC {
		offset := rtpFixedHeaderSize + 4*i
		packet.CSRC[i] = binary.BigEndian.Uint32(data[offset : offset+4])
	}

	if hasExtension {
		extensionWords := int(binary.BigEndian.Uint16(data[extensionStart+2 : extensionStart+4]))
		extensionDataLength := extensionWords * 4
		if extensionDataLength > len(plaintext) {
			return nil, fmt.Errorf("RTP extension length %d exceeds plaintext length %d", extensionDataLength, len(plaintext))
		}
		packet.Extension = make([]byte, 4+extensionDataLength)
		copy(packet.Extension, data[extensionStart:extensionStart+4])
		copy(packet.Extension[4:], plaintext[:extensionDataLength])
		plaintext = plaintext[extensionDataLength:]
	}

	packet.Opus = plaintext
	return packet, nil
}

// Reconnect will close down a voice connection then immediately try to
// reconnect to that session.
// NOTE : This func is messy and a WIP while I find what works.
// It will be cleaned up once a proven stable option is flushed out.
// aka: this is ugly shit code, please don't judge too harshly.
func (v *VoiceConnection) reconnect() {

	v.log(LogInformational, "called")

	v.Lock()
	if v.reconnecting {
		v.log(LogInformational, "already reconnecting to channel %s, exiting", v.ChannelID)
		v.Unlock()
		return
	}
	v.reconnecting = true
	v.Unlock()

	defer func() {
		v.Lock()
		v.reconnecting = false
		v.Unlock()
	}()

	// Close any currently open connections
	v.Close()

	wait := time.Duration(1)
	for {

		<-time.After(wait * time.Second)
		wait *= 2
		if wait > 600 {
			wait = 600
		}

		v.RLock()
		session := v.session
		guildID := v.GuildID
		channelID := v.ChannelID
		mute := v.mute
		deaf := v.deaf
		v.RUnlock()
		if session == nil {
			v.log(LogInformational, "cannot reconnect voice connection without a session")
			return
		}

		session.RLock()
		sessionReady := session.DataReady && session.wsConn != nil
		session.RUnlock()
		if !sessionReady {
			v.log(LogInformational, "cannot reconnect to channel %s with unready session", channelID)
			continue
		}

		v.log(LogInformational, "trying to reconnect to channel %s", channelID)

		_, err := session.ChannelVoiceJoin(guildID, channelID, mute, deaf)
		if err == nil {
			v.log(LogInformational, "successfully reconnected to channel %s", channelID)
			return
		}

		v.log(LogInformational, "error reconnecting to channel %s, %s", channelID, err)

		// if the reconnect above didn't work lets just send a disconnect
		// packet to reset things.
		// Send a OP4 with a nil channel to disconnect
		data := voiceChannelJoinOp{4, voiceChannelJoinData{&guildID, nil, true, true}}
		err = session.GatewayWriteStruct(data)
		if err != nil {
			v.log(LogError, "error sending disconnect packet, %s", err)
		}

	}
}

// ------------------------------------------------------------------------------------------------
// DAVE E2EE Protocol Handlers
// ------------------------------------------------------------------------------------------------

func (v *VoiceConnection) handleDAVEBinary(message []byte) {
	if len(message) < 3 {
		v.log(LogWarning, "DAVE binary message too short: %d bytes", len(message))
		return
	}

	sequence := binary.BigEndian.Uint16(message[:2])
	v.Lock()
	if v.hasDAVESequence {
		delta := sequence - v.lastDAVESequence
		if delta == 0 || delta > 1<<15 {
			v.Unlock()
			v.log(LogWarning, "dropping replayed or out-of-order DAVE binary sequence")
			return
		}
	}
	v.lastDAVESequence = sequence
	v.hasDAVESequence = true
	v.seqAck = int(sequence)
	v.Unlock()

	opcode := message[2]
	payload := message[3:]
	v.log(LogDebug, "DAVE binary opcode=%d len=%d", opcode, len(payload))

	switch opcode {
	case 25:
		v.RLock()
		dave := v.dave
		v.RUnlock()
		if dave != nil {
			if err := dave.HandleExternalSenderPackage(payload); err != nil {
				v.log(LogError, "DAVE external sender package failed: %s", err)
			}
		}

	case 27:
		v.RLock()
		dave := v.dave
		v.RUnlock()
		if dave == nil {
			return
		}
		commitWelcome, err := dave.HandleProposals(payload)
		if err != nil {
			v.log(LogError, "DAVE proposal processing failed: %s", err)
			return
		}
		if len(commitWelcome) > 0 {
			v.sendDAVECommitWelcomeBinary(commitWelcome)
		}

	case 29:
		if len(payload) <= 2 {
			v.log(LogWarning, "DAVE commit payload too short")
			return
		}
		transitionID := binary.BigEndian.Uint16(payload[0:2])
		commit := payload[2:]
		v.log(LogInformational, "DAVE commit transition_id=%d", transitionID)

		v.RLock()
		dave := v.dave
		v.RUnlock()
		if dave == nil {
			return
		}

		if err := dave.HandleCommit(commit); err != nil {
			v.log(LogError, "DAVE commit processing failed: %s", err)
			v.recoverDAVEGroup(transitionID, dave)
			return
		}
		if err := dave.HandlePrepareTransition(transitionID, 1); err != nil {
			v.log(LogError, "DAVE commit transition preparation failed: %s", err)
			v.recoverDAVEGroup(transitionID, dave)
			return
		}
		v.sendDAVEReadyForTransition(transitionID)

	case 30:
		if len(payload) <= 2 {
			v.log(LogWarning, "DAVE welcome payload too short")
			return
		}
		transitionID := binary.BigEndian.Uint16(payload[0:2])
		welcomeData := payload[2:]

		v.log(LogInformational, "DAVE welcome (%d bytes) transition_id=%d", len(welcomeData), transitionID)
		v.RLock()
		dave := v.dave
		v.RUnlock()
		if dave == nil {
			v.log(LogWarning, "DAVE welcome received but no session")
			return
		}

		if err := dave.HandleWelcome(welcomeData); err != nil {
			v.log(LogError, "DAVE welcome processing failed: %s", err)
			v.recoverDAVEGroup(transitionID, dave)
			return
		}

		if err := dave.HandlePrepareTransition(transitionID, 1); err != nil {
			v.log(LogError, "DAVE welcome transition preparation failed: %s", err)
			v.recoverDAVEGroup(transitionID, dave)
			return
		}
		v.log(LogInformational, "DAVE encryption prepared after Welcome")
		v.sendDAVEReadyForTransition(transitionID)

	default:
		v.log(LogDebug, "DAVE unknown binary opcode %d (%d bytes)", opcode, len(payload))
	}
}

func (v *VoiceConnection) handleDAVEPrepareTransition(data json.RawMessage) {
	var msg struct {
		TransitionID        uint16 `json:"transition_id"`
		DAVEProtocolVersion int    `json:"protocol_version"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		v.log(LogError, "DAVE prepare_transition unmarshal error: %s", err)
		return
	}

	v.log(LogInformational, "DAVE prepare_transition id=%d version=%d", msg.TransitionID, msg.DAVEProtocolVersion)

	v.RLock()
	dave := v.dave
	v.RUnlock()
	if dave != nil {
		if err := dave.HandlePrepareTransition(msg.TransitionID, msg.DAVEProtocolVersion); err != nil {
			v.log(LogError, "DAVE prepare_transition failed: %s", err)
			return
		}
		if msg.TransitionID == 0 {
			if err := dave.HandleExecuteTransition(0); err != nil {
				v.log(LogError, "DAVE immediate transition failed: %s", err)
			}
			return
		}
		v.sendDAVEReadyForTransition(msg.TransitionID)
	}
}

func (v *VoiceConnection) handleDAVEExecuteTransition(data json.RawMessage) {
	var msg struct {
		TransitionID uint16 `json:"transition_id"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		v.log(LogError, "DAVE execute_transition unmarshal error: %s", err)
		return
	}

	v.RLock()
	dave := v.dave
	v.RUnlock()
	if dave != nil {
		if err := dave.HandleExecuteTransition(msg.TransitionID); err != nil {
			v.log(LogError, "DAVE execute_transition failed: %s", err)
			return
		}
		v.log(LogInformational, "DAVE execute_transition id=%d canEncrypt=%v", msg.TransitionID, dave.CanEncrypt())
	}
}

func (v *VoiceConnection) handleDAVEPrepareEpoch(data json.RawMessage) {
	var msg struct {
		Epoch               uint64 `json:"epoch"`
		DAVEProtocolVersion int    `json:"protocol_version"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		v.log(LogError, "DAVE prepare_epoch unmarshal error: %s", err)
		return
	}

	v.log(LogInformational, "DAVE prepare_epoch epoch=%d version=%d", msg.Epoch, msg.DAVEProtocolVersion)

	v.RLock()
	dave := v.dave
	v.RUnlock()
	if dave == nil {
		return
	}

	kpData, err := dave.HandlePrepareEpoch(msg.Epoch, msg.DAVEProtocolVersion)
	if err != nil {
		v.log(LogError, "DAVE prepare_epoch failed: %s", err)
		return
	}

	if len(kpData) > 0 {
		v.sendDAVEKeyPackageBinary(kpData)
	}
}

func (v *VoiceConnection) RekeyDAVE() {
	v.RLock()
	dave := v.dave
	wsConn := v.wsConn
	v.RUnlock()
	if dave == nil || wsConn == nil {
		return
	}

	kpData, err := dave.ResetForReWelcome()
	if err != nil {
		v.log(LogError, "DAVE rekey failed: %s", err)
		return
	}
	v.sendDAVEKeyPackageBinary(kpData)
}

func (v *VoiceConnection) sendDAVEKeyPackageBinary(kpData []byte) {
	v.log(LogInformational, "DAVE sending key package (%d bytes)", len(kpData))
	binMsg := make([]byte, 1+len(kpData))
	binMsg[0] = 26
	copy(binMsg[1:], kpData)

	if _, err := v.writeVoiceMessage(websocket.BinaryMessage, binMsg); err != nil && !errors.Is(err, ErrVoiceConnectionClosed) {
		v.log(LogError, "DAVE key package send failed: %s", err)
	}
}

func (v *VoiceConnection) sendDAVECommitWelcomeBinary(data []byte) {
	v.log(LogInformational, "DAVE sending commit/welcome (%d bytes)", len(data))
	message := make([]byte, 1+len(data))
	message[0] = 28
	copy(message[1:], data)
	if _, err := v.writeVoiceMessage(websocket.BinaryMessage, message); err != nil &&
		!errors.Is(err, ErrVoiceConnectionClosed) {
		v.log(LogError, "DAVE commit/welcome send failed: %s", err)
	}
}

func (v *VoiceConnection) recoverDAVEGroup(transitionID uint16, dave *DAVESession) {
	v.sendDAVEInvalidCommitWelcome(transitionID)
	keyPackage, err := dave.ResetForReWelcome()
	if err != nil {
		v.log(LogError, "DAVE reset for re-Welcome failed: %s", err)
		return
	}
	v.sendDAVEKeyPackageBinary(keyPackage)
}

func (v *VoiceConnection) sendDAVEReadyForTransition(transitionID uint16) {
	v.log(LogDebug, "DAVE sending ready_for_transition id=%d", transitionID)

	type readyData struct {
		TransitionID uint16 `json:"transition_id"`
	}
	type readyOp struct {
		Op   int       `json:"op"`
		Data readyData `json:"d"`
	}

	if _, err := v.writeVoiceJSON(readyOp{23, readyData{transitionID}}); err != nil && !errors.Is(err, ErrVoiceConnectionClosed) {
		v.log(LogError, "DAVE ready_for_transition send failed: %s", err)
	}
}

func (v *VoiceConnection) sendDAVEInvalidCommitWelcome(transitionID uint16) {
	v.log(LogInformational, "DAVE sending invalid_commit_welcome id=%d", transitionID)

	type invalidData struct {
		TransitionID uint16 `json:"transition_id"`
	}
	type invalidOp struct {
		Op   int         `json:"op"`
		Data invalidData `json:"d"`
	}

	if _, err := v.writeVoiceJSON(invalidOp{31, invalidData{transitionID}}); err != nil && !errors.Is(err, ErrVoiceConnectionClosed) {
		v.log(LogError, "DAVE invalid_commit_welcome send failed: %s", err)
	}
}
