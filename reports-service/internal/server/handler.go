package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/bionicpro/reports-service/internal/auth"
	"github.com/bionicpro/reports-service/internal/cdn"
	"github.com/bionicpro/reports-service/internal/config"
	"github.com/bionicpro/reports-service/internal/reports"
	"github.com/bionicpro/reports-service/internal/storage"
)

const (
	dateLayout     = "2006-01-02"
	defaultDaySpan = 29
)

type Handler struct {
	cfg      *config.Config
	verifier *auth.Verifier
	store    *reports.Store
	objects  *storage.Store
	signer   *cdn.Signer
}

func New(cfg *config.Config, verifier *auth.Verifier, store *reports.Store,
	objects *storage.Store, signer *cdn.Signer) *Handler {
	return &Handler{cfg: cfg, verifier: verifier, store: store, objects: objects, signer: signer}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /reports", h.reports)
	return mux
}

func (h *Handler) reports(w http.ResponseWriter, r *http.Request) {
	identity, err := h.verifier.Authenticate(r)
	switch {
	case errors.Is(err, auth.ErrNoToken), errors.Is(err, auth.ErrInvalidToken):
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	case errors.Is(err, auth.ErrForbidden):
		writeError(w, http.StatusForbidden, "role "+h.cfg.RequiredRole+" is required")
		return
	case err != nil:
		log.Printf("authenticate: %v", err)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	if requested := r.URL.Query().Get("username"); requested != "" && requested != identity.Username {
		writeError(w, http.StatusForbidden, "reports of other users are not available")
		return
	}

	covered, err := h.store.CoveredUntil(r.Context())
	if err != nil {
		if errors.Is(err, reports.ErrNoCoverage) {
			writeError(w, http.StatusServiceUnavailable, "no period has been processed by the ETL yet")
			return
		}
		log.Printf("covered until: %v", err)
		writeError(w, http.StatusBadGateway, "report storage is unavailable")
		return
	}

	to, err := parseDate(r.URL.Query().Get("to"), covered)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid 'to' date, expected YYYY-MM-DD")
		return
	}
	from, err := parseDate(r.URL.Query().Get("from"), to.AddDate(0, 0, -defaultDaySpan))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid 'from' date, expected YYYY-MM-DD")
		return
	}
	if from.After(to) {
		writeError(w, http.StatusBadRequest, "'from' must not be after 'to'")
		return
	}
	if from.After(covered) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":            "requested period has not been processed by the ETL yet",
			"dataCoveredUntil": covered.Format(dateLayout),
		})
		return
	}

	requestedTo := to
	if to.After(covered) {
		to = covered
	}

	crmVersion, err := h.store.DimensionVersion(r.Context(), identity.Username)
	if err != nil {
		log.Printf("read dimension version: %v", err)
		writeError(w, http.StatusBadGateway, "report storage is unavailable")
		return
	}

	key := objectKey(identity.Username, from, to, covered, crmVersion)
	source := "s3"

	exists, err := h.objects.Exists(r.Context(), key)
	if err != nil {
		log.Printf("stat report object: %v", err)
		writeError(w, http.StatusBadGateway, "object storage is unavailable")
		return
	}

	if !exists {
		days, client, err := h.store.Days(r.Context(), identity.Username, from, to)
		if err != nil {
			log.Printf("read mart: %v", err)
			writeError(w, http.StatusBadGateway, "report storage is unavailable")
			return
		}

		report := reports.Build(identity.Username, days, from, to, requestedTo, covered)
		report.Client = client

		if err := h.objects.PutJSON(r.Context(), key, report); err != nil {
			log.Printf("store report object: %v", err)
			writeError(w, http.StatusBadGateway, "object storage is unavailable")
			return
		}
		source = "generated"
	}

	url, expiresAt := h.signer.Sign("/" + h.objects.Bucket() + "/" + key)

	writeJSON(w, http.StatusOK, map[string]any{
		"username":     identity.Username,
		"source":       source,
		"objectKey":    key,
		"url":          url,
		"urlExpiresAt": expiresAt.Format(time.RFC3339),
		"period": map[string]any{
			"from":             from.Format(dateLayout),
			"to":               to.Format(dateLayout),
			"requestedTo":      requestedTo.Format(dateLayout),
			"dataCoveredUntil": covered.Format(dateLayout),
			"truncated":        requestedTo.After(covered),
		},
	})
}

func objectKey(username string, from, to, covered, crmVersion time.Time) string {
	return fmt.Sprintf("username=%s/covered=%s/crm=%d/%s_%s.json",
		username, covered.Format(dateLayout), crmVersion.Unix(),
		from.Format(dateLayout), to.Format(dateLayout))
}

func parseDate(raw string, fallback time.Time) (time.Time, error) {
	if raw == "" {
		return fallback, nil
	}
	return time.Parse(dateLayout, raw)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}
