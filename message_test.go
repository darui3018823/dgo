package dgo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMessageReactionDetails(t *testing.T) {
	var message Message
	if err := json.Unmarshal([]byte(`{
		"reactions":[{
			"count":3,
			"count_details":{"burst":1,"normal":2},
			"me":true,
			"me_burst":true,
			"emoji":{"id":null,"name":"🔥"},
			"burst_colors":["#FF0000","#00FF00"]
		}]
	}`), &message); err != nil {
		t.Fatal(err)
	}
	if len(message.Reactions) != 1 {
		t.Fatalf("reactions = %d", len(message.Reactions))
	}
	reaction := message.Reactions[0]
	if reaction.CountDetails == nil ||
		reaction.CountDetails.Burst != 1 ||
		reaction.CountDetails.Normal != 2 ||
		!reaction.MeBurst ||
		len(reaction.BurstColors) != 2 {
		t.Fatalf("unexpected reaction: %#v", reaction)
	}
}

func TestGatewayMessageReactionBurstFields(t *testing.T) {
	var reaction MessageReactionAdd
	if err := json.Unmarshal([]byte(`{
		"user_id":"reactor",
		"message_id":"message",
		"message_author_id":"author",
		"channel_id":"channel",
		"emoji":{"name":"🔥"},
		"burst":true,
		"burst_colors":["#FF0000"],
		"type":1
	}`), &reaction); err != nil {
		t.Fatal(err)
	}
	if reaction.MessageReaction == nil ||
		reaction.MessageAuthorID != "author" ||
		!reaction.Burst ||
		reaction.Type != ReactionTypeBurst ||
		len(reaction.BurstColors) != 1 {
		t.Fatalf("unexpected gateway reaction: %#v", reaction.MessageReaction)
	}
}

func TestCurrentMessageTypes(t *testing.T) {
	tests := map[MessageType]int{
		MessageTypeGuildDiscoveryGracePeriodInitialWarning: 16,
		MessageTypeGuildDiscoveryGracePeriodFinalWarning:   17,
		MessageTypeGuildInviteReminder:                     22,
		MessageTypeAutoModerationAction:                    24,
		MessageTypeRoleSubscriptionPurchase:                25,
		MessageTypeInteractionPremiumUpsell:                26,
		MessageTypeStageStart:                              27,
		MessageTypeStageEnd:                                28,
		MessageTypeStageSpeaker:                            29,
		MessageTypeStageTopic:                              31,
		MessageTypeGuildApplicationPremiumSubscription:     32,
		MessageTypeGuildIncidentAlertModeEnabled:           36,
		MessageTypeGuildIncidentAlertModeDisabled:          37,
		MessageTypeGuildIncidentReportRaid:                 38,
		MessageTypeGuildIncidentReportFalseAlarm:           39,
		MessageTypePurchaseNotification:                    44,
		MessageTypePollResult:                              46,
	}
	for messageType, want := range tests {
		if int(messageType) != want {
			t.Errorf("message type = %d, want %d", messageType, want)
		}
	}
	if MessageFlagsHasSnapshot != 1<<14 {
		t.Fatalf("MessageFlagsHasSnapshot = %#x", MessageFlagsHasSnapshot)
	}
	if EmbedTypePollResult != "poll_result" {
		t.Fatalf("EmbedTypePollResult = %q", EmbedTypePollResult)
	}
}

func TestCurrentMessageFields(t *testing.T) {
	payload := []byte(`{
		"id":"message",
		"channel_id":"channel",
		"type":46,
		"nonce":9007199254740993,
		"application_id":"app",
		"stickers":[{"id":"sticker","name":"Dgo","format_type":1}],
		"position":0,
		"role_subscription_data":{
			"role_subscription_listing_id":"listing",
			"tier_name":"Gold",
			"total_months_subscribed":12,
			"is_renewal":true
		},
		"resolved":{
			"users":{"user":{"id":"user"}},
			"members":{},
			"roles":{},
			"channels":{},
			"messages":{},
			"attachments":{}
		},
		"call":{"participants":["user"],"ended_timestamp":null},
		"shared_client_theme":{
			"colors":["5865F2","7258F2"],
			"gradient_angle":45,
			"base_mix":58,
			"base_theme":4
		},
		"interaction_metadata":{
			"id":"interaction",
			"type":2,
			"user":{"id":"user"},
			"authorizing_integration_owners":{"0":"guild"},
			"target_user":{"id":"target"},
			"target_message_id":"target-message"
		}
	}`)

	var message Message
	if err := json.Unmarshal(payload, &message); err != nil {
		t.Fatal(err)
	}
	if string(message.Nonce) != "9007199254740993" {
		t.Fatalf("nonce lost integer precision: %s", message.Nonce)
	}
	if message.ApplicationID != "app" ||
		len(message.Stickers) != 1 ||
		message.Position == nil ||
		*message.Position != 0 {
		t.Fatalf("unexpected application/sticker/position fields: %#v", message)
	}
	if message.RoleSubscriptionData == nil ||
		message.RoleSubscriptionData.RoleSubscriptionListingID != "listing" ||
		!message.RoleSubscriptionData.IsRenewal {
		t.Fatalf("unexpected role subscription data: %#v", message.RoleSubscriptionData)
	}
	if message.Resolved == nil ||
		message.Resolved.Users["user"] == nil ||
		message.Call == nil ||
		message.Call.EndedTimestamp != nil {
		t.Fatalf("unexpected resolved/call fields: %#v", message)
	}
	if message.SharedClientTheme == nil ||
		message.SharedClientTheme.BaseTheme == nil ||
		*message.SharedClientTheme.BaseTheme != SharedClientThemeBaseMidnight {
		t.Fatalf("unexpected shared client theme: %#v", message.SharedClientTheme)
	}
	if message.InteractionMetadata == nil ||
		message.InteractionMetadata.TargetUser == nil ||
		message.InteractionMetadata.TargetUser.ID != "target" ||
		message.InteractionMetadata.TargetMessageID != "target-message" {
		t.Fatalf("unexpected interaction metadata: %#v", message.InteractionMetadata)
	}

	var nullable Message
	if err := json.Unmarshal([]byte(`{
		"nonce":"client-nonce",
		"shared_client_theme":{
			"colors":["5865F2"],
			"gradient_angle":0,
			"base_mix":0,
			"base_theme":null
		}
	}`), &nullable); err != nil {
		t.Fatal(err)
	}
	if string(nullable.Nonce) != `"client-nonce"` ||
		nullable.SharedClientTheme == nil ||
		nullable.SharedClientTheme.BaseTheme != nil {
		t.Fatalf("nullable/string union fields were not preserved: %#v", nullable)
	}
}

func TestMessageSendCurrentFields(t *testing.T) {
	base := SharedClientThemeBaseDark
	data, err := json.Marshal(MessageSend{
		Nonce:        "client-nonce",
		EnforceNonce: true,
		SharedClientTheme: &SharedClientTheme{
			Colors:        []string{"5865F2"},
			GradientAngle: 90,
			BaseMix:       50,
			BaseTheme:     &base,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, field := range []string{`"nonce":"client-nonce"`, `"enforce_nonce":true`, `"shared_client_theme"`} {
		if !strings.Contains(got, field) {
			t.Fatalf("message send JSON missing %s: %s", field, got)
		}
	}
}

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
