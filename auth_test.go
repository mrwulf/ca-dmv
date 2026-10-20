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
	// noEndSession omits end_session_endpoint from discovery.
	noEndSession bool
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
		f.nonce, f.challenge = q.Get("nonce"), q.Get("code_challenge")
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
	if res.StatusCode != http.StatusOK || me.Name != "Pat Driver" {
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

// loginAndLogout signs in through the fake IdP and returns the JSON the logout endpoint answers with.
func loginAndLogout(t *testing.T, idp *fakeIdP) (appURL string, redirect string, client *http.Client) {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(app.Close)
	cfg := config{issuer: idp.srv.URL, clientID: "cid", baseURL: app.URL}
	s := &server{cfg: cfg, st: st, auth: &authn{cfg: cfg, st: st}}
	handler = s.routes()

	jar, _ := cookiejar.New(nil)
	client = &http.Client{Jar: jar}
	if res, err := client.Get(app.URL + "/auth/login"); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("login: %v %v", err, res)
	}
	req, _ := http.NewRequest("POST", app.URL+"/auth/logout", nil)
	req.Header.Set("Origin", app.URL)
	res, err := client.Do(req)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("logout: %v %v", err, res)
	}
	var body struct{ Redirect string }
	json.NewDecoder(res.Body).Decode(&body)
	return app.URL, body.Redirect, client
}

// Logout must send the browser to the provider's end_session_endpoint with the ID token, or the
// provider keeps the user signed in and logs them straight back in.
func TestLogoutUsesProviderEndSession(t *testing.T) {
	idp := newFakeIdP(t, "cid")
	appURL, redirect, client := loginAndLogout(t, idp)

	u, err := url.Parse(redirect)
	if err != nil || u.Scheme+"://"+u.Host != idp.srv.URL || u.Path != "/endsession" {
		t.Fatalf("redirect = %q, want the IdP end_session_endpoint", redirect)
	}
	q := u.Query()
	if q.Get("client_id") != "cid" || q.Get("post_logout_redirect_uri") != appURL+"/" || q.Get("id_token_hint") == "" {
		t.Fatalf("end_session query = %v", q)
	}
	// The local session is gone too.
	if res, _ := client.Get(appURL + "/api/me"); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout /api/me = %d", res.StatusCode)
	}
}

func TestLogoutWithoutEndSessionEndpoint(t *testing.T) {
	idp := newFakeIdP(t, "cid")
	idp.noEndSession = true
	_, redirect, _ := loginAndLogout(t, idp)
	if redirect != "/" {
		t.Fatalf("redirect = %q, want /", redirect)
	}
}
