// dgo - Discord bindings for Go
// Available at https://github.com/darui3018823/dgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dgo

import (
	"encoding/json"
	"errors"
	"net/url"
)

// ApplicationIdentity identifies a user's external account for an application.
type ApplicationIdentity struct {
	UserID               string `json:"user_id"`
	ProviderType         string `json:"provider_type"`
	ProviderID           string `json:"provider_id,omitempty"`
	ProviderIssuedUserID string `json:"provider_issued_user_id"`
}

// ApplicationIdentityProfile contains the game stats displayed on a user's
// Discord profile.
type ApplicationIdentityProfile struct {
	Username *string                         `json:"username"`
	Metadata json.RawMessage                 `json:"metadata"`
	Data     *ApplicationIdentityProfileData `json:"data"`
}

// ApplicationIdentityProfileData contains predefined and custom profile data.
type ApplicationIdentityProfileData struct {
	Primary *ApplicationIdentityPrimaryProfileData `json:"primary,omitempty"`
	Dynamic []ApplicationIdentityDynamicField      `json:"dynamic,omitempty"`
}

// ApplicationIdentityPrimaryProfileData contains Discord's predefined game
// stat fields. Pointer fields preserve the distinction between zero and an
// omitted value when constructing a PATCH request.
type ApplicationIdentityPrimaryProfileData struct {
	Season                       *string                   `json:"season,omitempty"`
	RankName                     *string                   `json:"rank_name,omitempty"`
	RankImage                    *ApplicationIdentityMedia `json:"rank_image,omitempty"`
	HighestRank                  *string                   `json:"highest_rank,omitempty"`
	HighestRankImage             *ApplicationIdentityMedia `json:"highest_rank_image,omitempty"`
	FeaturedPlayedCharacter      *string                   `json:"featured_played_character,omitempty"`
	FeaturedPlayedCharacterImage *ApplicationIdentityMedia `json:"featured_played_character_image,omitempty"`
	PlaytimeHours                *float64                  `json:"playtime_hours,omitempty"`
	TotalWins                    *int                      `json:"total_wins,omitempty"`
	CurrentPeriodWins            *int                      `json:"current_period_wins,omitempty"`
	TotalGames                   *int                      `json:"total_games,omitempty"`
	CurrentPeriodGames           *int                      `json:"current_period_games,omitempty"`
	TotalKills                   *int                      `json:"total_kills,omitempty"`
	CurrentPeriodKills           *int                      `json:"current_period_kills,omitempty"`
	TotalAssists                 *int                      `json:"total_assists,omitempty"`
	CurrentPeriodAssists         *int                      `json:"current_period_assists,omitempty"`
	TotalDeaths                  *int                      `json:"total_deaths,omitempty"`
	CurrentPeriodDeaths          *int                      `json:"current_period_deaths,omitempty"`
}

// ApplicationIdentityDynamicFieldType determines the JSON type of a dynamic
// field's Value.
type ApplicationIdentityDynamicFieldType int

const (
	ApplicationIdentityDynamicFieldString ApplicationIdentityDynamicFieldType = 1
	ApplicationIdentityDynamicFieldNumber ApplicationIdentityDynamicFieldType = 2
	ApplicationIdentityDynamicFieldMedia  ApplicationIdentityDynamicFieldType = 3
)

// ApplicationIdentityDynamicField is a custom profile stat. Value is a
// string, number, or ApplicationIdentityMedia according to Type.
type ApplicationIdentityDynamicField struct {
	Type  ApplicationIdentityDynamicFieldType `json:"type"`
	Name  string                              `json:"name"`
	Value any                                 `json:"value"`
}

// ApplicationIdentityMedia references a publicly reachable profile image.
type ApplicationIdentityMedia struct {
	URL string `json:"url"`
}

// ApplicationIdentityProfileEdit is the body accepted by the profile PATCH
// endpoint. Data, when supplied, replaces the complete existing data object.
type ApplicationIdentityProfileEdit struct {
	Username *string                         `json:"username,omitempty"`
	Data     *ApplicationIdentityProfileData `json:"data,omitempty"`
}

// ApplicationIdentityDeleteParams optionally selects a provider-specific
// identity when deleting an application identity.
type ApplicationIdentityDeleteParams struct {
	ProviderID string `json:"provider_id,omitempty"`
}

type applicationIdentitiesResponse struct {
	Identities []*ApplicationIdentity `json:"identities"`
}

func applicationIdentityPathSegment(value string) string {
	return url.PathEscape(value)
}

// ApplicationIdentityProfileUpdate updates a user's application identity
// profile. Discord returns nil, nil when the successful response is 204 No
// Content; the first successful write returns the created profile.
func (s *Session) ApplicationIdentityProfileUpdate(applicationID, userID, providerIssuedUserID string, edit *ApplicationIdentityProfileEdit, options ...RequestOption) (*ApplicationIdentityProfile, error) {
	if edit == nil {
		return nil, errors.New("application identity profile edit must not be nil")
	}

	body, err := s.RequestWithBucketID("PATCH", EndpointApplicationIdentityProfile(applicationID, userID, providerIssuedUserID), edit, applicationIdentityBucket(applicationID), options...)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, nil
	}

	profile := &ApplicationIdentityProfile{}
	if err := unmarshal(body, profile); err != nil {
		return nil, err
	}
	return profile, nil
}

// ApplicationIdentityProfile returns the profile for a user's application
// identity.
func (s *Session) ApplicationIdentityProfile(applicationID, userID, providerIssuedUserID string, options ...RequestOption) (*ApplicationIdentityProfile, error) {
	body, err := s.RequestWithBucketID("GET", EndpointApplicationIdentityProfile(applicationID, userID, providerIssuedUserID), nil, applicationIdentityBucket(applicationID), options...)
	if err != nil {
		return nil, err
	}

	profile := &ApplicationIdentityProfile{}
	if err := unmarshal(body, profile); err != nil {
		return nil, err
	}
	return profile, nil
}

// ApplicationIdentities returns all application identities belonging to a
// Discord user.
func (s *Session) ApplicationIdentities(applicationID, userID string, options ...RequestOption) ([]*ApplicationIdentity, error) {
	body, err := s.RequestWithBucketID("GET", EndpointApplicationIdentitiesByUser(applicationID, userID), nil, applicationIdentityBucket(applicationID), options...)
	if err != nil {
		return nil, err
	}

	response := &applicationIdentitiesResponse{}
	if err := unmarshal(body, response); err != nil {
		return nil, err
	}
	return response.Identities, nil
}

// ApplicationIdentitiesByExternalID resolves application identities using
// their provider type, provider-issued user ID, and optional provider ID.
func (s *Session) ApplicationIdentitiesByExternalID(applicationID, providerType, providerIssuedUserID, providerID string, options ...RequestOption) ([]*ApplicationIdentity, error) {
	endpoint := EndpointApplicationIdentitiesByExternalID(applicationID, providerType, providerIssuedUserID)
	if providerID != "" {
		endpoint += "?provider_id=" + url.QueryEscape(providerID)
	}

	body, err := s.RequestWithBucketID("GET", endpoint, nil, applicationIdentityBucket(applicationID), options...)
	if err != nil {
		return nil, err
	}

	response := &applicationIdentitiesResponse{}
	if err := unmarshal(body, response); err != nil {
		return nil, err
	}
	return response.Identities, nil
}

// ApplicationIdentityDelete deletes an identity and its associated profile
// data. Deletion may be rejected by Discord if it would remove the user's last
// account-linking identity for the application.
func (s *Session) ApplicationIdentityDelete(applicationID, userID, providerType, providerIssuedUserID string, params *ApplicationIdentityDeleteParams, options ...RequestOption) error {
	_, err := s.RequestWithBucketID("DELETE", EndpointApplicationIdentity(applicationID, userID, providerType, providerIssuedUserID), params, applicationIdentityBucket(applicationID), options...)
	return err
}

func applicationIdentityBucket(applicationID string) string {
	return EndpointApplication(applicationID) + "/application-identities"
}
