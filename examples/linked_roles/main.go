package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"

	"github.com/darui3018823/dgo"
	"github.com/joho/godotenv"
	"golang.org/x/oauth2"
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

func init() {
	flag.Parse()
	godotenv.Load()
	oauthConfig.ClientID = *appID
	oauthConfig.ClientSecret = *clientSecret
	oauthConfig.RedirectURL, _ = url.JoinPath(*redirectURL, "/linked-roles-callback")
}

func main() {
	s, _ := dgo.New("Bot " + *token)

	_, err := s.ApplicationRoleConnectionMetadataUpdate(*appID, []*dgo.ApplicationRoleConnectionMetadata{
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
		panic(err)
	}

	fmt.Println("Updated application metadata")
	http.HandleFunc("/linked-roles", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		// Generate a per-request state and store it in a cookie, then redirect the user to Discord OAuth2 page.
		state := generateStateOauthCookie(w)
		http.Redirect(w, r, oauthConfig.AuthCodeURL(state), http.StatusMovedPermanently)
	})
	http.HandleFunc("/linked-roles-callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		// A safeguard against CSRF attacks: validate that the state returned by Discord matches the one stored in the cookie.
		stateValues := q["state"]
		if len(stateValues) == 0 {
			http.Error(w, "state parameter missing", http.StatusBadRequest)
			return
		}
		oauthState, err := r.Cookie("oauthstate")
		if err != nil {
			http.Error(w, "oauthstate cookie not found", http.StatusBadRequest)
			return
		}
		if stateValues[0] != oauthState.Value {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}

		// Fetch the tokens with code we've received.
		tokens, err := oauthConfig.Exchange(r.Context(), q["code"][0])
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		}

		// Construct a temporary session with user's OAuth2 access_token.
		ts, _ := dgo.New("Bearer " + tokens.AccessToken)

		// Retrive the user data.
		u, err := ts.User("@me")
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		}

		// Fetch external metadata...
		// NOTE: Hardcoded for the sake of the example.
		metadata := map[string]string{
			"gopher":     "1", // 1 for true, 0 for false
			"loc":        "10000",
			"first_line": "1970-01-01", // YYYY-MM-DD
		}

		// And submit it back to discord.
		_, err = ts.UserApplicationRoleConnectionUpdate(*appID, &dgo.ApplicationRoleConnection{
			PlatformName:     "Discord Gophers",
			PlatformUsername: u.Username,
			Metadata:         metadata,
		})
		if err != nil {
			w.Write([]byte(err.Error()))
			return
		}

		// Retrieve it to check if everything is ok.
		info, err := ts.UserApplicationRoleConnection(*appID)

		if err != nil {
			w.Write([]byte(err.Error()))
			return
		}
		jsonMetadata, _ := json.Marshal(info.Metadata)
		// And show it to the user.
		w.Write([]byte(fmt.Sprintf("Your updated metadata is: %s", jsonMetadata)))
	})
	http.ListenAndServe(":8010", nil)
}

// generateStateOauthCookie creates a cryptographically random state string,
// stores it in a short-lived cookie, and returns it for use in the OAuth2 flow.
func generateStateOauthCookie(w http.ResponseWriter) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// A failure to generate cryptographically random data is a critical error for a security feature.
		// The application should not proceed with an insecure fallback.
		panic("failed to generate random bytes for oauth state: " + err.Error())
	}
	state := base64.URLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name:     "oauthstate",
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
	})
	return state
}
