package oidc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	authURL   string
	tokenURL  string
	logoutURL string
	clientID  string
	secret    string
	http      *http.Client
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

type Claims struct {
	Subject  string
	Username string
	Email    string
	Roles    []string
	Expiry   time.Time
}

func New(publicURL, internalURL, realm, clientID, secret string) *Client {
	public := endpointBase(publicURL, realm)
	internal := endpointBase(internalURL, realm)
	return &Client{
		authURL:   public + "/auth",
		tokenURL:  internal + "/token",
		logoutURL: internal + "/logout",
		clientID:  clientID,
		secret:    secret,
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

func endpointBase(baseURL, realm string) string {
	return fmt.Sprintf("%s/realms/%s/protocol/openid-connect", strings.TrimRight(baseURL, "/"), realm)
}

func (c *Client) AuthCodeURL(redirectURI, state, nonce, challenge string) string {
	q := url.Values{
		"client_id":             {c.clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {"openid profile email"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	return c.authURL + "?" + q.Encode()
}

func (c *Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (*Tokens, error) {
	return c.postForm(ctx, c.tokenURL, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	return c.postForm(ctx, c.tokenURL, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (c *Client) Logout(ctx context.Context, refreshToken string) error {
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.secret},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.logoutURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("build logout request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("logout request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("logout failed: %s: %s", resp.Status, body)
	}
	return nil
}

func (c *Client) postForm(ctx context.Context, endpoint string, form url.Values) (*Tokens, error) {
	form.Set("client_id", c.clientID)
	form.Set("client_secret", c.secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %s: %s", resp.Status, body)
	}

	var tokens Tokens
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("unmarshal tokens: %w", err)
	}
	return &tokens, nil
}

func ParseClaims(accessToken string) (*Claims, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("malformed jwt: expected 3 segments, got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode jwt payload: %w", err)
	}

	var raw struct {
		Subject     string `json:"sub"`
		Username    string `json:"preferred_username"`
		Email       string `json:"email"`
		Expiry      int64  `json:"exp"`
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal jwt payload: %w", err)
	}

	return &Claims{
		Subject:  raw.Subject,
		Username: raw.Username,
		Email:    raw.Email,
		Roles:    raw.RealmAccess.Roles,
		Expiry:   time.Unix(raw.Expiry, 0).UTC(),
	}, nil
}
