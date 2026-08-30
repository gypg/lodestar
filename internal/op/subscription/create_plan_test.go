package subscription

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
)

// SubscriptionPlan.Enabled 带 gorm default:true 标签，adminCreatePlan 直接
// ShouldBindJSON 绑 model——显式的 enabled=false 会被 create 回调吞成 true，
// 停用套餐创建即上架可售。三条测试都真走 JSON 反序列化（adminCreatePlan
// 就是这么绑的），钉住显式 false / 显式 true / 字段缺失三种形状。

func TestCreatePlanPersistsExplicitDisabled(t *testing.T) {
	initSubTestDB(t)

	plan, err := unmarshalPlan(`{"name":"b5-disabled","enabled":false,"price":9.9,"duration_type":"month","duration_days":30}`)
	if err != nil {
		t.Fatalf("unmarshal plan failed: %v", err)
	}
	if err := CreatePlan(plan, ctxBackground()); err != nil {
		t.Fatalf("CreatePlan returned error: %v", err)
	}

	if plan.Enabled {
		t.Fatalf("create returned plan with enabled=true; caller asked for false")
	}

	saved := loadPlanByID(t, plan.ID)
	if saved.Enabled {
		t.Fatalf("user asked for enabled=false; stored enabled=true")
	}
}

func TestCreatePlanPersistsExplicitEnabled(t *testing.T) {
	initSubTestDB(t)

	plan, err := unmarshalPlan(`{"name":"b5-enabled","enabled":true,"price":19.9,"duration_type":"month","duration_days":30}`)
	if err != nil {
		t.Fatalf("unmarshal plan failed: %v", err)
	}
	if err := CreatePlan(plan, ctxBackground()); err != nil {
		t.Fatalf("CreatePlan returned error: %v", err)
	}

	saved := loadPlanByID(t, plan.ID)
	if !saved.Enabled {
		t.Fatalf("user asked for enabled=true; stored enabled=false")
	}
}

func TestCreatePlanWithoutEnabledFieldDefaultsToEnabled(t *testing.T) {
	initSubTestDB(t)

	// 老客户端的形状：JSON 里没有 "enabled" 键，必须保持默认启用。
	plan, err := unmarshalPlan(`{"name":"b5-missing","price":5,"duration_type":"month","duration_days":30}`)
	if err != nil {
		t.Fatalf("unmarshal plan failed: %v", err)
	}
	if err := CreatePlan(plan, ctxBackground()); err != nil {
		t.Fatalf("CreatePlan returned error: %v", err)
	}

	saved := loadPlanByID(t, plan.ID)
	if !saved.Enabled {
		t.Fatalf("expected plan without explicit enabled to default to enabled=true, got enabled=false")
	}
}

func unmarshalPlan(body string) (*model.SubscriptionPlan, error) {
	plan := &model.SubscriptionPlan{}
	if err := json.Unmarshal([]byte(body), plan); err != nil {
		return nil, err
	}
	return plan, nil
}

func ctxBackground() context.Context {
	return context.Background()
}

func loadPlanByID(t *testing.T, id int) model.SubscriptionPlan {
	t.Helper()
	var saved model.SubscriptionPlan
	if err := db.GetDB().First(&saved, id).Error; err != nil {
		t.Fatalf("load subscription plan %d failed: %v", id, err)
	}
	return saved
}
