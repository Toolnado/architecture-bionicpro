package cdn

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

type Signer struct {
	baseURL string
	secret  string
	ttl     time.Duration
}

func NewSigner(baseURL, secret string, ttl time.Duration) *Signer {
	return &Signer{baseURL: strings.TrimRight(baseURL, "/"), secret: secret, ttl: ttl}
}

func (s *Signer) TTL() time.Duration { return s.ttl }

func (s *Signer) Sign(objectPath string) (string, time.Time) {
	if !strings.HasPrefix(objectPath, "/") {
		objectPath = "/" + objectPath
	}
	expiresAt := time.Now().Add(s.ttl).UTC()
	expires := expiresAt.Unix()

	sum := md5.Sum([]byte(fmt.Sprintf("%d%s %s", expires, objectPath, s.secret)))
	token := base64.RawURLEncoding.EncodeToString(sum[:])

	return fmt.Sprintf("%s%s?token=%s&expires=%d", s.baseURL, objectPath, token, expires), expiresAt
}
