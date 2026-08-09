package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/relaylog"
	"github.com/gypg/lodestar/internal/op/setting"
)

// R-9 调用点守卫。
//
// relay 侧曾在 RelayLogAdd 之后立即调 RelayLogAttemptsAdd 写明细，而 RelayLogAdd
// 只进内存缓存 —— 明细因此先于父日志落库。修复是删掉这两处 eager 写，改由
// relayLogFlushToDB 随父日志同批写入。
//
// ★ 这两个测试的入口是 Save / MediaHandler 下游的真实收尾路径（不是
// relaylog 包内部函数），因为要守的是"relay 有没有多写一次明细"这个调用点行为。
// 若把 eager 写加回去，明细会被写两遍：eager 一次 + 刷盘一次。断言落在行数上。
// 实测：不加这两个测试时，把 metrics.go 的 eager 写还原回去，全仓测试照绿。

func countAttemptRows(t *testing.T, relayLogID int64) int64 {
	t.Helper()
	var n int64
	if err := db.GetLogDB().Model(&model.RelayLogAttempt{}).
		Where("relay_log_id = ?", relayLogID).Count(&n).Error; err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	return n
}

// TestMediaHandlerDoesNotWriteAttemptRowsBeforeFlush 守 media 侧的调用点。
// 入口是 MediaHandler（真实生产入口），复用 media_handler_callsite_test.go 的
// harness；这里只额外准备日志库与日志表。
//
// media 路径成功时只有一次尝试，故"写两遍"表现为 2 行而不是 1 行；更早的
// "刷盘前就有行"断言同样会红。
func TestMediaHandlerDoesNotWriteAttemptRowsBeforeFlush(t *testing.T) {
	const requestModel = "attempts-callsite-image"
	const expr = `1.0`
	env := initMediaCallsiteEnv(t, requestModel, expr)

	// initMediaCallsiteEnv 只迁移了 Setting 表；日志落库还需要这两张表 + 日志库句柄。
	if err := db.GetDB().AutoMigrate(&model.RelayLog{}, &model.RelayLogAttempt{}); err != nil {
		t.Fatalf("migrate log tables: %v", err)
	}
	if err := db.InitLogDB("", "", false); err != nil {
		t.Fatalf("InitLogDB(shared): %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable keep: %v", err)
	}
	t.Cleanup(relaylog.SetCacheForTest(nil))

	req := newMultipartImageEditRequest(t, requestModel, "1024x1024")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key_id", 55901)

	MediaHandler(MediaEndpointImageEdit, c)

	if rec.Code != http.StatusOK {
		t.Fatalf("MediaHandler status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	// 前置校验：请求真到了假上游，否则下面的断言是空转。
	if got := env.upstreamGotFields["size"]; got != "1024x1024" {
		t.Fatalf("upstream never received the request (size=%q)", got)
	}

	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	if len(logs) == 0 {
		lock.Unlock()
		t.Fatal("relay log cache empty after MediaHandler")
	}
	logID := logs[len(logs)-1].ID
	attemptCount := len(logs[len(logs)-1].Attempts)
	lock.Unlock()

	if attemptCount == 0 {
		t.Fatal("relayLog.Attempts empty — 拓扑不对，明细无从写起")
	}

	if got := countAttemptRows(t, logID); got != 0 {
		t.Fatalf("before flush: attempt rows = %d, want 0 (media 抢先写了明细)", got)
	}

	if err := relaylog.RelayLogSaveDBTask(context.Background()); err != nil {
		t.Fatalf("RelayLogSaveDBTask: %v", err)
	}

	if got := countAttemptRows(t, logID); got != int64(attemptCount) {
		t.Fatalf("after flush: attempt rows = %d, want exactly %d (翻倍 = eager 写被加回来了)", got, attemptCount)
	}
}

// TestSaveDoesNotDoubleWriteAttemptRows 驱动 RelayMetrics.Save（LLM relay 的终态
// 收尾），然后强制刷盘，断言每次尝试恰好落一行。
func TestSaveDoesNotDoubleWriteAttemptRows(t *testing.T) {
	initRelayMetricsTestDB(t)
	if err := db.InitLogDB("", "", false); err != nil {
		t.Fatalf("InitLogDB(shared): %v", err)
	}
	if err := setting.RefreshCache(context.Background()); err != nil {
		t.Fatalf("RefreshCache: %v", err)
	}
	if err := setting.SetString(model.SettingKeyRelayLogKeepEnabled, "true"); err != nil {
		t.Fatalf("enable keep: %v", err)
	}
	t.Cleanup(relaylog.SetCacheForTest(nil))

	attempts := []model.ChannelAttempt{
		{ChannelID: 91, ChannelName: "ch-a", ModelName: "m", Status: model.AttemptFailed, Duration: 10},
		{ChannelID: 92, ChannelName: "ch-b", ModelName: "m", Status: model.AttemptSuccess, Duration: 20},
	}
	newUsageMetrics(78001, "attempts-callsite", 0, 5, 5).Save(true, nil, attempts)

	// 取出刚写入缓存的日志 ID。
	logs, lock := relaylog.GetCacheAndLock()
	lock.Lock()
	if len(logs) == 0 {
		lock.Unlock()
		t.Fatal("relay log cache empty after Save")
	}
	logID := logs[len(logs)-1].ID
	lock.Unlock()

	// 刷盘前：明细一行都不该有（relay 不得抢在父日志之前写）。
	if got := countAttemptRows(t, logID); got != 0 {
		t.Fatalf("before flush: attempt rows = %d, want 0 (relay 抢先写了明细)", got)
	}

	// 用生产已有的导出入口刷盘，不为测试新增导出面。
	if err := relaylog.RelayLogSaveDBTask(context.Background()); err != nil {
		t.Fatalf("RelayLogSaveDBTask: %v", err)
	}

	// 刷盘后：恰好 2 行。写两遍会变 4 行。
	if got := countAttemptRows(t, logID); got != 2 {
		t.Fatalf("after flush: attempt rows = %d, want exactly 2 (4 = eager 写被加回来了)", got)
	}
}
