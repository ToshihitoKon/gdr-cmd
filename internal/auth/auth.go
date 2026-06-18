// Package auth handles Google account authentication via OAuth 2.0.
//
// Instead of holding a service account key locally, it authenticates using an
// OAuth client (of the desktop app type) that the user created in the GCP
// Console. The obtained token is cached in the config directory and refreshed
// automatically with the refresh token.
//
// About the authentication flow:
//
//	Google discontinued the legacy OOB flow (redirect_uri=urn:ietf:wg:oauth:2.0:oob)
//	in 2022. For this reason this package uses a loopback redirect
//	(http://127.0.0.1:<port>).
//
//	By default it starts a temporary local HTTP server and automatically receives
//	the authorization code redirected after the user approves in the browser. For
//	environments where a browser cannot be opened locally (such as over SSH),
//	when --no-browser is given it falls back to the manual copy-paste method
//	(paste the redirect URL and extract the code from it).
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ToshihitoKon/gdr-cmd/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	driveapi "google.golang.org/api/drive/v3"
)

// Scopes are the OAuth scopes to request.
//
// Anticipating future upload support, the read-write drive scope is the default
// (drive.readonly would suffice for download only, but the write permission is
// acquired upfront to avoid re-authentication).
var Scopes = []string{driveapi.DriveScope}

// loginTimeout is the maximum time to wait for browser approval. Once exceeded, authentication is aborted.
const loginTimeout = 5 * time.Minute

// Authenticator bundles the OAuth configuration and token I/O.
type Authenticator struct {
	oauthConfig *oauth2.Config
	tokenPath   string
}

// New builds an authenticator from the credentials in the config directory.
// redirectURL is determined after the loopback server starts, so it is left empty here.
func New() (*Authenticator, error) {
	creds, err := config.LoadClientCredentials()
	if err != nil {
		return nil, err
	}
	tokenPath, err := config.TokenPath()
	if err != nil {
		return nil, err
	}
	return &Authenticator{
		oauthConfig: &oauth2.Config{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			Scopes:       Scopes,
			Endpoint:     google.Endpoint,
		},
		tokenPath: tokenPath,
	}, nil
}

// Client returns an authenticated HTTP client.
//
// If a cached token exists it is used (and refreshed automatically with the
// refresh token if expired); otherwise an error is returned. When not
// authenticated, the caller should guide the user to run `auth login`. A
// refreshed token is re-saved on a best-effort basis.
func (a *Authenticator) Client(ctx context.Context) (*http.Client, error) {
	tok, err := a.loadToken()
	if err != nil {
		return nil, err
	}

	src := a.oauthConfig.TokenSource(ctx, tok)
	refreshed, err := src.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token. please run `gdr auth login` again: %w", err)
	}
	// If the refresh issued a new access token, re-save it.
	if refreshed.AccessToken != tok.AccessToken {
		if saveErr := a.saveToken(refreshed); saveErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save the refreshed token: %v\n", saveErr)
		}
	}
	return oauth2.NewClient(ctx, oauth2.StaticTokenSource(refreshed)), nil
}

// HasToken reports whether a cached token exists.
func (a *Authenticator) HasToken() bool {
	_, err := os.Stat(a.tokenPath)
	return err == nil
}

// Login runs the OAuth flow interactively and saves the obtained token.
// When noBrowser is true, it does not auto-launch the browser or auto-receive
// via loopback, and instead receives the authorization code via the manual
// copy-paste method.
func (a *Authenticator) Login(ctx context.Context, noBrowser bool) error {
	if noBrowser {
		return a.loginManual(ctx)
	}
	return a.loginLoopback(ctx)
}

// loginLoopback automatically receives the authorization code via a local HTTP server.
func (a *Authenticator) loginLoopback(ctx context.Context) error {
	// Let the OS pick the port, and build redirect_uri from the resolved address.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start local server. please try manual authentication with --no-browser: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	a.oauthConfig.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", port)

	state, err := randomState()
	if err != nil {
		return err
	}
	authURL := a.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	// Channel to pass the authorization code or an error from the handler to main.
	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)

	srv := &http.Server{}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			writeBrowserMessage(w, "Authentication was denied: "+e)
			resultCh <- result{err: fmt.Errorf("authentication was denied: %s", e)}
			return
		}
		if q.Get("state") != state {
			writeBrowserMessage(w, "The state does not match. Please retry authentication.")
			resultCh <- result{err: fmt.Errorf("state mismatch: possible CSRF")}
			return
		}
		code := q.Get("code")
		if code == "" {
			writeBrowserMessage(w, "Could not obtain the authorization code.")
			resultCh <- result{err: fmt.Errorf("authorization code is empty")}
			return
		}
		writeBrowserMessage(w, "Authentication succeeded. You can close this tab and return to the terminal.")
		resultCh <- result{code: code}
	})
	srv.Handler = mux

	go func() { _ = srv.Serve(listener) }()
	defer srv.Close()

	fmt.Fprintln(os.Stderr, "Open the following URL in your browser to authenticate:")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+authURL)
	fmt.Fprintln(os.Stderr, "")
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintln(os.Stderr, "(Failed to auto-launch the browser. Please open the URL above manually.)")
	}
	fmt.Fprintln(os.Stderr, "Waiting for authentication to complete...")

	timeoutCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()

	select {
	case <-timeoutCtx.Done():
		return fmt.Errorf("authentication timed out (%s)", loginTimeout)
	case res := <-resultCh:
		if res.err != nil {
			return res.err
		}
		return a.exchangeAndSave(ctx, res.code)
	}
}

// loginManual receives the authorization code via the manual copy-paste method.
//
// Since nothing actually listens at the loopback redirect target (127.0.0.1),
// the browser shows a "cannot connect" message. The user is asked to paste the
// entire URL from the address bar, and the code query is extracted from it.
// Pasting the bare authorization code is also accepted.
func (a *Authenticator) loginManual(ctx context.Context) error {
	// Nothing listens, but reading the code from the browser's address bar is enough.
	// A fixed port is used so the user can recognize the URL more easily.
	const manualPort = 9999
	a.oauthConfig.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d", manualPort)

	state, err := randomState()
	if err != nil {
		return err
	}
	authURL := a.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	fmt.Fprintln(os.Stderr, "Open the following URL in a browser (on any device) to authenticate:")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  "+authURL)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "After approving, the browser will show a connection error to 127.0.0.1 (this is expected).")
	fmt.Fprintln(os.Stderr, "Paste the entire URL from the address bar at that point here and press Enter:")
	fmt.Fprint(os.Stderr, "> ")

	var input string
	if _, err := fmt.Scanln(&input); err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	code, err := extractCode(input, state)
	if err != nil {
		return err
	}
	return a.exchangeAndSave(ctx, code)
}

// extractCode pulls the authorization code out of the pasted string.
// If it is a URL, it extracts the code query and validates state; otherwise it
// treats the input itself as the authorization code.
func extractCode(input, wantState string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("input is empty")
	}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err != nil {
			return "", fmt.Errorf("failed to parse URL: %w", err)
		}
		q := u.Query()
		if e := q.Get("error"); e != "" {
			return "", fmt.Errorf("authentication was denied: %s", e)
		}
		if s := q.Get("state"); s != "" && s != wantState {
			return "", fmt.Errorf("state mismatch: possible CSRF")
		}
		code := q.Get("code")
		if code == "" {
			return "", fmt.Errorf("the URL does not contain an authorization code (code)")
		}
		return code, nil
	}
	return input, nil
}

// exchangeAndSave exchanges the authorization code for a token and saves it.
func (a *Authenticator) exchangeAndSave(ctx context.Context, code string) error {
	tok, err := a.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to exchange the authorization code for a token: %w", err)
	}
	if tok.RefreshToken == "" {
		// Without a refresh token, re-authentication will be required next time.
		// One is normally issued because ApprovalForce is set, but warn just in case.
		fmt.Fprintln(os.Stderr, "Warning: no refresh token was obtained. You may need to authenticate again next time.")
	}
	if err := a.saveToken(tok); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Authentication succeeded and the token was saved.")
	return nil
}

// Logout removes the cached token.
func (a *Authenticator) Logout() error {
	err := os.Remove(a.tokenPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to delete the token: %w", err)
	}
	return nil
}

// loadToken loads the cached token.
func (a *Authenticator) loadToken() (*oauth2.Token, error) {
	data, err := os.ReadFile(a.tokenPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not authenticated. please run `gdr auth login` first")
		}
		return nil, fmt.Errorf("failed to read the token: %w", err)
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("failed to parse the token. please re-authenticate with `gdr auth login`: %w", err)
	}
	return &tok, nil
}

// saveToken saves the token with mode 0600 (read/write by owner only, since it is a credential).
func (a *Authenticator) saveToken(tok *oauth2.Token) error {
	if _, err := config.EnsureDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize the token: %w", err)
	}
	if err := os.WriteFile(a.tokenPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to save the token: %w", err)
	}
	return nil
}

// randomState generates a state value for CSRF protection.
func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// writeBrowserMessage returns a simple HTML message for the browser.
func writeBrowserMessage(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<html><body style='font-family:sans-serif;padding:2em'><p>%s</p></body></html>", msg)
}

// openBrowser opens the URL in the OS's default browser.
func openBrowser(rawURL string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", rawURL}
	default: // linux, etc.
		cmd = "xdg-open"
		args = []string{rawURL}
	}
	return exec.Command(cmd, args...).Start()
}
