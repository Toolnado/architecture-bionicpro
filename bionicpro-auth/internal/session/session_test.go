package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/bionicpro/bionicpro-auth/internal/secure"
)

func newTestStore(t *testing.T) (*Store, *redis.Client) {
	t.Helper()

	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis is not reachable at %s: %v", addr, err)
	}

	cipher, err := secure.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })

	return NewStore(rdb, cipher, time.Minute), rdb
}

func TestRotateRebindsTokensAndDropsPreviousID(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx, &Session{Subject: "sub-1", Access: "access-1", Refresh: "refresh-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	previousID := created.ID
	t.Cleanup(func() { _ = store.Delete(ctx, previousID) })

	rotated, err := store.Rotate(ctx, created)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ctx, rotated.ID) })

	if rotated.ID == previousID {
		t.Fatal("rotate must issue a new session id")
	}
	if _, err := store.Get(ctx, previousID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("previous session id must be gone, got %v", err)
	}

	current, err := store.Get(ctx, rotated.ID)
	if err != nil {
		t.Fatalf("get rotated: %v", err)
	}
	if current.Access != "access-1" || current.Refresh != "refresh-1" {
		t.Fatalf("tokens are not rebound to the new id: %+v", current)
	}
}

func TestRefreshTokenIsEncryptedAtRest(t *testing.T) {
	store, rdb := newTestStore(t)
	ctx := context.Background()

	created, err := store.Create(ctx, &Session{Subject: "sub-2", Access: "access-2", Refresh: "refresh-secret"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _ = store.Delete(ctx, created.ID) })

	raw, err := rdb.Get(ctx, sessionKey(created.ID)).Bytes()
	if err != nil {
		t.Fatalf("read raw session: %v", err)
	}

	var stored Session
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("unmarshal raw session: %v", err)
	}
	if stored.Refresh != "" {
		t.Fatal("plaintext refresh token must not be serialized")
	}
	if stored.RefreshEnc == "" || stored.RefreshEnc == "refresh-secret" {
		t.Fatalf("refresh token is not encrypted at rest: %q", stored.RefreshEnc)
	}

	loaded, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if loaded.Refresh != "refresh-secret" {
		t.Fatalf("refresh token did not survive the round trip: %q", loaded.Refresh)
	}
}

func TestLoginStateIsSingleUse(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	if err := store.SaveLogin(ctx, "state-1", LoginState{Verifier: "v", Nonce: "n", Return: "/"}, time.Minute); err != nil {
		t.Fatalf("save login: %v", err)
	}

	if _, err := store.TakeLogin(ctx, "state-1"); err != nil {
		t.Fatalf("first take: %v", err)
	}
	if _, err := store.TakeLogin(ctx, "state-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("login state must be consumed once, got %v", err)
	}
}
