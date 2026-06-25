package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

func initQuotaTestDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "-", "\\", "-", " ", "-").Replace(t.Name()))
	if err := db.InitDB("sqlite", dsn, false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.GetDB().AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
}

func TestDeductQuota_neverNegative(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "u1", Password: "x", Quota: 1.0, UsedQuota: 0}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	// First deduction succeeds (1.0 >= 0.6).
	if err := DeductQuota(u.ID, 0.6, ctx); err != nil {
		t.Fatal(err)
	}
	// Second deduction fails: remaining 0.4 < 0.6.
	err := DeductQuota(u.ID, 0.6, ctx)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("want ErrInsufficientBalance, got %v", err)
	}

	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem < 0 {
		t.Fatalf("quota went negative: %v", rem)
	}
	// Only the first deduction went through (0.6 charged).
	if rem != 0.4 {
		t.Fatalf("want quota 0.4 after one successful deduction, got %v", rem)
	}
	if used != 0.6 {
		t.Fatalf("want used_quota 0.6, got %v", used)
	}
}

func TestDeductQuota_insufficientBalance(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "u2", Password: "x", Quota: 0.25, UsedQuota: 10}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	// Attempting to deduct more than available returns ErrInsufficientBalance.
	err := DeductQuota(u.ID, 1.0, ctx)
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("want ErrInsufficientBalance, got %v", err)
	}

	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Balance unchanged — the deduction was rejected.
	if rem != 0.25 {
		t.Fatalf("want 0.25 remaining (unchanged), got %v", rem)
	}
	if used != 10 {
		t.Fatalf("want used 10 (unchanged), got %v", used)
	}
}