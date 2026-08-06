package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bionicpro/bionicpro-auth/internal/secure"
)

var ErrNotFound = errors.New("session not found")

type Session struct {
	ID         string    `json:"id"`
	Subject    string    `json:"sub"`
	Username   string    `json:"username"`
	Email      string    `json:"email"`
	Roles      []string  `json:"roles"`
	Access     string    `json:"access_token"`
	IDToken    string    `json:"id_token"`
	RefreshEnc string    `json:"refresh_enc"`
	AccessExp  time.Time `json:"access_exp"`
	CreatedAt  time.Time `json:"created_at"`
	RotatedAt  time.Time `json:"rotated_at"`

	Refresh string `json:"-"`
}

type LoginState struct {
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
	Return   string `json:"return"`
}

type Store struct {
	rdb    *redis.Client
	cipher *secure.Cipher
	ttl    time.Duration
}

func NewStore(rdb *redis.Client, cipher *secure.Cipher, ttl time.Duration) *Store {
	return &Store{rdb: rdb, cipher: cipher, ttl: ttl}
}

func (s *Store) TTL() time.Duration { return s.ttl }

func sessionKey(id string) string { return "session:" + id }

func loginKey(state string) string { return "login:" + state }

func (s *Store) Create(ctx context.Context, sess *Session) (*Session, error) {
	id, err := secure.Token(32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	sess.ID = id
	sess.CreatedAt = now
	sess.RotatedAt = now
	if err := s.persist(ctx, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	raw, err := s.rdb.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis get: %w", err)
	}

	var sess Session
	if err := json.Unmarshal(raw, &sess); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	if sess.RefreshEnc != "" {
		refresh, err := s.cipher.Decrypt(sess.RefreshEnc)
		if err != nil {
			return nil, fmt.Errorf("decrypt refresh token: %w", err)
		}
		sess.Refresh = refresh
	}
	return &sess, nil
}

func (s *Store) Save(ctx context.Context, sess *Session) error {
	return s.persist(ctx, sess)
}

func (s *Store) Rotate(ctx context.Context, sess *Session) (*Session, error) {
	previous := sess.ID

	id, err := secure.Token(32)
	if err != nil {
		return nil, err
	}
	sess.ID = id
	sess.RotatedAt = time.Now().UTC()

	if err := s.persist(ctx, sess); err != nil {
		return nil, err
	}
	if err := s.rdb.Del(ctx, sessionKey(previous)).Err(); err != nil {
		return nil, fmt.Errorf("redis del: %w", err)
	}
	return sess, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.rdb.Del(ctx, sessionKey(id)).Err(); err != nil {
		return fmt.Errorf("redis del: %w", err)
	}
	return nil
}

func (s *Store) SaveLogin(ctx context.Context, state string, ls LoginState, ttl time.Duration) error {
	raw, err := json.Marshal(ls)
	if err != nil {
		return fmt.Errorf("marshal login state: %w", err)
	}
	if err := s.rdb.Set(ctx, loginKey(state), raw, ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

func (s *Store) TakeLogin(ctx context.Context, state string) (*LoginState, error) {
	raw, err := s.rdb.GetDel(ctx, loginKey(state)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("redis getdel: %w", err)
	}

	var ls LoginState
	if err := json.Unmarshal(raw, &ls); err != nil {
		return nil, fmt.Errorf("unmarshal login state: %w", err)
	}
	return &ls, nil
}

func (s *Store) persist(ctx context.Context, sess *Session) error {
	if sess.Refresh != "" {
		encrypted, err := s.cipher.Encrypt(sess.Refresh)
		if err != nil {
			return fmt.Errorf("encrypt refresh token: %w", err)
		}
		sess.RefreshEnc = encrypted
	}

	raw, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if err := s.rdb.Set(ctx, sessionKey(sess.ID), raw, s.ttl).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}
