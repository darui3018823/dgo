package dgo

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	soundboardSoundNameMinLength = 2
	soundboardSoundNameMaxLength = 32
	soundboardSoundMaxFileSize   = 512 * 1024
)

var (
	// ErrSoundboardValidation is returned before a malformed soundboard REST
	// request is sent to Discord.
	ErrSoundboardValidation = errors.New("invalid soundboard request")
)

// SoundboardSound represents a default or guild soundboard sound.
type SoundboardSound struct {
	Name      string  `json:"name"`
	SoundID   string  `json:"sound_id"`
	Volume    float64 `json:"volume"`
	EmojiID   *string `json:"emoji_id"`
	EmojiName *string `json:"emoji_name"`
	GuildID   string  `json:"guild_id,omitempty"`
	Available bool    `json:"available"`
	User      *User   `json:"user,omitempty"`
}

// SoundboardNullable represents a nullable JSON field that may also be
// omitted. Its zero value omits the field. Use SoundboardValue to send a value
// and SoundboardNull to send an explicit JSON null.
type SoundboardNullable[T any] struct {
	value T
	set   bool
	null  bool
}

// SoundboardValue returns a nullable soundboard field containing value.
func SoundboardValue[T any](value T) SoundboardNullable[T] {
	return SoundboardNullable[T]{value: value, set: true}
}

// SoundboardNull returns a nullable soundboard field that encodes as JSON null.
func SoundboardNull[T any]() SoundboardNullable[T] {
	return SoundboardNullable[T]{set: true, null: true}
}

// IsZero reports whether the field should be omitted from a JSON request.
func (value SoundboardNullable[T]) IsZero() bool {
	return !value.set
}

// IsSet reports whether the field will be included in a JSON request.
func (value SoundboardNullable[T]) IsSet() bool {
	return value.set
}

// IsNull reports whether the field is explicitly set to JSON null.
func (value SoundboardNullable[T]) IsNull() bool {
	return value.set && value.null
}

// Get returns the field value and true when it is set and non-null.
func (value SoundboardNullable[T]) Get() (T, bool) {
	return value.value, value.set && !value.null
}

// MarshalJSON implements json.Marshaler.
func (value SoundboardNullable[T]) MarshalJSON() ([]byte, error) {
	if !value.set || value.null {
		return []byte("null"), nil
	}
	return json.Marshal(value.value)
}

// UnmarshalJSON implements json.Unmarshaler.
func (value *SoundboardNullable[T]) UnmarshalJSON(data []byte) error {
	if value == nil {
		return errors.New("cannot unmarshal soundboard nullable into nil receiver")
	}
	value.set = true
	if bytes.Equal(data, []byte("null")) {
		var zero T
		value.value = zero
		value.null = true
		return nil
	}
	value.null = false
	return json.Unmarshal(data, &value.value)
}

// SoundboardSoundSendParams contains fields accepted when playing a
// soundboard sound in a voice channel.
type SoundboardSoundSendParams struct {
	SoundID       string                     `json:"sound_id"`
	SourceGuildID SoundboardNullable[string] `json:"source_guild_id,omitzero"`
}

// SoundboardSoundCreateParams contains fields accepted when creating a guild
// soundboard sound. Sound must be an MP3 or Ogg base64 data URI.
type SoundboardSoundCreateParams struct {
	Name      string                      `json:"name"`
	Sound     string                      `json:"sound"`
	Volume    SoundboardNullable[float64] `json:"volume,omitzero"`
	EmojiID   SoundboardNullable[string]  `json:"emoji_id,omitzero"`
	EmojiName SoundboardNullable[string]  `json:"emoji_name,omitzero"`
}

// SoundboardSoundEditParams contains fields accepted when modifying a guild
// soundboard sound. A zero field is omitted; SoundboardNull explicitly clears
// a nullable field.
type SoundboardSoundEditParams struct {
	Name      *string                     `json:"name,omitempty"`
	Volume    SoundboardNullable[float64] `json:"volume,omitzero"`
	EmojiID   SoundboardNullable[string]  `json:"emoji_id,omitzero"`
	EmojiName SoundboardNullable[string]  `json:"emoji_name,omitzero"`
}

// SoundboardSoundID represents the sound_id union used by Voice Channel Effect
// Send events. Discord sends guild sound IDs as snowflake strings and may send
// default sound IDs as JSON integers.
type SoundboardSoundID struct {
	Value     string
	IsInteger bool
}

// String returns the decimal sound ID without its JSON representation.
func (id SoundboardSoundID) String() string {
	return id.Value
}

// UnmarshalJSON decodes either a snowflake string or a non-negative integer.
func (id *SoundboardSoundID) UnmarshalJSON(data []byte) error {
	if id == nil {
		return fmt.Errorf("cannot unmarshal soundboard sound ID into nil receiver")
	}
	if bytes.Equal(data, []byte("null")) {
		*id = SoundboardSoundID{}
		return nil
	}

	if len(data) > 0 && data[0] == '"' {
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("unmarshal soundboard sound ID string: %w", err)
		}
		if value == "" {
			return fmt.Errorf("soundboard sound ID string cannot be empty")
		}
		id.Value = value
		id.IsInteger = false
		return nil
	}

	value := string(data)
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return fmt.Errorf("unmarshal soundboard sound ID integer %q: %w", value, err)
	}
	id.Value = value
	id.IsInteger = true
	return nil
}

// MarshalJSON preserves whether Discord supplied the sound ID as a string or
// integer.
func (id SoundboardSoundID) MarshalJSON() ([]byte, error) {
	if id.Value == "" {
		return []byte("null"), nil
	}
	if !id.IsInteger {
		return json.Marshal(id.Value)
	}
	if _, err := strconv.ParseUint(id.Value, 10, 64); err != nil {
		return nil, fmt.Errorf("marshal soundboard sound ID integer %q: %w", id.Value, err)
	}
	return []byte(id.Value), nil
}

func validateSoundboardSnowflake(name, value string) error {
	if value == "" {
		return fmt.Errorf("%w: %s must not be empty", ErrSoundboardValidation, name)
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return fmt.Errorf("%w: %s must be a decimal snowflake", ErrSoundboardValidation, name)
		}
	}
	if _, err := strconv.ParseUint(value, 10, 64); err != nil {
		return fmt.Errorf("%w: %s must be a decimal snowflake", ErrSoundboardValidation, name)
	}
	return nil
}

func validateSoundboardName(name string) error {
	length := utf8.RuneCountInString(name)
	if !utf8.ValidString(name) || length < soundboardSoundNameMinLength || length > soundboardSoundNameMaxLength {
		return fmt.Errorf(
			"%w: name length must be between %d and %d characters",
			ErrSoundboardValidation,
			soundboardSoundNameMinLength,
			soundboardSoundNameMaxLength,
		)
	}
	return nil
}

func validateSoundboardVolume(volume SoundboardNullable[float64]) error {
	value, ok := volume.Get()
	if !ok {
		return nil
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return fmt.Errorf("%w: volume must be between 0 and 1", ErrSoundboardValidation)
	}
	return nil
}

func validateSoundboardEmoji(
	emojiID SoundboardNullable[string],
	emojiName SoundboardNullable[string],
) error {
	customEmojiID, hasCustomEmoji := emojiID.Get()
	unicodeEmoji, hasUnicodeEmoji := emojiName.Get()
	if hasCustomEmoji {
		if err := validateSoundboardSnowflake("emoji_id", customEmojiID); err != nil {
			return err
		}
	}
	if hasUnicodeEmoji {
		length := utf8.RuneCountInString(unicodeEmoji)
		if !utf8.ValidString(unicodeEmoji) || length < 1 || length > 32 {
			return fmt.Errorf(
				"%w: emoji_name length must be between 1 and 32 characters",
				ErrSoundboardValidation,
			)
		}
	}
	return nil
}

func validateSoundboardDataURI(sound string) error {
	header, encoded, found := strings.Cut(sound, ",")
	if !found {
		return fmt.Errorf("%w: sound must be a base64 data URI", ErrSoundboardValidation)
	}
	switch strings.ToLower(header) {
	case "data:audio/mpeg;base64", "data:audio/ogg;base64":
	default:
		return fmt.Errorf("%w: sound data URI must contain MP3 or Ogg audio", ErrSoundboardValidation)
	}
	if encoded == "" {
		return fmt.Errorf("%w: sound data URI must not be empty", ErrSoundboardValidation)
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(soundboardSoundMaxFileSize) {
		return fmt.Errorf(
			"%w: decoded sound must not exceed %d bytes",
			ErrSoundboardValidation,
			soundboardSoundMaxFileSize,
		)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("%w: sound data URI contains invalid base64: %v", ErrSoundboardValidation, err)
	}
	if len(decoded) == 0 || len(decoded) > soundboardSoundMaxFileSize {
		return fmt.Errorf(
			"%w: decoded sound size must be between 1 and %d bytes",
			ErrSoundboardValidation,
			soundboardSoundMaxFileSize,
		)
	}
	return nil
}

func validateSoundboardCreate(data *SoundboardSoundCreateParams) error {
	if data == nil {
		return fmt.Errorf("%w: create parameters must not be nil", ErrSoundboardValidation)
	}
	if err := validateSoundboardName(data.Name); err != nil {
		return err
	}
	if err := validateSoundboardDataURI(data.Sound); err != nil {
		return err
	}
	if err := validateSoundboardVolume(data.Volume); err != nil {
		return err
	}
	return validateSoundboardEmoji(data.EmojiID, data.EmojiName)
}

func validateSoundboardEdit(data *SoundboardSoundEditParams) error {
	if data == nil {
		return fmt.Errorf("%w: edit parameters must not be nil", ErrSoundboardValidation)
	}
	if data.Name != nil {
		if err := validateSoundboardName(*data.Name); err != nil {
			return err
		}
	}
	if err := validateSoundboardVolume(data.Volume); err != nil {
		return err
	}
	return validateSoundboardEmoji(data.EmojiID, data.EmojiName)
}

func decodeSoundboardSound(body []byte) (*SoundboardSound, error) {
	var sound SoundboardSound
	if err := unmarshal(body, &sound); err != nil {
		return nil, err
	}
	return &sound, nil
}

// SoundboardDefaultSounds returns the sounds available to all users.
func (s *Session) SoundboardDefaultSounds(options ...RequestOption) ([]*SoundboardSound, error) {
	body, err := s.RequestWithBucketID(
		http.MethodGet,
		EndpointSoundboardDefaultSounds,
		nil,
		EndpointSoundboardDefaultSounds,
		options...,
	)
	if err != nil {
		return nil, err
	}
	var sounds []*SoundboardSound
	if err = unmarshal(body, &sounds); err != nil {
		return nil, err
	}
	return sounds, nil
}

// GuildSoundboardSounds returns all soundboard sounds for a guild.
func (s *Session) GuildSoundboardSounds(guildID string, options ...RequestOption) ([]*SoundboardSound, error) {
	if err := validateSoundboardSnowflake("guild_id", guildID); err != nil {
		return nil, err
	}
	endpoint := EndpointGuildSoundboardSounds(guildID)
	body, err := s.RequestWithBucketID(http.MethodGet, endpoint, nil, endpoint, options...)
	if err != nil {
		return nil, err
	}
	var response struct {
		Items []*SoundboardSound `json:"items"`
	}
	if err = unmarshal(body, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

// GuildSoundboardSound returns a single guild soundboard sound.
func (s *Session) GuildSoundboardSound(guildID, soundID string, options ...RequestOption) (*SoundboardSound, error) {
	if err := validateSoundboardSnowflake("guild_id", guildID); err != nil {
		return nil, err
	}
	if err := validateSoundboardSnowflake("sound_id", soundID); err != nil {
		return nil, err
	}
	endpoint := EndpointGuildSoundboardSound(guildID, soundID)
	body, err := s.RequestWithBucketID(http.MethodGet, endpoint, nil, endpoint, options...)
	if err != nil {
		return nil, err
	}
	return decodeSoundboardSound(body)
}

// GuildSoundboardSoundCreate creates a soundboard sound in a guild.
func (s *Session) GuildSoundboardSoundCreate(
	guildID string,
	data *SoundboardSoundCreateParams,
	options ...RequestOption,
) (*SoundboardSound, error) {
	if err := validateSoundboardSnowflake("guild_id", guildID); err != nil {
		return nil, err
	}
	if err := validateSoundboardCreate(data); err != nil {
		return nil, err
	}
	endpoint := EndpointGuildSoundboardSounds(guildID)
	body, err := s.RequestWithBucketID(http.MethodPost, endpoint, data, endpoint, options...)
	if err != nil {
		return nil, err
	}
	return decodeSoundboardSound(body)
}

// GuildSoundboardSoundEdit modifies a soundboard sound in a guild.
func (s *Session) GuildSoundboardSoundEdit(
	guildID, soundID string,
	data *SoundboardSoundEditParams,
	options ...RequestOption,
) (*SoundboardSound, error) {
	if err := validateSoundboardSnowflake("guild_id", guildID); err != nil {
		return nil, err
	}
	if err := validateSoundboardSnowflake("sound_id", soundID); err != nil {
		return nil, err
	}
	if err := validateSoundboardEdit(data); err != nil {
		return nil, err
	}
	endpoint := EndpointGuildSoundboardSound(guildID, soundID)
	body, err := s.RequestWithBucketID(
		http.MethodPatch,
		endpoint,
		data,
		EndpointGuildSoundboardSounds(guildID),
		options...,
	)
	if err != nil {
		return nil, err
	}
	return decodeSoundboardSound(body)
}

// GuildSoundboardSoundDelete deletes a soundboard sound from a guild.
func (s *Session) GuildSoundboardSoundDelete(guildID, soundID string, options ...RequestOption) error {
	if err := validateSoundboardSnowflake("guild_id", guildID); err != nil {
		return err
	}
	if err := validateSoundboardSnowflake("sound_id", soundID); err != nil {
		return err
	}
	endpoint := EndpointGuildSoundboardSound(guildID, soundID)
	_, err := s.RequestWithBucketID(
		http.MethodDelete,
		endpoint,
		nil,
		EndpointGuildSoundboardSounds(guildID),
		options...,
	)
	return err
}

// ChannelSoundboardSoundSend plays a soundboard sound in a voice channel.
func (s *Session) ChannelSoundboardSoundSend(
	channelID string,
	data *SoundboardSoundSendParams,
	options ...RequestOption,
) error {
	if err := validateSoundboardSnowflake("channel_id", channelID); err != nil {
		return err
	}
	if data == nil {
		return fmt.Errorf("%w: send parameters must not be nil", ErrSoundboardValidation)
	}
	if err := validateSoundboardSnowflake("sound_id", data.SoundID); err != nil {
		return err
	}
	if sourceGuildID, ok := data.SourceGuildID.Get(); ok {
		if err := validateSoundboardSnowflake("source_guild_id", sourceGuildID); err != nil {
			return err
		}
	}
	endpoint := EndpointChannelSoundboardSoundSend(channelID)
	_, err := s.RequestWithBucketID(http.MethodPost, endpoint, data, endpoint, options...)
	return err
}

// VoiceChannelEffectAnimationType identifies the animation accompanying an
// emoji reaction or soundboard effect in a voice channel.
type VoiceChannelEffectAnimationType int

// Voice channel effect animation types.
const (
	VoiceChannelEffectAnimationPremium VoiceChannelEffectAnimationType = 0
	VoiceChannelEffectAnimationBasic   VoiceChannelEffectAnimationType = 1
)
