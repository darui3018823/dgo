package dgo

import (
	"net/http"
	"testing"
)

func TestApplicationIdentityEndpoints(t *testing.T) {
	if got, want := EndpointApplicationIdentityProfile("app", "user", "external/user"), EndpointAPI+"applications/app/users/user/identities/external%2Fuser/profile"; got != want {
		t.Fatalf("profile endpoint = %q, want %q", got, want)
	}
	if got, want := EndpointApplicationIdentitiesByExternalID("app", "steam", "external/user"), EndpointAPI+"applications/app/application-identities/steam/external%2Fuser"; got != want {
		t.Fatalf("external identity endpoint = %q, want %q", got, want)
	}
	if got, want := EndpointApplicationIdentitiesByUser("app", "user"), EndpointAPI+"users/user/application-identities/app"; got != want {
		t.Fatalf("user identities endpoint = %q, want %q", got, want)
	}
	if got, want := EndpointApplicationIdentity("app", "user", "steam", "external/user"), EndpointAPI+"users/user/application-identities/app/steam/external%2Fuser/delete"; got != want {
		t.Fatalf("delete endpoint = %q, want %q", got, want)
	}
}

func TestApplicationIdentityRESTMethods(t *testing.T) {
	profileEdit := &ApplicationIdentityProfileEdit{
		Username: stringPointer("pilot"),
		Data: &ApplicationIdentityProfileData{
			Primary: &ApplicationIdentityPrimaryProfileData{
				TotalWins: stringIntPointer(42),
			},
			Dynamic: []ApplicationIdentityDynamicField{{
				Type:  ApplicationIdentityDynamicFieldNumber,
				Name:  "rating",
				Value: 123.45,
			}},
		},
	}

	tests := []struct {
		name         string
		method       string
		path         string
		query        string
		requestBody  string
		responseCode int
		responseBody string
		call         func(*Session) error
	}{
		{
			name:         "profile update creates profile",
			method:       http.MethodPatch,
			path:         "/api/v10/applications/app/users/user/identities/external-user/profile",
			requestBody:  `{"username":"pilot","data":{"primary":{"total_wins":42},"dynamic":[{"type":2,"name":"rating","value":123.45}]}}`,
			responseCode: http.StatusCreated,
			responseBody: `{"username":"pilot","metadata":null,"data":{"primary":{"total_wins":42}}}`,
			call: func(session *Session) error {
				profile, err := session.ApplicationIdentityProfileUpdate("app", "user", "external-user", profileEdit)
				if err == nil && (profile == nil || profile.Username == nil || *profile.Username != "pilot" || profile.Data == nil) {
					t.Fatalf("unexpected profile: %#v", profile)
				}
				return err
			},
		},
		{
			name:         "profile update accepts no content",
			method:       http.MethodPatch,
			path:         "/api/v10/applications/app/users/user/identities/external-user/profile",
			requestBody:  `{"username":"pilot","data":{"primary":{"total_wins":42},"dynamic":[{"type":2,"name":"rating","value":123.45}]}}`,
			responseCode: http.StatusNoContent,
			call: func(session *Session) error {
				profile, err := session.ApplicationIdentityProfileUpdate("app", "user", "external-user", profileEdit)
				if err == nil && profile != nil {
					t.Fatalf("profile = %#v, want nil for 204", profile)
				}
				return err
			},
		},
		{
			name:         "get profile",
			method:       http.MethodGet,
			path:         "/api/v10/applications/app/users/user/identities/external-user/profile",
			responseCode: http.StatusOK,
			responseBody: `{"username":"pilot","metadata":{"region":"jp"},"data":null}`,
			call: func(session *Session) error {
				profile, err := session.ApplicationIdentityProfile("app", "user", "external-user")
				if err == nil && (profile == nil || profile.Username == nil || *profile.Username != "pilot" || string(profile.Metadata) != `{"region":"jp"}`) {
					t.Fatalf("unexpected profile: %#v", profile)
				}
				return err
			},
		},
		{
			name:         "list identities by user",
			method:       http.MethodGet,
			path:         "/api/v10/users/user/application-identities/app",
			responseCode: http.StatusOK,
			responseBody: `{"identities":[{"user_id":"user","provider_type":"steam","provider_issued_user_id":"external-user"}]}`,
			call: func(session *Session) error {
				identities, err := session.ApplicationIdentities("app", "user")
				if err == nil && (len(identities) != 1 || identities[0].ProviderType != "steam") {
					t.Fatalf("unexpected identities: %#v", identities)
				}
				return err
			},
		},
		{
			name:         "list identities by external id",
			method:       http.MethodGet,
			path:         "/api/v10/applications/app/application-identities/steam/external%2Fuser",
			query:        "provider_id=region%2Fone",
			responseCode: http.StatusOK,
			responseBody: `{"identities":[]}`,
			call: func(session *Session) error {
				identities, err := session.ApplicationIdentitiesByExternalID("app", "steam", "external/user", "region/one")
				if err == nil && identities == nil {
					t.Fatal("identities is nil")
				}
				return err
			},
		},
		{
			name:         "delete identity",
			method:       http.MethodDelete,
			path:         "/api/v10/users/user/application-identities/app/steam/external-user/delete",
			requestBody:  `{"provider_id":"region-one"}`,
			responseCode: http.StatusNoContent,
			call: func(session *Session) error {
				return session.ApplicationIdentityDelete("app", "user", "steam", "external-user", &ApplicationIdentityDeleteParams{ProviderID: "region-one"})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session, err := New("Bot identity-token")
			if err != nil {
				t.Fatal(err)
			}
			session.Client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method != test.method {
					t.Fatalf("method = %q, want %q", request.Method, test.method)
				}
				gotPath := request.URL.EscapedPath()
				if gotPath != test.path {
					t.Fatalf("path = %q, want %q", gotPath, test.path)
				}
				if request.URL.RawQuery != test.query {
					t.Fatalf("query = %q, want %q", request.URL.RawQuery, test.query)
				}
				body, err := readRequestBody(request)
				if err != nil {
					t.Fatal(err)
				}
				assertJSONEqual(t, body, test.requestBody)
				return jsonResponse(test.responseCode, test.responseBody), nil
			})

			if err := test.call(session); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestApplicationIdentityProfileUpdateValidation(t *testing.T) {
	session, err := New("Bot identity-token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.ApplicationIdentityProfileUpdate("app", "user", "external-user", nil); err == nil {
		t.Fatal("nil edit was accepted")
	}
}

func stringIntPointer(value int) *int {
	return &value
}
