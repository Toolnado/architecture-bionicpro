package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/bionicpro/bionicpro-auth/internal/config"
	"github.com/bionicpro/bionicpro-auth/internal/oidc"
	"github.com/bionicpro/bionicpro-auth/internal/secure"
	"github.com/bionicpro/bionicpro-auth/internal/session"
)

const (
	loginCookieName = "bp_login"
	sessionIDHeader = "X-Session-Id"
	refreshLeeway   = 10 * time.Second
)

type ctxKey struct{}

type Handler struct {
	cfg   *config.Config
	oidc  *oidc.Client
	store *session.Store
	proxy http.Handler
}

func New(cfg *config.Config, client *oidc.Client, store *session.Store) (*Handler, error) {
	h := &Handler{cfg: cfg, oidc: client, store: store}

	if cfg.ReportsAPIURL != "" {
		target, err := url.Parse(cfg.ReportsAPIURL)
		if err != nil {
			return nil, err
		}
		h.proxy = h.newReportsProxy(target)
	}
	return h, nil
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /auth/login", h.login)
	mux.HandleFunc("GET /auth/callback", h.callback)
	mux.Handle("POST /auth/logout", h.requireSession(http.HandlerFunc(h.logout)))
	mux.Handle("GET /auth/session", h.requireSession(http.HandlerFunc(h.currentSession)))
	mux.Handle("/api/", h.requireSession(http.HandlerFunc(h.reports)))

	return h.withCORS(mux)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	state, err := secure.Token(32)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "cannot start login", err)
		return
	}
	nonce, err := secure.Token(16)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "cannot start login", err)
		return
	}
	verifier, err := secure.CodeVerifier()
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "cannot start login", err)
		return
	}

	ls := session.LoginState{Verifier: verifier, Nonce: nonce, Return: h.sanitizeReturn(r.URL.Query().Get("return_to"))}
	if err := h.store.SaveLogin(r.Context(), state, ls, h.cfg.LoginTTL); err != nil {
		h.fail(w, http.StatusInternalServerError, "cannot start login", err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     loginCookieName,
		Value:    state,
		Path:     "/auth",
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.cfg.LoginTTL.Seconds()),
	})

	challenge := secure.CodeChallengeS256(verifier)
	http.Redirect(w, r, h.oidc.AuthCodeURL(h.cfg.RedirectURI, state, nonce, challenge), http.StatusFound)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if errParam := query.Get("error"); errParam != "" {
		h.fail(w, http.StatusUnauthorized, "identity provider returned an error", errors.New(errParam))
		return
	}

	state := query.Get("state")
	bound, err := r.Cookie(loginCookieName)
	if err != nil || bound.Value == "" || bound.Value != state {
		h.fail(w, http.StatusBadRequest, "state does not match the browser that started login", err)
		return
	}
	h.clearCookie(w, loginCookieName, "/auth")

	ls, err := h.store.TakeLogin(r.Context(), state)
	if err != nil {
		h.fail(w, http.StatusBadRequest, "login state is unknown or already used", err)
		return
	}

	code := query.Get("code")
	if code == "" {
		h.fail(w, http.StatusBadRequest, "authorization code is missing", nil)
		return
	}

	tokens, err := h.oidc.Exchange(r.Context(), code, ls.Verifier, h.cfg.RedirectURI)
	if err != nil {
		h.fail(w, http.StatusBadGateway, "cannot exchange authorization code", err)
		return
	}

	sess, err := h.newSession(r.Context(), tokens)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "cannot create session", err)
		return
	}

	h.setSessionCookie(w, sess.ID)
	http.Redirect(w, r, h.cfg.FrontendURL+ls.Return, http.StatusFound)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())

	if sess.Refresh != "" {
		if err := h.oidc.Logout(r.Context(), sess.Refresh); err != nil {
			log.Printf("logout: revoking refresh token failed: %v", err)
		}
	}
	if err := h.store.Delete(r.Context(), sess.ID); err != nil {
		log.Printf("logout: dropping session failed: %v", err)
	}

	h.clearCookie(w, h.cfg.CookieName, "/")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) currentSession(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())

	writeJSON(w, http.StatusOK, map[string]any{
		"sessionId":         sess.ID,
		"username":          sess.Username,
		"email":             sess.Email,
		"roles":             sess.Roles,
		"accessTokenExpiry": sess.AccessExp,
		"rotatedAt":         sess.RotatedAt,
	})
}

func (h *Handler) reports(w http.ResponseWriter, r *http.Request) {
	if h.proxy == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "REPORTS_API_URL is not configured",
		})
		return
	}
	h.proxy.ServeHTTP(w, r)
}

func (h *Handler) newReportsProxy(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	base := proxy.Director

	proxy.Director = func(r *http.Request) {
		base(r)
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api")
		r.Host = target.Host
		if sess := sessionFrom(r.Context()); sess != nil {
			r.Header.Set("Authorization", "Bearer "+sess.Access)
		}
		r.Header.Del("Cookie")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		log.Printf("reports proxy: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "reports api is unavailable"})
	}
	return proxy
}

func (h *Handler) newSession(ctx context.Context, tokens *oidc.Tokens) (*session.Session, error) {
	claims, err := oidc.ParseClaims(tokens.AccessToken)
	if err != nil {
		return nil, err
	}

	return h.store.Create(ctx, &session.Session{
		Subject:   claims.Subject,
		Username:  claims.Username,
		Email:     claims.Email,
		Roles:     claims.Roles,
		Access:    tokens.AccessToken,
		IDToken:   tokens.IDToken,
		Refresh:   tokens.RefreshToken,
		AccessExp: claims.Expiry,
	})
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cfg.CookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.cfg.SessionTTL.Seconds()),
	})
}

func (h *Handler) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		HttpOnly: true,
		Secure:   h.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (h *Handler) sanitizeReturn(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return "/"
	}
	return raw
}

func (h *Handler) fail(w http.ResponseWriter, status int, message string, err error) {
	if err != nil {
		log.Printf("%s: %v", message, err)
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}

func sessionFrom(ctx context.Context) *session.Session {
	sess, _ := ctx.Value(ctxKey{}).(*session.Session)
	return sess
}
