package dgo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultAllowedMentionsDisableParsing(t *testing.T) {
	s, err := New("Bot token")
	if err != nil {
		t.Fatal(err)
	}
	mentions, err := s.resolveAllowedMentions(nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(&MessageSend{Content: "hello <@1>", AllowedMentions: mentions})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"allowed_mentions":{"parse":[]`) {
		t.Fatalf("message JSON does not disable mention parsing: %s", data)
	}

	mentions.Parse = append(mentions.Parse, AllowedMentionTypeEveryone)
	if len(s.AllowedMentions.Parse) != 0 {
		t.Fatal("resolved allowed mentions mutated the session default")
	}
}

func TestValidateAllowedMentions(t *testing.T) {
	oneHundredOne := make([]string, 101)
	tests := map[string]*MessageAllowedMentions{
		"roles parse and IDs": {
			Parse: []AllowedMentionType{AllowedMentionTypeRoles},
			Roles: []string{"1"},
		},
		"users parse and IDs": {
			Parse: []AllowedMentionType{AllowedMentionTypeUsers},
			Users: []string{"1"},
		},
		"too many roles": {
			Roles: oneHundredOne,
		},
		"too many users": {
			Users: oneHundredOne,
		},
		"duplicate parse": {
			Parse: []AllowedMentionType{AllowedMentionTypeEveryone, AllowedMentionTypeEveryone},
		},
		"unknown parse": {
			Parse: []AllowedMentionType{"channels"},
		},
	}
	for name, mentions := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateAllowedMentions(mentions); err == nil {
				t.Fatal("validateAllowedMentions succeeded")
			}
		})
	}
}

func TestContentWithMoreMentionsReplaced(t *testing.T) {
	s := &Session{StateEnabled: true, State: NewState()}

	user := &User{
		ID:       "user",
		Username: "User Name",
	}

	s.State.GuildAdd(&Guild{ID: "guild"})
	s.State.RoleAdd("guild", &Role{
		ID:          "role",
		Name:        "Role Name",
		Mentionable: true,
	})
	s.State.MemberAdd(&Member{
		User:    user,
		Nick:    "User Nick",
		GuildID: "guild",
	})
	s.State.ChannelAdd(&Channel{
		Name:    "Channel Name",
		GuildID: "guild",
		ID:      "channel",
	})
	m := &Message{
		Content:      "<@&role> <@!user> <@user> <#channel>",
		ChannelID:    "channel",
		MentionRoles: []string{"role"},
		Mentions:     []*User{user},
	}
	if result, _ := m.ContentWithMoreMentionsReplaced(s); result != "@Role Name @User Nick @User Name #Channel Name" {
		t.Error(result)
	}
}
func TestGettingEmojisFromMessage(t *testing.T) {
	msg := "test test <:kitty14:811736565172011058> <:kitty4:811736468812595260>"
	m := &Message{
		Content: msg,
	}
	emojis := m.GetCustomEmojis()
	if len(emojis) < 1 {
		t.Error("No emojis found.")
		return
	}

}

func TestMessage_Reference(t *testing.T) {
	m := &Message{
		ID:        "811736565172011001",
		GuildID:   "811736565172011002",
		ChannelID: "811736565172011003",
	}

	ref := m.Reference()

	if ref.Type != 0 {
		t.Error("Default reference type should be 0")
	}

	if ref.MessageID != m.ID {
		t.Error("Message ID should be the same")
	}

	if ref.GuildID != m.GuildID {
		t.Error("Guild ID should be the same")
	}

	if ref.ChannelID != m.ChannelID {
		t.Error("Channel ID should be the same")
	}
}

func TestMessage_Forward(t *testing.T) {
	m := &Message{
		ID:        "811736565172011001",
		GuildID:   "811736565172011002",
		ChannelID: "811736565172011003",
	}

	ref := m.Forward()

	if ref.Type != MessageReferenceTypeForward {
		t.Error("Reference type should be 1 (forward)")
	}

	if ref.MessageID != m.ID {
		t.Error("Message ID should be the same")
	}

	if ref.GuildID != m.GuildID {
		t.Error("Guild ID should be the same")
	}

	if ref.ChannelID != m.ChannelID {
		t.Error("Channel ID should be the same")
	}
}

func TestMessageReference_DefaultTypeIsDefault(t *testing.T) {
	r := MessageReference{}
	if r.Type != MessageReferenceTypeDefault {
		t.Error("Default message type should be MessageReferenceTypeDefault")
	}
}
