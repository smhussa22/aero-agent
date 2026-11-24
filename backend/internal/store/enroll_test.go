package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/smhussa22/aero-agent/backend/internal/store"
	"github.com/smhussa22/aero-agent/backend/internal/testdb"
)

func TestEnrollCode(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	a := testdb.Seed(t, pool, "enr")

	code, _, err := store.CreateEnrollCode(ctx, pool, a.OrgID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RedeemEnrollCode(ctx, pool, code, "bad name!"); !errors.Is(err, store.ErrBadName) {
		t.Fatalf("bad name: got %v", err)
	}
	hostID, token, err := store.RedeemEnrollCode(ctx, pool, code, "web-new")
	if err != nil || hostID == "" || token == "" {
		t.Fatalf("redeem: %v", err)
	}
	if id, err := (store.TokenStore{Pool: pool}).IdentityForToken(ctx, token); err != nil || id.OrgID != a.OrgID {
		t.Fatalf("token should belong to the code's org: %v %v", id, err)
	}
	if _, _, err := store.RedeemEnrollCode(ctx, pool, code, "web-new2"); !errors.Is(err, store.ErrBadCode) {
		t.Fatalf("second use must fail, got %v", err)
	}

	expired, _, _ := store.CreateEnrollCode(ctx, pool, a.OrgID, "")
	pool.Exec(ctx, "UPDATE enroll_codes SET expires_at = now() - interval '1 second' WHERE org_id = $1::uuid AND used_at IS NULL", a.OrgID)
	if _, _, err := store.RedeemEnrollCode(ctx, pool, expired, "web-x"); !errors.Is(err, store.ErrBadCode) {
		t.Fatalf("expired code must fail, got %v", err)
	}
}
