package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
)

const callbackPath = "/callback"

// RedirectURL is the OAuth redirect URI for the local callback server. It must
// exactly match a redirect URI registered on the Spotify application.
func RedirectURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackPath)
}

func newAuthenticator(clientID string, port int) *spotifyauth.Authenticator {
	return spotifyauth.New(
		spotifyauth.WithClientID(clientID),
		spotifyauth.WithRedirectURL(RedirectURL(port)),
		spotifyauth.WithScopes(
			spotifyauth.ScopePlaylistReadPrivate,
			spotifyauth.ScopePlaylistReadCollaborative,
		),
	)
}

// RunInteractiveFlow performs the authorization-code + PKCE flow: it opens the
// user's browser to Spotify's consent page, receives the redirect on a local
// callback server, exchanges the code for a token, and caches it to disk.
func RunInteractiveFlow(ctx context.Context, clientID string, port int) error {
	if clientID == "" {
		return fmt.Errorf("client_id is not set (see byom-sync.yaml.example)")
	}

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return err
	}
	authr := newAuthenticator(clientID, port)
	authURL := authr.AuthURL(state, oauth2.S256ChallengeOption(verifier))

	type result struct {
		tok *oauth2.Token
		err error
	}
	resultCh := make(chan result, 1)
	var once sync.Once

	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			if e := r.URL.Query().Get("error"); e != "" {
				http.Error(w, "authorization denied: "+e, http.StatusBadRequest)
				resultCh <- result{err: fmt.Errorf("authorization denied: %s", e)}
				return
			}
			if r.URL.Query().Get("state") != state {
				http.Error(w, "state mismatch", http.StatusBadRequest)
				resultCh <- result{err: fmt.Errorf("state mismatch (possible CSRF)")}
				return
			}
			tok, err := authr.Token(r.Context(), state, r, oauth2.VerifierOption(verifier))
			if err != nil {
				http.Error(w, "token exchange failed", http.StatusInternalServerError)
				resultCh <- result{err: fmt.Errorf("token exchange: %w", err)}
				return
			}
			fmt.Fprintln(w, "byom-sync: authentication complete. You can close this tab.")
			resultCh <- result{tok: tok}
		})
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("callback server on port %d: %w", port, err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Opening browser for Spotify authorization...\nIf it doesn't open, visit:\n%s\n", authURL)
	if err := openBrowser(authURL); err != nil {
		fmt.Printf("(could not open browser automatically: %v)\n", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			return res.err
		}
		return SaveToken(res.tok)
	}
}

// Client builds an authenticated Spotify client with silent token refresh and
// automatic 429 retry. The returned token is the one loaded from disk; pass it
// to PersistRefreshed after use to write back any refreshed token.
func Client(ctx context.Context, clientID string, port int) (*spotify.Client, *oauth2.Token, error) {
	tok, err := LoadToken()
	if err != nil {
		return nil, nil, err
	}
	authr := newAuthenticator(clientID, port)
	httpClient := authr.Client(ctx, tok)
	return spotify.New(httpClient, spotify.WithRetry(true)), tok, nil
}

// PersistRefreshed re-saves the client's current token if it differs from prev
// (i.e. the transport silently refreshed it during use).
func PersistRefreshed(client *spotify.Client, prev *oauth2.Token) {
	cur, err := client.Token()
	if err == nil && cur != nil && prev != nil && cur.AccessToken != prev.AccessToken {
		_ = SaveToken(cur)
	}
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		cmd = "xdg-open"
	}
	return exec.Command(cmd, append(args, url)...).Start()
}
