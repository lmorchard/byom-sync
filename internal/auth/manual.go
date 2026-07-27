package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

// ParseManualRedirect extracts the authorization code from whatever the user
// pasted after authorizing in a browser. It accepts either the full redirect
// URL (whose query carries both code and state) or a bare code value, because
// copying a whole address bar is fiddly and users often grab just the code.
//
// When a URL is pasted, its state must equal wantState — that check is the CSRF
// defense the local callback server performs in the interactive flow. A bare
// code carries no state to verify, which is an accepted trade-off for a value
// the user has just copied out of their own browser.
func ParseManualRedirect(pasted, wantState string) (string, error) {
	s := strings.TrimSpace(pasted)
	if s == "" {
		return "", errors.New("nothing pasted — expected the redirect URL or the code from it")
	}

	hasScheme := strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")

	// A bare code has no scheme and no query string.
	if !hasScheme && !strings.Contains(s, "?") {
		return s, nil
	}

	// Browsers hide the scheme in the address bar, so pasting
	// "127.0.0.1:8888/callback?code=...&state=..." without "http://" is the
	// likely-common case, not an edge case. Without a scheme, url.Parse reads
	// "127.0.0.1" as a path segment containing a colon and rejects it
	// outright, so add the scheme back before parsing.
	if !hasScheme && (strings.HasPrefix(s, "127.0.0.1") || strings.HasPrefix(s, "localhost")) {
		s = "http://" + s
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("parse pasted URL: %w (paste the full URL including http://)", err)
	}
	q := u.Query()

	if e := q.Get("error"); e != "" {
		if desc := q.Get("error_description"); desc != "" {
			return "", fmt.Errorf("authorization denied: %s: %s", e, desc)
		}
		return "", fmt.Errorf("authorization denied: %s", e)
	}
	if got := q.Get("state"); got != wantState {
		return "", fmt.Errorf("state mismatch (possible CSRF): pasted URL carried state %q", got)
	}
	code := q.Get("code")
	if code == "" {
		return "", errors.New("pasted URL has no code parameter")
	}
	return code, nil
}

// RunManualFlow performs the authorization-code + PKCE flow without a local
// callback server, for headless or remote hosts.
//
// Spotify requires the redirect URI to be exactly http://127.0.0.1:<port>/callback
// and matches it as a literal string, so it cannot be pointed at a remote host.
// Over SSH that means the consent page redirects the *local* browser to the
// *local* machine's port, and the code never reaches the machine running this
// command. Here the user relays the code by hand instead.
func RunManualFlow(ctx context.Context, clientID string, port int, in io.Reader, out io.Writer) error {
	if clientID == "" {
		return errors.New("client_id is not set (see byom-sync.yaml.example)")
	}

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return err
	}
	authr := newAuthenticator(clientID, port)
	authURL := authr.AuthURL(state, oauth2.S256ChallengeOption(verifier))

	_, _ = fmt.Fprintf(out, `Open this URL in a browser on any machine and authorize:

%s

Your browser will then fail to load %s — that is expected, and the
address bar still holds the code.

Paste that full URL here (or just the code): `, authURL, RedirectURL(port))

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read pasted input: %w", err)
	}
	_, _ = fmt.Fprintln(out)

	code, err := ParseManualRedirect(line, state)
	if err != nil {
		return err
	}

	tok, err := authr.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return fmt.Errorf("token exchange: %w", err)
	}
	return SaveToken(tok)
}
