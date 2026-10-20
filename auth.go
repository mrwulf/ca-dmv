package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "session"
	stateCookie   = "oidc_state"
	sessionTTL    = 30 * 24 * time.Hour
)

type authn struct {
	cfg config
	st  *store

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// init discovers the provider lazily so the app starts even if the IdP is briefly down.
func (a *authn) init(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.oauth != nil {
		return a.oauth, a.verifier, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, a.cfg.issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("oidc discovery: %w", err)
	}
	a.oauth = &oauth2.Config{
		ClientID:     a.cfg.clientID,
		ClientSecret: a.cfg.clientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  a.cfg.baseURL + "/auth/callback",
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	a.verifier = p.Verifier(&oidc.Config{ClientID: a.cfg.clientID})
	return a.oauth, a.verifier, nil
}

func (a *authn) cookie(name, value, path string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: path, MaxAge: maxAge,
		HttpOnly: true, Secure: a.cfg.secure, SameSite: http.SameSiteLaxMode,
	}
}

func (a *authn) login(w http.ResponseWriter, r *http.Request) {
	oc, _, err := a.init(r.Context())
	if err != nil {
		slog.Error("login", "err", err)
		http.Error(w, "sign-in is temporarily unavailable", http.StatusBadGateway)
		return
	}
	state, nonce, verifier := randomToken(), randomToken(), oauth2.GenerateVerifier()
	if err = a.st.saveFlow(r.Context(), state, nonce, verifier); err != nil {
		slog.Error("save login flow", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// The cookie binds the flow to this browser so a forged callback link cannot log someone in.
	http.SetCookie(w, a.cookie(stateCookie, state, "/auth", 600))
	http.Redirect(w, r, oc.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (a *authn) callback(w http.ResponseWriter, r *http.Request) {
	oc, verifier, err := a.init(r.Context())
	if err != nil {
		slog.Error("callback", "err", err)
		http.Error(w, "sign-in is temporarily unavailable", http.StatusBadGateway)
		return
	}
	state := r.URL.Query().Get("state")
	c, err := r.Cookie(stateCookie)
	if err != nil || state == "" || c.Value != state {
		http.Error(w, "sign-in expired, please try again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, a.cookie(stateCookie, "", "/auth", -1))
	nonce, pkce, err := a.st.takeFlow(r.Context(), state)
	if err != nil {
		http.Error(w, "sign-in expired, please try again", http.StatusBadRequest)
		return
	}
	if e := r.URL.Query().Get("error"); e != "" {
		slog.Warn("idp returned error", "error", e)
		http.Error(w, "sign-in was not completed", http.StatusForbidden)
		return
	}
	tok, err := oc.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(pkce))
	if err != nil {
		slog.Warn("code exchange failed", "err", err)
		http.Error(w, "sign-in failed", http.StatusBadGateway)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := verifier.Verify(r.Context(), rawID)
	if err != nil || idt.Nonce != nonce {
		slog.Warn("id token rejected", "err", err)
		http.Error(w, "sign-in failed", http.StatusForbidden)
		return
	}
	var claims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
	}
	if err = idt.Claims(&claims); err != nil {
		http.Error(w, "sign-in failed", http.StatusForbidden)
		return
	}
	name := firstNonEmpty(claims.Name, claims.PreferredUsername, claims.Email, "Driver")
	token, err := a.st.createSession(r.Context(), idt.Subject, name, sessionTTL)
	if err != nil {
		slog.Error("create session", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.cookie(sessionCookie, token, "/", int(sessionTTL.Seconds())))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *authn) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err = a.st.deleteSession(r.Context(), c.Value); err != nil {
			slog.Error("delete session", "err", err)
		}
	}
	http.SetCookie(w, a.cookie(sessionCookie, "", "/", -1))
	w.WriteHeader(http.StatusNoContent)
}

func (a *authn) currentUser(r *http.Request) (user, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return user{}, errors.New("no session")
	}
	return a.st.sessionUser(r.Context(), c.Value)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
