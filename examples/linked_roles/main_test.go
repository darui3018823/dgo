package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestLinkedRolesStartSetsSecureStateCookie(t *testing.T) {
	config := testOAuthConfig()
	server := newLinkedRolesServer(config, "app-id", log.New(io.Discard, "", 0))
	request := httptest.NewRequest(http.MethodGet, "/linked-roles", nil)
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSeeOther)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}

	cookie := findResponseCookie(t, response.Result().Cookies(), oauthStateCookieName)
	if cookie.Value == "" {
		t.Fatal("OAuth state cookie is empty")
	}
	if !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("cookie flags HttpOnly=%v Secure=%v, want both true", cookie.HttpOnly, cookie.Secure)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want %v", cookie.SameSite, http.SameSiteLaxMode)
	}
	if cookie.Path != oauthCallbackPath {
		t.Fatalf("Path = %q, want %q", cookie.Path, oauthCallbackPath)
	}
	if cookie.MaxAge != int(oauthStateLifetime/time.Second) {
		t.Fatalf("MaxAge = %d, want %d", cookie.MaxAge, int(oauthStateLifetime/time.Second))
	}
	if remaining := time.Until(cookie.Expires); remaining <= 0 || remaining > oauthStateLifetime+time.Second {
		t.Fatalf("cookie lifetime = %v, want within (0, %v]", remaining, oauthStateLifetime)
	}

	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if got := location.Query().Get("state"); got != cookie.Value {
		t.Fatalf("redirect state = %q, cookie state = %q", got, cookie.Value)
	}
}

func TestLinkedRolesCallbackRejectsInvalidInputWithoutPanicking(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		cookies []*http.Cookie
	}{
		{name: "missing code", target: oauthCallbackPath + "?state=expected", cookies: stateCookies("expected")},
		{name: "missing state", target: oauthCallbackPath + "?code=code", cookies: stateCookies("expected")},
		{name: "empty code", target: oauthCallbackPath + "?code=&state=expected", cookies: stateCookies("expected")},
		{name: "multiple codes", target: oauthCallbackPath + "?code=one&code=two&state=expected", cookies: stateCookies("expected")},
		{name: "multiple states", target: oauthCallbackPath + "?code=code&state=one&state=two", cookies: stateCookies("expected")},
		{name: "missing cookie", target: oauthCallbackPath + "?code=code&state=expected"},
		{name: "empty cookie", target: oauthCallbackPath + "?code=code&state=expected", cookies: stateCookies("")},
		{name: "multiple cookies", target: oauthCallbackPath + "?code=code&state=expected", cookies: append(stateCookies("expected"), stateCookies("expected")...)},
		{name: "state mismatch", target: oauthCallbackPath + "?code=code&state=unexpected", cookies: stateCookies("expected")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := testLinkedRolesServer()
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			for _, cookie := range test.cookies {
				request.AddCookie(cookie)
			}
			response := httptest.NewRecorder()

			server.routes().ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if got := strings.TrimSpace(response.Body.String()); got != "Invalid OAuth callback." {
				t.Fatalf("body = %q, want generic callback error", got)
			}
		})
	}
}

func TestLinkedRolesCallbackClearsStateAndUpdatesConnection(t *testing.T) {
	server := testLinkedRolesServer()
	server.exchangeCode = func(_ context.Context, code string) (string, error) {
		if code != "authorization-code" {
			t.Fatalf("code = %q, want authorization-code", code)
		}
		return "access-token", nil
	}
	server.updateRoleConnection = func(_ context.Context, applicationID, accessToken string) (map[string]string, error) {
		if applicationID != "app-id" {
			t.Fatalf("applicationID = %q, want app-id", applicationID)
		}
		if accessToken != "access-token" {
			t.Fatalf("accessToken = %q, want access-token", accessToken)
		}
		return map[string]string{"gopher": "1"}, nil
	}

	request := httptest.NewRequest(
		http.MethodGet,
		oauthCallbackPath+"?code=authorization-code&state=expected-state",
		nil,
	)
	request.AddCookie(stateCookies("expected-state")[0])
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if !strings.Contains(response.Body.String(), `"gopher":"1"`) {
		t.Fatalf("body = %q, want metadata", response.Body.String())
	}

	cookie := findResponseCookie(t, response.Result().Cookies(), oauthStateCookieName)
	if cookie.MaxAge >= 0 {
		t.Fatalf("cleared cookie MaxAge = %d, want negative", cookie.MaxAge)
	}
	if !cookie.Expires.Before(time.Now()) {
		t.Fatalf("cleared cookie Expires = %v, want past time", cookie.Expires)
	}
	if cookie.Path != oauthCallbackPath || !cookie.HttpOnly || !cookie.Secure ||
		cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cleared cookie security attributes were not preserved: %+v", cookie)
	}
}

func TestLinkedRolesCallbackDoesNotExposeInternalErrors(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*linkedRolesServer)
	}{
		{
			name: "exchange error",
			configure: func(server *linkedRolesServer) {
				server.exchangeCode = func(context.Context, string) (string, error) {
					return "", errors.New("exchange failed with client_secret=do-not-expose")
				}
			},
		},
		{
			name: "Discord API error",
			configure: func(server *linkedRolesServer) {
				server.updateRoleConnection = func(context.Context, string, string) (map[string]string, error) {
					return nil, errors.New("API rejected access_token=do-not-expose")
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := testLinkedRolesServer()
			test.configure(server)
			request := httptest.NewRequest(
				http.MethodGet,
				oauthCallbackPath+"?code=authorization-code&state=expected-state",
				nil,
			)
			request.AddCookie(stateCookies("expected-state")[0])
			response := httptest.NewRecorder()

			server.routes().ServeHTTP(response, request)

			if response.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusBadGateway)
			}
			body := response.Body.String()
			if strings.Contains(body, "do-not-expose") || strings.Contains(body, "access_token") ||
				strings.Contains(body, "client_secret") {
				t.Fatalf("response exposed internal error: %q", body)
			}
			if !strings.Contains(body, "Unable to complete linked-role authorization.") {
				t.Fatalf("body = %q, want generic completion error", body)
			}

			cookie := findResponseCookie(t, response.Result().Cookies(), oauthStateCookieName)
			if cookie.MaxAge >= 0 {
				t.Fatalf("state cookie was not cleared after validation: %+v", cookie)
			}
		})
	}
}

func TestLinkedRolesStartHandlesRandomFailure(t *testing.T) {
	server := testLinkedRolesServer()
	server.random = errorReader{}
	request := httptest.NewRequest(http.MethodGet, "/linked-roles", nil)
	response := httptest.NewRecorder()

	server.routes().ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "random failure") {
		t.Fatalf("response exposed internal error: %q", response.Body.String())
	}
}

func TestLinkedRolesHandlersRejectNonGETMethods(t *testing.T) {
	for _, path := range []string{"/linked-roles", oauthCallbackPath} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			response := httptest.NewRecorder()

			testLinkedRolesServer().routes().ServeHTTP(response, request)

			if response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
			}
			if got := response.Header().Get("Allow"); got != http.MethodGet {
				t.Fatalf("Allow = %q, want GET", got)
			}
		})
	}
}

func TestHTTPServerHasDefensiveTimeouts(t *testing.T) {
	server := newHTTPServer(":8010", http.NewServeMux())

	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 ||
		server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("server timeouts must all be positive: %+v", server)
	}
}

func testLinkedRolesServer() *linkedRolesServer {
	server := newLinkedRolesServer(testOAuthConfig(), "app-id", log.New(io.Discard, "", 0))
	server.exchangeCode = func(context.Context, string) (string, error) {
		return "access-token", nil
	}
	server.updateRoleConnection = func(context.Context, string, string) (map[string]string, error) {
		return map[string]string{"gopher": "1"}, nil
	}
	return server
}

func testOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:    "client-id",
		RedirectURL: "https://example.com" + oauthCallbackPath,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://discord.com/oauth2/authorize",
			TokenURL: "https://discord.com/api/oauth2/token",
		},
	}
}

func stateCookies(value string) []*http.Cookie {
	return []*http.Cookie{{
		Name:  oauthStateCookieName,
		Value: value,
		Path:  oauthCallbackPath,
	}}
}

func findResponseCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("response cookie %q not found", name)
	return nil
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("random failure")
}
