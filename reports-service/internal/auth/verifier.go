package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

var (
	ErrNoToken      = errors.New("bearer token is missing")
	ErrInvalidToken = errors.New("bearer token is invalid")
	ErrForbidden    = errors.New("required role is missing")
)

type Identity struct {
	Subject  string
	Username string
	Email    string
	Roles    []string
}

type Verifier struct {
	verifier     *oidc.IDTokenVerifier
	requiredRole string
}

func NewVerifier(ctx context.Context, internalIssuer, publicIssuer, requiredRole string) (*Verifier, error) {
	discoveryCtx := ctx
	if internalIssuer != publicIssuer {
		discoveryCtx = oidc.InsecureIssuerURLContext(ctx, publicIssuer)
	}

	provider, err := oidc.NewProvider(discoveryCtx, internalIssuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	return &Verifier{
		verifier:     provider.Verifier(&oidc.Config{SkipClientIDCheck: true}),
		requiredRole: requiredRole,
	}, nil
}

func (v *Verifier) Authenticate(r *http.Request) (*Identity, error) {
	raw, ok := bearerToken(r)
	if !ok {
		return nil, ErrNoToken
	}

	token, err := v.verifier.Verify(r.Context(), raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidToken, err)
	}

	var claims struct {
		Username    string `json:"preferred_username"`
		Email       string `json:"email"`
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
	}
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: claims: %s", ErrInvalidToken, err)
	}
	if claims.Username == "" {
		return nil, fmt.Errorf("%w: preferred_username is empty", ErrInvalidToken)
	}
	if v.requiredRole != "" && !slices.Contains(claims.RealmAccess.Roles, v.requiredRole) {
		return nil, ErrForbidden
	}

	return &Identity{
		Subject:  token.Subject,
		Username: claims.Username,
		Email:    claims.Email,
		Roles:    claims.RealmAccess.Roles,
	}, nil
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}
	value, found := strings.CutPrefix(header, "Bearer ")
	if !found || value == "" {
		return "", false
	}
	return value, true
}
