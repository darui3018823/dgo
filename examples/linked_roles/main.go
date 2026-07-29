package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/darui3018823/dgo"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
)

const (
	oauthStateCookieName = "oauthstate"
	oauthCallbackPath    = "/linked-roles-callback"
	oauthStateLifetime   = 5 * time.Minute
)

var oauthConfig = oauth2.Config{
	Endpoint: oauth2.Endpoint{
		AuthURL:  "https://discord.com/oauth2/authorize",
		TokenURL: "https://discord.com/api/oauth2/token",
	},
	Scopes: []string{"identify", "role_connections.write"},
}

var (
	appID        = flag.String("app", "", "Application ID")
	token        = flag.String("token", "", "Application token")
	clientSecret = flag.String("secret", "", "OAuth2 secret")
	redirectURL  = flag.String("redirect", "", "OAuth2 Redirect URL")
)

type linkedRolesServer struct {
	oauthConfig          *oauth2.Config
	appID                string
	random               io.Reader
	logger               *log.Logger
	exchangeCode         func(context.Context, string) (string, error)
	updateRoleConnection func(context.Context, string, string) (map[string]string, error)
}

func main() {
	flag.Parse()
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("linked roles: .env file was not loaded: %v", err)
	}
	oauthConfig.ClientID = *appID
	oauthConfig.ClientSecret = *clientSecret
	var err error
	oauthConfig.RedirectURL, err = url.JoinPath(*redirectURL, oauthCallbackPath)
	if err != nil {
		log.Fatalf("build OAuth redirect URL: %v", err)
	}

	s, err := dgo.New("Bot " + *token)
	if err != nil {
		log.Fatalf("create Discord session: %v", err)
	}

	_, err = s.ApplicationRoleConnectionMetadataUpdate(*appID, []*dgo.ApplicationRoleConnectionMetadata{
		{
			Type:                     dgo.ApplicationRoleConnectionMetadataIntegerGreaterThanOrEqual,
			Key:                      "loc",
			Name:                     "Lines of Code",
			NameLocalizations:        map[dgo.Locale]string{},
			Description:              "Total lines of code written",
			DescriptionLocalizations: map[dgo.Locale]string{},
		},
		{
			Type:                     dgo.ApplicationRoleConnectionMetadataBooleanEqual,
			Key:                      "gopher",
			Name:                     "Gopher",
			NameLocalizations:        map[dgo.Locale]string{},
			Description:              "Writes in Go",
			DescriptionLocalizations: map[dgo.Locale]string{},
		},
		{
			Type:                     dgo.ApplicationRoleConnectionMetadataDatetimeGreaterThanOrEqual,
			Key:                      "first_line",
			Name:                     "First line written",
			NameLocalizations:        map[dgo.Locale]string{},
			Description:              "Days since the first line of code",
			DescriptionLocalizations: map[dgo.Locale]string{},
		},
	})
	if err != nil {
		log.Fatalf("update application metadata: %v", err)
	}

	server := newLinkedRolesServer(&oauthConfig, *appID, log.Default())
	httpServer := newHTTPServer(":8010", server.routes())

	log.Printf("updated application metadata; listening on %s", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("linked roles server failed: %v", err)
	}
}

func newLinkedRolesServer(config *oauth2.Config, applicationID string, logger *log.Logger) *linkedRolesServer {
	server := &linkedRolesServer{
		oauthConfig: config,
		appID:       applicationID,
		random:      rand.Reader,
		logger:      logger,
	}
	server.exchangeCode = func(ctx context.Context, code string) (string, error) {
		tokens, err := config.Exchange(ctx, code)
		if err != nil {
			return "", err
		}
		return tokens.AccessToken, nil
	}
	server.updateRoleConnection = updateRoleConnection
	return server
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func (s *linkedRolesServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/linked-roles", s.handleLinkedRoles)
	mux.HandleFunc(oauthCallbackPath, s.handleLinkedRolesCallback)
	return mux
}

func (s *linkedRolesServer) handleLinkedRoles(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}

	state, err := generateStateOAuthCookie(w, s.random, time.Now())
	if err != nil {
		s.logError("generate OAuth state", err)
		http.Error(w, "Unable to start linked-role authorization.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, s.oauthConfig.AuthCodeURL(state), http.StatusSeeOther)
}

func (s *linkedRolesServer) handleLinkedRolesCallback(w http.ResponseWriter, r *http.Request) {
	setNoStoreHeaders(w)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return
	}

	code, codeOK := singleQueryValue(r.URL.Query(), "code")
	state, stateOK := singleQueryValue(r.URL.Query(), "state")
	stateCookie, cookieOK := singleCookie(r, oauthStateCookieName)
	if !codeOK || !stateOK || !cookieOK ||
		subtle.ConstantTimeCompare([]byte(state), []byte(stateCookie.Value)) != 1 {
		http.Error(w, "Invalid OAuth callback.", http.StatusBadRequest)
		return
	}

	// Invalidate the state immediately after successful validation so it cannot
	// be replayed, even if a later OAuth or Discord API request fails.
	clearStateOAuthCookie(w)

	accessToken, err := s.exchangeCode(r.Context(), code)
	if err != nil {
		s.logError("exchange OAuth code", err)
		http.Error(w, "Unable to complete linked-role authorization.", http.StatusBadGateway)
		return
	}

	metadata, err := s.updateRoleConnection(r.Context(), s.appID, accessToken)
	if err != nil {
		s.logError("update role connection", err)
		http.Error(w, "Unable to complete linked-role authorization.", http.StatusBadGateway)
		return
	}

	jsonMetadata, err := json.Marshal(metadata)
	if err != nil {
		s.logError("encode role connection metadata", err)
		http.Error(w, "Unable to complete linked-role authorization.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, `{"message":"linked-role metadata updated","metadata":%s}`, jsonMetadata)
}

func (s *linkedRolesServer) logError(operation string, err error) {
	if s.logger != nil {
		s.logger.Printf("%s: %v", operation, err)
	}
}

func updateRoleConnection(ctx context.Context, applicationID, accessToken string) (map[string]string, error) {
	session, err := dgo.New("Bearer " + accessToken)
	if err != nil {
		return nil, fmt.Errorf("create user session: %w", err)
	}

	user, err := session.User("@me", dgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("retrieve current user: %w", err)
	}

	metadata := map[string]string{
		"gopher":     "1",
		"loc":        "10000",
		"first_line": "1970-01-01",
	}

	_, err = session.UserApplicationRoleConnectionUpdate(applicationID, &dgo.ApplicationRoleConnection{
		PlatformName:     "Discord Gophers",
		PlatformUsername: user.Username,
		Metadata:         metadata,
	})
	if err != nil {
		return nil, fmt.Errorf("submit role connection: %w", err)
	}

	info, err := session.UserApplicationRoleConnection(applicationID)
	if err != nil {
		return nil, fmt.Errorf("retrieve role connection: %w", err)
	}
	return info.Metadata, nil
}

func generateStateOAuthCookie(w http.ResponseWriter, random io.Reader, now time.Time) (string, error) {
	randomBytes := make([]byte, 32)
	if _, err := io.ReadFull(random, randomBytes); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	state := base64.RawURLEncoding.EncodeToString(randomBytes)
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    state,
		Path:     oauthCallbackPath,
		Expires:  now.Add(oauthStateLifetime),
		MaxAge:   int(oauthStateLifetime / time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return state, nil
}

func clearStateOAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    "",
		Path:     oauthCallbackPath,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func singleQueryValue(values url.Values, key string) (string, bool) {
	matches := values[key]
	returnValue := ""
	if len(matches) == 1 {
		returnValue = matches[0]
	}
	return returnValue, len(matches) == 1 && returnValue != ""
}

func singleCookie(r *http.Request, name string) (*http.Cookie, bool) {
	var match *http.Cookie
	for _, cookie := range r.Cookies() {
		if cookie.Name != name {
			continue
		}
		if match != nil || cookie.Value == "" {
			return nil, false
		}
		match = cookie
	}
	return match, match != nil
}

func setNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}
