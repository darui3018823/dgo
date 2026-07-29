// Discordgo - Discord bindings for Go
// Available at https://github.com/darui3018823/dgo

// Copyright 2015-2016 Bruce Marriner <bruce@sqls.net>.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This file contains functions related to Discord OAuth2 endpoints

package dgo

import "errors"

// ------------------------------------------------------------------------------------------------
// Code specific to Discord OAuth2 Applications
// ------------------------------------------------------------------------------------------------

// The MembershipState represents whether the user is in the team or has been invited into it
type MembershipState int

// Constants for the different stages of the MembershipState
const (
	MembershipStateInvited  MembershipState = 1
	MembershipStateAccepted MembershipState = 2
)

// A TeamMember struct stores values for a single Team Member, extending the normal User data - note that the user field is partial
type TeamMember struct {
	User            *User           `json:"user"`
	TeamID          string          `json:"team_id"`
	MembershipState MembershipState `json:"membership_state"`
	Permissions     []string        `json:"permissions"`
}

// A Team struct stores the members of a Discord Developer Team as well as some metadata about it
type Team struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Icon        string        `json:"icon"`
	OwnerID     string        `json:"owner_user_id"`
	Members     []*TeamMember `json:"members"`
}

// Application formerly returned a specific OAuth2 application.
//
// Deprecated: use CurrentApplication for the bot's application.
//
//	appID : The ID of an Application
func (s *Session) Application(appID string) (st *Application, err error) {
	return nil, ErrOAuthApplicationCRUDUnsupported
}

// Applications formerly returned all OAuth2 applications for a user.
//
// Deprecated: this is not a public bot API operation.
func (s *Session) Applications() (st []*Application, err error) {
	return nil, ErrOAuthApplicationCRUDUnsupported
}

// ApplicationCreate formerly created an OAuth2 application.
//
// Deprecated: this is not a public bot API operation.
//	name : Name of Application / Bot
//	uris : Redirect URIs (Not required)
func (s *Session) ApplicationCreate(ap *Application) (st *Application, err error) {
	return nil, ErrOAuthApplicationCRUDUnsupported
}

// ApplicationUpdate formerly updated an OAuth2 application.
//
// Deprecated: use CurrentApplicationEdit.
//	var : desc
func (s *Session) ApplicationUpdate(appID string, ap *Application) (st *Application, err error) {
	return nil, ErrOAuthApplicationCRUDUnsupported
}

// ApplicationDelete formerly deleted an OAuth2 application.
//
// Deprecated: this is not a public bot API operation.
//	appID : The ID of an Application
func (s *Session) ApplicationDelete(appID string) (err error) {
	return ErrOAuthApplicationCRUDUnsupported
}

// CurrentApplication returns the application associated with the bot token.
func (s *Session) CurrentApplication(options ...RequestOption) (*Application, error) {
	body, err := s.RequestWithBucketID("GET", EndpointCurrentApplication, nil, EndpointCurrentApplication, options...)
	if err != nil {
		return nil, err
	}
	application := &Application{}
	if err = unmarshal(body, application); err != nil {
		return nil, err
	}
	return application, nil
}

// CurrentApplicationEdit edits the application associated with the bot token.
func (s *Session) CurrentApplicationEdit(edit *ApplicationEdit, options ...RequestOption) (*Application, error) {
	if edit == nil {
		return nil, errors.New("application edit must not be nil")
	}
	body, err := s.RequestWithBucketID("PATCH", EndpointCurrentApplication, edit, EndpointCurrentApplication, options...)
	if err != nil {
		return nil, err
	}
	application := &Application{}
	if err = unmarshal(body, application); err != nil {
		return nil, err
	}
	return application, nil
}

// Asset struct stores values for an asset of an application
type Asset struct {
	Type int    `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ApplicationAssets returns an application's assets
func (s *Session) ApplicationAssets(appID string) (ass []*Asset, err error) {

	body, err := s.RequestWithBucketID("GET", EndpointOAuth2ApplicationAssets(appID), nil, EndpointOAuth2ApplicationAssets(""))
	if err != nil {
		return
	}

	err = unmarshal(body, &ass)
	return
}

// ------------------------------------------------------------------------------------------------
// Code specific to Discord OAuth2 Application Bots
// ------------------------------------------------------------------------------------------------

// ApplicationBotCreate creates an Application Bot Account
//
//	appID : The ID of an Application
//
// NOTE: func name may change, if I can think up something better.
func (s *Session) ApplicationBotCreate(appID string) (st *User, err error) {

	body, err := s.RequestWithBucketID("POST", EndpointOAuth2ApplicationsBot(appID), nil, EndpointOAuth2ApplicationsBot(""))
	if err != nil {
		return
	}

	err = unmarshal(body, &st)
	return
}
