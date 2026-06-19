package user

import (
	"context"
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

	if err := DeductQuota(u.ID, 0.6, ctx); err != nil {
		t.Fatal(err)
	}
	if err := DeductQuota(u.ID, 0.6, ctx); err != nil {
		t.Fatal(err)
	}

	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem < 0 {
		t.Fatalf("quota went negative: %v", rem)
	}
	if rem != 0 {
		t.Fatalf("want quota 0 after capped deductions, got %v", rem)
	}
	if used != 1.0 {
		t.Fatalf("want used_quota 1.0 (only remaining balance charged), got %v", used)
	}
}

func TestDeductQuota_partialWhenInsufficient(t *testing.T) {
	initQuotaTestDB(t)
	ctx := context.Background()
	u := model.User{Username: "u2", Password: "x", Quota: 0.25, UsedQuota: 10}
	if err := db.GetDB().Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	if err := DeductQuota(u.ID, 1.0, ctx); err != nil {
		t.Fatal(err)
	}
	rem, used, err := GetQuota(u.ID, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rem != 0 {
		t.Fatalf("want 0 remaining, got %v", rem)
	}
	if used != 10.25 {
		t.Fatalf("want used 10.25, got %v", used)
	}
}