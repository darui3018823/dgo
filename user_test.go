package dgo

import (
	"encoding/json"
	"testing"
)

func TestUser_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		u    *User
		want string
	}{
		{
			name: "User with a discriminator",
			u: &User{
				Username:      "bob",
				Discriminator: "8192",
			},
			want: "bob#8192",
		},
		{
			name: "User with discriminator set to 0",
			u: &User{
				Username:      "aldiwildan",
				Discriminator: "0",
			},
			want: "aldiwildan",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.u.String(); got != tc.want {
				t.Errorf("User.String() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUser_DisplayName(t *testing.T) {
	t.Run("no global name set", func(t *testing.T) {
		u := &User{
			GlobalName: "",
			Username:   "username",
		}
		if dn := u.DisplayName(); dn != u.Username {
			t.Errorf("User.DisplayName() = %v, want %v", dn, u.Username)
		}
	})
	t.Run("global name set", func(t *testing.T) {
		u := &User{
			GlobalName: "global",
			Username:   "username",
		}
		if dn := u.DisplayName(); dn != u.GlobalName {
			t.Errorf("User.DisplayName() = %v, want %v", dn, u.GlobalName)
		}
	})
}

func TestUserProfileCosmeticsAndPrimaryGuild(t *testing.T) {
	payload := []byte(`{
		"id":"user",
		"avatar_decoration_data":{
			"asset":"a_decoration",
			"sku_id":"decoration-sku"
		},
		"collectibles":{
			"nameplate":{
				"sku_id":"nameplate-sku",
				"asset":"nameplates/twilight/",
				"label":"Twilight",
				"palette":"cobalt"
			}
		},
		"primary_guild":{
			"identity_guild_id":"guild",
			"identity_enabled":true,
			"tag":"DGO",
			"badge":"badge-hash"
		}
	}`)

	var user User
	if err := json.Unmarshal(payload, &user); err != nil {
		t.Fatal(err)
	}
	if user.AvatarDecorationData == nil ||
		user.AvatarDecorationData.Asset != "a_decoration" ||
		user.AvatarDecorationData.SKUID != "decoration-sku" {
		t.Fatalf("unexpected avatar decoration: %#v", user.AvatarDecorationData)
	}
	if user.Collectibles == nil ||
		user.Collectibles.Nameplate == nil ||
		user.Collectibles.Nameplate.Palette != NameplatePaletteCobalt {
		t.Fatalf("unexpected collectibles: %#v", user.Collectibles)
	}
	if user.PrimaryGuild == nil ||
		user.PrimaryGuild.IdentityGuildID == nil ||
		*user.PrimaryGuild.IdentityGuildID != "guild" ||
		user.PrimaryGuild.IdentityEnabled == nil ||
		!*user.PrimaryGuild.IdentityEnabled {
		t.Fatalf("unexpected primary guild: %#v", user.PrimaryGuild)
	}

	var withoutCosmetics User
	if err := json.Unmarshal([]byte(`{
		"id":"user",
		"avatar_decoration_data":null,
		"collectibles":null,
		"primary_guild":null
	}`), &withoutCosmetics); err != nil {
		t.Fatal(err)
	}
	if withoutCosmetics.AvatarDecorationData != nil ||
		withoutCosmetics.Collectibles != nil ||
		withoutCosmetics.PrimaryGuild != nil {
		t.Fatalf("nullable fields were not preserved: %#v", withoutCosmetics)
	}
}

func TestMemberProfileCosmetics(t *testing.T) {
	var member Member
	if err := json.Unmarshal([]byte(`{
		"avatar_decoration_data":{"asset":"a_member","sku_id":"sku"},
		"collectibles":{"nameplate":{"sku_id":"np","asset":"asset","label":"","palette":"berry"}}
	}`), &member); err != nil {
		t.Fatal(err)
	}
	if member.AvatarDecorationData == nil ||
		member.AvatarDecorationData.Asset != "a_member" ||
		member.Collectibles == nil ||
		member.Collectibles.Nameplate == nil ||
		member.Collectibles.Nameplate.Palette != NameplatePaletteBerry {
		t.Fatalf("unexpected member profile cosmetics: %#v", member)
	}
}
