package task

import (
	"context"
	"testing"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op"
	"github.com/gypg/lodestar/internal/op/setting"
)

// TestInitRegistersCustomerAlertTask（T-C 接线）：守卫钉住 Register 调用点——
// 只测 CustomerAlertTask 函数本身的话，注册行删了测试照样绿（与 M-B1 同构）。
func TestInitRegistersCustomerAlertTask(t *testing.T) {
	resetTaskRegistryForTest(t)

	if _, exists := lookupTaskForTest(TaskCustomerAlert); exists {
		t.Fatalf("task %q already registered before Init(): assertion below would be vacuous", TaskCustomerAlert)
	}

	if err := db.InitDB("sqlite", "file:task_customer_alert_wiring?mode=memory&cache=shared", false); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := op.InitCache(); err != nil {
		t.Fatalf("init cache: %v", err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}

	Init()

	entry, exists := lookupTaskForTest(TaskCustomerAlert)
	if !exists {
		t.Fatalf("task %q not registered after task.Init(): customer alerts would never run on a schedule", TaskCustomerAlert)
	}
	if entry.interval != customerAlertTick {
		t.Fatalf("task %q interval = %v, want %v", TaskCustomerAlert, entry.interval, customerAlertTick)
	}
	if entry.fn == nil {
		t.Fatalf("task %q registered with a nil fn", TaskCustomerAlert)
	}
}

// TestCustomerAlertTaskDisabledByDefault（T-C3 任务侧守卫）：customer_alert_enabled
// 默认 false，任务体必须直接返回——给客户发邮件的决定不替运营者做（与探测默认
// 关闭同一决策，M-B7 同构守卫）。
func TestCustomerAlertTaskDisabledByDefault(t *testing.T) {
	if enabled, err := setting.GetBool(model.SettingKeyCustomerAlertEnabled); err == nil && enabled {
		t.Fatalf("customer_alert_enabled defaults to true, want false: alert emails go to customers, the operator must opt in")
	}
}
