package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeIdP is a minimal OIDC provider that enforces PKCE and returns a signed ID token.
type fakeIdP struct {
	srv       *httptest.Server
	key       *rsa.PrivateKey
	clientID  string
	nonce     string
	challenge string
	// noEndSession omits end_session_endpoint from discovery; noPrompts omits prompt_values_supported.
	noEndSession bool
	noPrompts    bool
	// prompt is the prompt parameter the last authorization request carried.
	prompt string
}

func newFakeIdP(t *testing.T, clientID string) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key, clientID: clientID}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		doc := map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"},
		}
		if !f.noEndSession {
			doc["end_session_endpoint"] = f.srv.URL + "/endsession"
		}
		if !f.noPrompts {
			doc["prompt_values_supported"] = []string{"none", "login", "consent", "select_account"}
		}
		json.NewEncoder(w).Encode(doc)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("client_id") != clientID {
			http.Error(w, "bad authorize request", http.StatusBadRequest)
			return
		}
		f.nonce, f.challenge, f.prompt = q.Get("nonce"), q.Get("code_challenge"), q.Get("prompt")
		http.Redirect(w, r, q.Get("redirect_uri")+"?code=abc&state="+url.QueryEscape(q.Get("state")), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k"))
		raw, _ := jwt.Signed(signer).Claims(jwt.Claims{
			Issuer: f.srv.URL, Subject: "sub-123", Audience: jwt.Audience{clientID},
			Expiry: jwt.NewNumericDate(time.Now().Add(time.Hour)), IssuedAt: jwt.NewNumericDate(time.Now()),
		}).Claims(map[string]any{"nonce": f.nonce, "name": "Pat Driver"}).Serialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": raw})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func TestLoginFlow(t *testing.T) {
	idp := newFakeIdP(t, "cid")
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	// The app's URL is only known once it is listening, so route through a late-bound handler.
	var handler http.Handler
	app := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}))
	app.Start()
	defer app.Close()
	cfg := config{issuer: idp.srv.URL, clientID: "cid", baseURL: app.URL}
	s := &server{cfg: cfg, st: st, auth: &authn{cfg: cfg, st: st}}
	handler = s.routes()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	// Not signed in yet.
	res, _ := client.Get(app.URL + "/api/me")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/me = %d", res.StatusCode)
	}

	// Full login: /auth/login -> IdP -> /auth/callback -> /.
	res, err = client.Get(app.URL + "/auth/login")
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login flow: %v status=%v", err, res)
	}
	res, _ = client.Get(app.URL + "/api/me")
	var me struct{ Name string }
	json.NewDecoder(res.Body).Decode(&me)
	if res.StatusCode != http.StatusOK || me.Name != "Pat" {
		t.Fatalf("after login /api/me = %d %+v", res.StatusCode, me)
	}

	// Logout ends the session.
	req, _ := http.NewRequest("POST", app.URL+"/auth/logout", nil)
	req.Header.Set("Origin", app.URL)
	if res, _ = client.Do(req); res.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d", res.StatusCode)
	}
	if res, _ = client.Get(app.URL + "/api/me"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout /api/me = %d", res.StatusCode)
	}
}

func TestCallbackRejectsForeignState(t *testing.T) {
	idp := newFakeIdP(t, "cid")
	st, _ := openStore(filepath.Join(t.TempDir(), "t.db"))
	cfg := config{issuer: idp.srv.URL, clientID: "cid", baseURL: "http://localhost"}
	s := &server{cfg: cfg, st: st, auth: &authn{cfg: cfg, st: st}}
	h := s.routes()

	// A flow started in one browser must not complete in another that lacks the state cookie.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/auth/login", nil))
	loc, _ := url.Parse(w.Header().Get("Location"))
	state := loc.Query().Get("state")

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/auth/callback?code=abc&state="+url.QueryEscape(state), nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("callback without state cookie = %d, want 400", w.Code)
	}
}

// authApp starts the app against the fake IdP, with tweak applied to its config.
func authApp(t *testing.T, idp *fakeIdP, tweak func(*config)) (appURL string, client *http.Client) {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(app.Close)
	cfg := config{issuer: idp.srv.URL, clientID: "cid", baseURL: app.URL}
	if tweak != nil {
		tweak(&cfg)
	}
	s := &server{cfg: cfg, st: st, auth: &authn{cfg: cfg, st: st}}
	handler = s.routes()
	jar, _ := cookiejar.New(nil)
	return app.URL, &http.Client{Jar: jar}
}

// loginAndLogout signs in through the fake IdP and returns the redirect the logout endpoint answers with.
func loginAndLogout(t *testing.T, idp *fakeIdP, tweak func(*config)) (appURL, redirect string, client *http.Client) {
	t.Helper()
	appURL, client = authApp(t, idp, tweak)
	if res, err := client.Get(appURL + "/auth/login"); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v %v", err, res)
	}
	req, _ := http.NewRequest("POST", appURL+"/auth/logout", nil)
	req.Header.Set("Origin", appURL)
	res, err := client.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("logout: %v %v", err, res)
	}
	var body struct{ Redirect string }
	json.NewDecoder(res.Body).Decode(&body)
	return appURL, body.Redirect, client
}

// By default signing out ends only this app's session: the provider session stays, so the user's
// other apps stay signed in.
func TestLogoutIsLocalByDefault(t *testing.T) {
	idp := newFakeIdP(t, "cid")
	appURL, redirect, client := loginAndLogout(t, idp, nil)
	if redirect != "/#/signed-out" {
		t.Fatalf("redirect = %q, want the app's signed-out screen", redirect)
	}
	if res, _ := client.Get(appURL + "/api/me"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout /api/me = %d", res.StatusCode)
	}
}

// With OIDC_LOGOUT_PROVIDER the browser is also sent to the provider's end_session_endpoint with
// the ID token.
func TestLogoutCanEndProviderSession(t *testing.T) {
	idp := newFakeIdP(t, "cid")
	appURL, redirect, _ := loginAndLogout(t, idp, func(c *config) { c.logoutProvider = true })
	u, err := url.Parse(redirect)
	if err != nil || u.Scheme+"://"+u.Host != idp.srv.URL || u.Path != "/endsession" {
		t.Fatalf("redirect = %q, want the IdP end_session_endpoint", redirect)
	}
	q := u.Query()
	if q.Get("client_id") != "cid" || q.Get("post_logout_redirect_uri") != appURL+"/" || q.Get("id_token_hint") == "" {
		t.Fatalf("end_session query = %v", q)
	}
}

func TestProviderLogoutWithoutEndSessionEndpoint(t *testing.T) {
	idp := newFakeIdP(t, "cid")
	idp.noEndSession = true
	_, redirect, _ := loginAndLogout(t, idp, func(c *config) { c.logoutProvider = true })
	if redirect != "/" {
		t.Fatalf("redirect = %q, want /", redirect)
	}
}

// After sign-out the app asks the provider to choose an account, but only for values the provider
// supports and the app allows.
func TestLoginForwardsPrompt(t *testing.T) {
	cases := []struct {
		name, query, want string
		noPrompts         bool
	}{
		{"select_account", "?prompt=select_account", "select_account", false},
		{"login", "?prompt=login", "login", false},
		{"none is not forwarded", "?prompt=none", "", false},
		{"unknown value", "?prompt=bogus", "", false},
		{"no prompt", "", "", false},
		{"provider lacks support", "?prompt=select_account", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			idp := newFakeIdP(t, "cid")
			idp.noPrompts = c.noPrompts
			appURL, client := authApp(t, idp, nil)
			if res, err := client.Get(appURL + "/auth/login" + c.query); err != nil || res.StatusCode != http.StatusOK {
				t.Fatalf("login: %v %v", err, res)
			}
			if idp.prompt != c.want {
				t.Fatalf("provider saw prompt=%q, want %q", idp.prompt, c.want)
			}
		})
	}
}
