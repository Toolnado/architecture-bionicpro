package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/bionicpro/bionicpro-auth/internal/oidc"
	"github.com/bionicpro/bionicpro-auth/internal/session"
)

func (h *Handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(h.cfg.CookieName)
		if err != nil || cookie.Value == "" {
			h.fail(w, http.StatusUnauthorized, "session cookie is missing", nil)
			return
		}

		sess, err := h.store.Get(r.Context(), cookie.Value)
		if errors.Is(err, session.ErrNotFound) {
			h.clearCookie(w, h.cfg.CookieName, "/")
			h.fail(w, http.StatusUnauthorized, "session is unknown or expired", nil)
			return
		}
		if err != nil {
			h.fail(w, http.StatusInternalServerError, "cannot read session", err)
			return
		}

		if time.Now().UTC().After(sess.AccessExp.Add(-refreshLeeway)) {
			if err := h.refreshTokens(r.Context(), sess); err != nil {
				_ = h.store.Delete(r.Context(), sess.ID)
				h.clearCookie(w, h.cfg.CookieName, "/")
				h.fail(w, http.StatusUnauthorized, "session cannot be refreshed", err)
				return
			}
		}

		rotated, err := h.store.Rotate(r.Context(), sess)
		if err != nil {
			h.fail(w, http.StatusInternalServerError, "cannot rotate session", err)
			return
		}

		h.setSessionCookie(w, rotated.ID)
		w.Header().Set(sessionIDHeader, rotated.ID)

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, rotated)))
	})
}

func (h *Handler) refreshTokens(ctx context.Context, sess *session.Session) error {
	if sess.Refresh == "" {
		return errors.New("session has no refresh token")
	}

	tokens, err := h.oidc.Refresh(ctx, sess.Refresh)
	if err != nil {
		return err
	}
	claims, err := oidc.ParseClaims(tokens.AccessToken)
	if err != nil {
		return err
	}

	sess.Access = tokens.AccessToken
	sess.AccessExp = claims.Expiry
	sess.Roles = claims.Roles
	if tokens.RefreshToken != "" {
		sess.Refresh = tokens.RefreshToken
	}
	if tokens.IDToken != "" {
		sess.IDToken = tokens.IDToken
	}
	return nil
}

func (h *Handler) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin == h.cfg.FrontendURL {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Expose-Headers", sessionIDHeader)
			w.Header().Add("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
