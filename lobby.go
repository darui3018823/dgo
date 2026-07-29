package dgo

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	lobbyMetadataMaxLength       = 1000
	lobbyMetadataMaxEntries      = 25
	lobbyMetadataValueMaxLength  = 1024
	lobbyIdleTimeoutMinimum      = 5
	lobbyIdleTimeoutMaximum      = 604800
	lobbyMemberMaximum           = 25
	lobbySecretMaxLength         = 250
	lobbyAdditionalNameMaxLength = 80
	lobbyMessageMaxLength        = 4000
	lobbyMessageLimitMaximum     = 200
	lobbyModerationMaxEntries    = 5
	lobbyModerationKeyMaxLength  = 1024
	lobbyModerationValueMaxLen   = 2000
)

var (
	// ErrLobbyValidation reports an invalid lobby identifier or payload.
	ErrLobbyValidation = errors.New("invalid lobby request")
	// ErrLobbyBearerToken reports an invalid raw OAuth2 access token.
	ErrLobbyBearerToken = errors.New("invalid lobby bearer token")
)

// LobbyMemberFlags is a bitfield of permissions granted to a lobby member.
type LobbyMemberFlags uint32

const (
	// LobbyMemberFlagCanLinkLobby allows the member to link a guild channel.
	LobbyMemberFlagCanLinkLobby LobbyMemberFlags = 1 << 0
)

// Lobby represents a Discord application lobby.
type Lobby struct {
	ID                       string            `json:"id"`
	ApplicationID            string            `json:"application_id"`
	Metadata                 map[string]string `json:"metadata"`
	Members                  []LobbyMember     `json:"members"`
	LinkedChannel            *Channel          `json:"linked_channel,omitempty"`
	Flags                    uint32            `json:"flags"`
	OverrideEventWebhooksURL *string           `json:"override_event_webhooks_url,omitempty"`
}

// LobbyMember represents a member of a lobby.
type LobbyMember struct {
	ID             string            `json:"id"`
	Metadata       map[string]string `json:"metadata"`
	Flags          LobbyMemberFlags  `json:"flags"`
	AdditionalName string            `json:"additional_name,omitempty"`
}

// LobbyMessageMember contains lobby-specific information about an author.
type LobbyMessageMember struct {
	AdditionalName string `json:"additional_name"`
}

// LobbyMessage represents a message sent to a lobby.
type LobbyMessage struct {
	ID                 string              `json:"id"`
	Type               MessageType         `json:"type"`
	Content            string              `json:"content"`
	LobbyID            string              `json:"lobby_id"`
	ChannelID          string              `json:"channel_id"`
	Author             *User               `json:"author"`
	LobbyMember        *LobbyMessageMember `json:"lobby_member,omitempty"`
	Metadata           map[string]string   `json:"metadata,omitempty"`
	ModerationMetadata map[string]string   `json:"moderation_metadata,omitempty"`
	Flags              MessageFlags        `json:"flags"`
	ApplicationID      string              `json:"application_id,omitempty"`
}

// LobbyInvite is a single-use invite for a lobby's linked guild channel.
type LobbyInvite struct {
	Code string `json:"code"`
}

// LobbyMemberParams describes a member included while creating or replacing a
// lobby's member list. A non-nil Metadata pointer can encode either an object
// or an explicit JSON null.
type LobbyMemberParams struct {
	ID       string             `json:"id"`
	Metadata *map[string]string `json:"metadata,omitempty"`
	Flags    *LobbyMemberFlags  `json:"flags,omitempty"`
}

// LobbyCreateParams contains fields accepted by POST /lobbies.
type LobbyCreateParams struct {
	Metadata                 *map[string]string   `json:"metadata,omitempty"`
	Members                  *[]LobbyMemberParams `json:"members,omitempty"`
	IdleTimeoutSeconds       *int                 `json:"idle_timeout_seconds,omitempty"`
	Flags                    *uint32              `json:"flags,omitempty"`
	OverrideEventWebhooksURL *string              `json:"override_event_webhooks_url,omitempty"`
}

// LobbyCreateOrJoinParams contains fields accepted by PUT /lobbies.
type LobbyCreateOrJoinParams struct {
	Secret             string             `json:"secret"`
	IdleTimeoutSeconds *int               `json:"idle_timeout_seconds,omitempty"`
	LobbyMetadata      *map[string]string `json:"lobby_metadata,omitempty"`
	MemberMetadata     *map[string]string `json:"member_metadata,omitempty"`
	Flags              *LobbyMemberFlags  `json:"flags,omitempty"`
}

// LobbyEditParams contains fields accepted by PATCH /lobbies/{lobby.id}.
type LobbyEditParams struct {
	Metadata                 *map[string]string   `json:"metadata,omitempty"`
	Members                  *[]LobbyMemberParams `json:"members,omitempty"`
	IdleTimeoutSeconds       *int                 `json:"idle_timeout_seconds,omitempty"`
	Flags                    *uint32              `json:"flags,omitempty"`
	OverrideEventWebhooksURL *string              `json:"override_event_webhooks_url,omitempty"`
}

// LobbyMemberUpdateParams contains fields accepted when adding or updating a
// single lobby member.
type LobbyMemberUpdateParams struct {
	Metadata       *map[string]string `json:"metadata,omitempty"`
	Flags          *LobbyMemberFlags  `json:"flags,omitempty"`
	AdditionalName *string            `json:"additional_name,omitempty"`
}

// LobbyMemberBulkUpdateParams describes one operation in a bulk member update.
type LobbyMemberBulkUpdateParams struct {
	ID             string             `json:"id"`
	Metadata       *map[string]string `json:"metadata,omitempty"`
	Flags          *LobbyMemberFlags  `json:"flags,omitempty"`
	RemoveMember   *bool              `json:"remove_member,omitempty"`
	AdditionalName *string            `json:"additional_name,omitempty"`
}

// LobbyMessageSendParams contains fields accepted when sending a lobby
// message. Content must be non-empty.
type LobbyMessageSendParams struct {
	Content  string             `json:"content"`
	Metadata *map[string]string `json:"metadata,omitempty"`
	Flags    *MessageFlags      `json:"flags,omitempty"`
}

func endpointLobbies() string {
	return EndpointAPI + "lobbies"
}

func endpointLobby(lobbyID string) string {
	return endpointLobbies() + "/" + url.PathEscape(lobbyID)
}

func endpointLobbyMember(lobbyID, userID string) string {
	return endpointLobby(lobbyID) + "/members/" + url.PathEscape(userID)
}

func endpointLobbyMessages(lobbyID string) string {
	return endpointLobby(lobbyID) + "/messages"
}

func validateLobbyID(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s must not be empty", ErrLobbyValidation, name)
	}
	return nil
}

func validateLobbyMetadata(name string, metadata *map[string]string) error {
	if metadata == nil || *metadata == nil {
		return nil
	}
	if len(*metadata) > lobbyMetadataMaxEntries {
		return fmt.Errorf("%w: %s has %d entries, maximum is %d", ErrLobbyValidation, name, len(*metadata), lobbyMetadataMaxEntries)
	}
	total := 0
	for key, value := range *metadata {
		keyLength := utf8.RuneCountInString(key)
		valueLength := utf8.RuneCountInString(value)
		if keyLength > lobbyMetadataValueMaxLength || valueLength > lobbyMetadataValueMaxLength {
			return fmt.Errorf("%w: %s key and value lengths must not exceed %d", ErrLobbyValidation, name, lobbyMetadataValueMaxLength)
		}
		total += keyLength + valueLength
	}
	if total > lobbyMetadataMaxLength {
		return fmt.Errorf("%w: %s total length is %d, maximum is %d", ErrLobbyValidation, name, total, lobbyMetadataMaxLength)
	}
	return nil
}

func validateLobbyFlags(name string, flags *LobbyMemberFlags) error {
	if flags != nil && *flags&^LobbyMemberFlagCanLinkLobby != 0 {
		return fmt.Errorf("%w: %s contains unsupported bits %#x", ErrLobbyValidation, name, uint32(*flags))
	}
	return nil
}

func validateLobbyIdleTimeout(timeout *int) error {
	if timeout != nil && (*timeout < lobbyIdleTimeoutMinimum || *timeout > lobbyIdleTimeoutMaximum) {
		return fmt.Errorf(
			"%w: idle_timeout_seconds must be between %d and %d",
			ErrLobbyValidation,
			lobbyIdleTimeoutMinimum,
			lobbyIdleTimeoutMaximum,
		)
	}
	return nil
}

func validateLobbyAdditionalName(name *string) error {
	if name == nil {
		return nil
	}
	length := utf8.RuneCountInString(*name)
	if length < 1 || length > lobbyAdditionalNameMaxLength {
		return fmt.Errorf(
			"%w: additional_name length must be between 1 and %d",
			ErrLobbyValidation,
			lobbyAdditionalNameMaxLength,
		)
	}
	return nil
}

func validateLobbyMembers(members []LobbyMemberParams) error {
	if len(members) > lobbyMemberMaximum {
		return fmt.Errorf("%w: members has %d entries, maximum is %d", ErrLobbyValidation, len(members), lobbyMemberMaximum)
	}
	seen := make(map[string]struct{}, len(members))
	for index, member := range members {
		if err := validateLobbyID(fmt.Sprintf("members[%d].id", index), member.ID); err != nil {
			return err
		}
		if _, ok := seen[member.ID]; ok {
			return fmt.Errorf("%w: duplicate member id %q", ErrLobbyValidation, member.ID)
		}
		seen[member.ID] = struct{}{}
		if err := validateLobbyMetadata(fmt.Sprintf("members[%d].metadata", index), member.Metadata); err != nil {
			return err
		}
		if err := validateLobbyFlags(fmt.Sprintf("members[%d].flags", index), member.Flags); err != nil {
			return err
		}
	}
	return nil
}

func validateLobbyWebhookURL(value *string) error {
	if value == nil || *value == "" {
		return nil
	}
	if utf8.RuneCountInString(*value) > 512 {
		return fmt.Errorf("%w: override_event_webhooks_url exceeds 512 characters", ErrLobbyValidation)
	}
	parsed, err := url.ParseRequestURI(*value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: override_event_webhooks_url must be an absolute URL", ErrLobbyValidation)
	}
	return nil
}

func validateLobbyCreate(params LobbyCreateParams) error {
	if err := validateLobbyMetadata("metadata", params.Metadata); err != nil {
		return err
	}
	if params.Members != nil {
		if err := validateLobbyMembers(*params.Members); err != nil {
			return err
		}
	}
	if err := validateLobbyIdleTimeout(params.IdleTimeoutSeconds); err != nil {
		return err
	}
	return validateLobbyWebhookURL(params.OverrideEventWebhooksURL)
}

func validateLobbyCreateOrJoin(params LobbyCreateOrJoinParams) error {
	secretLength := utf8.RuneCountInString(params.Secret)
	if secretLength < 1 || secretLength > lobbySecretMaxLength {
		return fmt.Errorf("%w: secret length must be between 1 and %d", ErrLobbyValidation, lobbySecretMaxLength)
	}
	if err := validateLobbyIdleTimeout(params.IdleTimeoutSeconds); err != nil {
		return err
	}
	if err := validateLobbyMetadata("lobby_metadata", params.LobbyMetadata); err != nil {
		return err
	}
	if err := validateLobbyMetadata("member_metadata", params.MemberMetadata); err != nil {
		return err
	}
	return validateLobbyFlags("flags", params.Flags)
}

func validateLobbyMemberUpdate(params LobbyMemberUpdateParams) error {
	if err := validateLobbyMetadata("metadata", params.Metadata); err != nil {
		return err
	}
	if err := validateLobbyFlags("flags", params.Flags); err != nil {
		return err
	}
	return validateLobbyAdditionalName(params.AdditionalName)
}

func validateLobbyBulkMembers(members []LobbyMemberBulkUpdateParams) error {
	if len(members) < 1 || len(members) > lobbyMemberMaximum {
		return fmt.Errorf("%w: bulk members must contain between 1 and %d entries", ErrLobbyValidation, lobbyMemberMaximum)
	}
	seen := make(map[string]struct{}, len(members))
	for index, member := range members {
		if err := validateLobbyID(fmt.Sprintf("members[%d].id", index), member.ID); err != nil {
			return err
		}
		if _, ok := seen[member.ID]; ok {
			return fmt.Errorf("%w: duplicate member id %q", ErrLobbyValidation, member.ID)
		}
		seen[member.ID] = struct{}{}
		if err := validateLobbyMetadata(fmt.Sprintf("members[%d].metadata", index), member.Metadata); err != nil {
			return err
		}
		if err := validateLobbyFlags(fmt.Sprintf("members[%d].flags", index), member.Flags); err != nil {
			return err
		}
		if err := validateLobbyAdditionalName(member.AdditionalName); err != nil {
			return err
		}
	}
	return nil
}

func lobbyBearerOptions(accessToken string, options []RequestOption) ([]RequestOption, error) {
	token := strings.TrimSpace(accessToken)
	lower := strings.ToLower(token)
	if token == "" ||
		strings.HasPrefix(lower, "bot ") ||
		strings.HasPrefix(lower, "bearer ") ||
		strings.ContainsAny(token, " \t\r\n") {
		return nil, fmt.Errorf("%w: pass the raw OAuth2 access token without an authorization scheme", ErrLobbyBearerToken)
	}

	result := make([]RequestOption, 0, len(options)+1)
	result = append(result, options...)
	// Add this last so an arbitrary RequestOption cannot replace the explicit
	// user authorization boundary with the Session's Bot token.
	result = append(result, WithHeader("Authorization", "Bearer "+token))
	return result, nil
}

func decodeLobby(body []byte) (*Lobby, error) {
	var lobby Lobby
	if err := Unmarshal(body, &lobby); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONUnmarshal, err)
	}
	return &lobby, nil
}

func decodeLobbyMember(body []byte) (*LobbyMember, error) {
	var member LobbyMember
	if err := Unmarshal(body, &member); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONUnmarshal, err)
	}
	return &member, nil
}

func decodeLobbyMembers(body []byte) ([]*LobbyMember, error) {
	var members []*LobbyMember
	if err := Unmarshal(body, &members); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONUnmarshal, err)
	}
	return members, nil
}

func decodeLobbyMessage(body []byte) (*LobbyMessage, error) {
	var message LobbyMessage
	if err := Unmarshal(body, &message); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONUnmarshal, err)
	}
	return &message, nil
}

func decodeLobbyMessages(body []byte) ([]*LobbyMessage, error) {
	var messages []*LobbyMessage
	if err := Unmarshal(body, &messages); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONUnmarshal, err)
	}
	return messages, nil
}

func decodeLobbyInvite(body []byte) (*LobbyInvite, error) {
	var invite LobbyInvite
	if err := Unmarshal(body, &invite); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrJSONUnmarshal, err)
	}
	return &invite, nil
}

// LobbyCreate creates an application lobby using the Session's Bot token.
func (s *Session) LobbyCreate(params LobbyCreateParams, options ...RequestOption) (*Lobby, error) {
	if err := validateLobbyCreate(params); err != nil {
		return nil, err
	}
	endpoint := endpointLobbies()
	body, err := s.RequestWithBucketID(http.MethodPost, endpoint, params, endpoint, options...)
	if err != nil {
		return nil, err
	}
	return decodeLobby(body)
}

// LobbyCreateOrJoin creates or joins a Social SDK lobby using a raw OAuth2
// access token with the sdk.social_layer scope.
func (s *Session) LobbyCreateOrJoin(accessToken string, params LobbyCreateOrJoinParams, options ...RequestOption) (*Lobby, error) {
	if err := validateLobbyCreateOrJoin(params); err != nil {
		return nil, err
	}
	bearerOptions, err := lobbyBearerOptions(accessToken, options)
	if err != nil {
		return nil, err
	}
	endpoint := endpointLobbies()
	body, err := s.RequestWithBucketID(http.MethodPut, endpoint, params, endpoint, bearerOptions...)
	if err != nil {
		return nil, err
	}
	return decodeLobby(body)
}

// Lobby returns an application lobby using the Session's Bot token.
func (s *Session) Lobby(lobbyID string, options ...RequestOption) (*Lobby, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	endpoint := endpointLobby(lobbyID)
	body, err := s.RequestWithBucketID(http.MethodGet, endpoint, nil, endpoint, options...)
	if err != nil {
		return nil, err
	}
	return decodeLobby(body)
}

// LobbyEdit modifies an application lobby using the Session's Bot token.
func (s *Session) LobbyEdit(lobbyID string, params LobbyEditParams, options ...RequestOption) (*Lobby, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	if err := validateLobbyCreate(LobbyCreateParams(params)); err != nil {
		return nil, err
	}
	endpoint := endpointLobby(lobbyID)
	body, err := s.RequestWithBucketID(http.MethodPatch, endpoint, params, endpoint, options...)
	if err != nil {
		return nil, err
	}
	return decodeLobby(body)
}

// LobbyDelete deletes an application lobby using the Session's Bot token.
func (s *Session) LobbyDelete(lobbyID string, options ...RequestOption) error {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return err
	}
	endpoint := endpointLobby(lobbyID)
	_, err := s.RequestWithBucketID(http.MethodDelete, endpoint, nil, endpoint, options...)
	return err
}

// LobbyMemberAdd adds or updates a lobby member using the Session's Bot token.
func (s *Session) LobbyMemberAdd(lobbyID, userID string, params LobbyMemberUpdateParams, options ...RequestOption) (*LobbyMember, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	if err := validateLobbyID("user_id", userID); err != nil {
		return nil, err
	}
	if err := validateLobbyMemberUpdate(params); err != nil {
		return nil, err
	}
	endpoint := endpointLobbyMember(lobbyID, userID)
	body, err := s.RequestWithBucketID(http.MethodPut, endpoint, params, endpointLobbyMember(lobbyID, ""), options...)
	if err != nil {
		return nil, err
	}
	return decodeLobbyMember(body)
}

// LobbyMembersBulkUpdate adds, updates, or removes up to 25 lobby members
// using the Session's Bot token.
func (s *Session) LobbyMembersBulkUpdate(lobbyID string, members []LobbyMemberBulkUpdateParams, options ...RequestOption) ([]*LobbyMember, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	if err := validateLobbyBulkMembers(members); err != nil {
		return nil, err
	}
	endpoint := endpointLobby(lobbyID) + "/members/bulk"
	body, err := s.RequestWithBucketID(http.MethodPost, endpoint, members, endpoint, options...)
	if err != nil {
		return nil, err
	}
	return decodeLobbyMembers(body)
}

// LobbyMemberDelete removes a lobby member using the Session's Bot token.
func (s *Session) LobbyMemberDelete(lobbyID, userID string, options ...RequestOption) error {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return err
	}
	if err := validateLobbyID("user_id", userID); err != nil {
		return err
	}
	endpoint := endpointLobbyMember(lobbyID, userID)
	_, err := s.RequestWithBucketID(http.MethodDelete, endpoint, nil, endpointLobbyMember(lobbyID, ""), options...)
	return err
}

// LobbyLeave removes the current user from a lobby using a raw OAuth2 access
// token.
func (s *Session) LobbyLeave(accessToken, lobbyID string, options ...RequestOption) error {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return err
	}
	bearerOptions, err := lobbyBearerOptions(accessToken, options)
	if err != nil {
		return err
	}
	endpoint := endpointLobby(lobbyID) + "/members/@me"
	_, err = s.RequestWithBucketID(http.MethodDelete, endpoint, nil, endpoint, bearerOptions...)
	return err
}

// LobbyChannelLink links a guild channel to a lobby using a raw OAuth2 access
// token.
func (s *Session) LobbyChannelLink(accessToken, lobbyID, channelID string, options ...RequestOption) (*Lobby, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	if err := validateLobbyID("channel_id", channelID); err != nil {
		return nil, err
	}
	bearerOptions, err := lobbyBearerOptions(accessToken, options)
	if err != nil {
		return nil, err
	}
	endpoint := endpointLobby(lobbyID) + "/channel-linking"
	body, err := s.RequestWithBucketID(
		http.MethodPatch,
		endpoint,
		struct {
			ChannelID string `json:"channel_id"`
		}{ChannelID: channelID},
		endpoint,
		bearerOptions...,
	)
	if err != nil {
		return nil, err
	}
	return decodeLobby(body)
}

// LobbyChannelUnlink removes a linked channel using a raw OAuth2 access token.
func (s *Session) LobbyChannelUnlink(accessToken, lobbyID string, options ...RequestOption) (*Lobby, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	bearerOptions, err := lobbyBearerOptions(accessToken, options)
	if err != nil {
		return nil, err
	}
	endpoint := endpointLobby(lobbyID) + "/channel-linking"
	body, err := s.RequestWithBucketID(http.MethodPatch, endpoint, struct{}{}, endpoint, bearerOptions...)
	if err != nil {
		return nil, err
	}
	return decodeLobby(body)
}

// LobbyMessageSend sends a lobby message using a raw OAuth2 access token with
// the sdk.social_layer scope.
func (s *Session) LobbyMessageSend(accessToken, lobbyID string, params LobbyMessageSendParams, options ...RequestOption) (*LobbyMessage, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	contentLength := utf8.RuneCountInString(params.Content)
	if contentLength < 1 || contentLength > lobbyMessageMaxLength {
		return nil, fmt.Errorf("%w: content length must be between 1 and %d", ErrLobbyValidation, lobbyMessageMaxLength)
	}
	if err := validateLobbyMetadata("metadata", params.Metadata); err != nil {
		return nil, err
	}
	bearerOptions, err := lobbyBearerOptions(accessToken, options)
	if err != nil {
		return nil, err
	}
	endpoint := endpointLobbyMessages(lobbyID)
	body, err := s.RequestWithBucketID(http.MethodPost, endpoint, params, endpoint, bearerOptions...)
	if err != nil {
		return nil, err
	}
	return decodeLobbyMessage(body)
}

// LobbyMessages returns recent lobby messages using a raw OAuth2 access token
// with the sdk.social_layer scope. A zero limit requests Discord's default.
func (s *Session) LobbyMessages(accessToken, lobbyID string, limit int, options ...RequestOption) ([]*LobbyMessage, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	if limit < 0 || limit > lobbyMessageLimitMaximum {
		return nil, fmt.Errorf("%w: limit must be 0 or between 1 and %d", ErrLobbyValidation, lobbyMessageLimitMaximum)
	}
	bearerOptions, err := lobbyBearerOptions(accessToken, options)
	if err != nil {
		return nil, err
	}
	endpoint := endpointLobbyMessages(lobbyID)
	requestURL := endpoint
	if limit != 0 {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(limit))
		requestURL += "?" + query.Encode()
	}
	body, err := s.RequestWithBucketID(http.MethodGet, requestURL, nil, endpoint, bearerOptions...)
	if err != nil {
		return nil, err
	}
	return decodeLobbyMessages(body)
}

// LobbyMessageModerationMetadataUpdate sets app-scoped moderation metadata for
// a lobby message using the Session's Bot token.
func (s *Session) LobbyMessageModerationMetadataUpdate(lobbyID, messageID string, metadata map[string]string, options ...RequestOption) error {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return err
	}
	if err := validateLobbyID("message_id", messageID); err != nil {
		return err
	}
	if len(metadata) > lobbyModerationMaxEntries {
		return fmt.Errorf("%w: moderation metadata has %d entries, maximum is %d", ErrLobbyValidation, len(metadata), lobbyModerationMaxEntries)
	}
	for key, value := range metadata {
		if utf8.RuneCountInString(key) > lobbyModerationKeyMaxLength {
			return fmt.Errorf("%w: moderation metadata key exceeds %d characters", ErrLobbyValidation, lobbyModerationKeyMaxLength)
		}
		if utf8.RuneCountInString(value) > lobbyModerationValueMaxLen {
			return fmt.Errorf("%w: moderation metadata value exceeds %d characters", ErrLobbyValidation, lobbyModerationValueMaxLen)
		}
	}
	endpoint := endpointLobbyMessages(lobbyID) + "/" + url.PathEscape(messageID) + "/moderation-metadata"
	_, err := s.RequestWithBucketID(http.MethodPut, endpoint, metadata, endpointLobbyMessages(lobbyID), options...)
	return err
}

// LobbyChannelInviteCreateForSelf creates a single-use invite targeted to the
// current user using a raw OAuth2 access token.
func (s *Session) LobbyChannelInviteCreateForSelf(accessToken, lobbyID string, options ...RequestOption) (*LobbyInvite, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	bearerOptions, err := lobbyBearerOptions(accessToken, options)
	if err != nil {
		return nil, err
	}
	endpoint := endpointLobby(lobbyID) + "/members/@me/invites"
	body, err := s.RequestWithBucketID(http.MethodPost, endpoint, nil, endpoint, bearerOptions...)
	if err != nil {
		return nil, err
	}
	return decodeLobbyInvite(body)
}

// LobbyChannelInviteCreateForUser creates a single-use invite targeted to a
// specified user using the Session's Bot token.
func (s *Session) LobbyChannelInviteCreateForUser(lobbyID, userID string, options ...RequestOption) (*LobbyInvite, error) {
	if err := validateLobbyID("lobby_id", lobbyID); err != nil {
		return nil, err
	}
	if err := validateLobbyID("user_id", userID); err != nil {
		return nil, err
	}
	endpoint := endpointLobbyMember(lobbyID, userID) + "/invites"
	body, err := s.RequestWithBucketID(http.MethodPost, endpoint, nil, endpointLobbyMember(lobbyID, ""), options...)
	if err != nil {
		return nil, err
	}
	return decodeLobbyInvite(body)
}
