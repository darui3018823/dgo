package dgo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
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

// VoiceChannelEffectAnimationType identifies the animation accompanying an
// emoji reaction or soundboard effect in a voice channel.
type VoiceChannelEffectAnimationType int

// Voice channel effect animation types.
const (
	VoiceChannelEffectAnimationPremium VoiceChannelEffectAnimationType = 0
	VoiceChannelEffectAnimationBasic   VoiceChannelEffectAnimationType = 1
)
