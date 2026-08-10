package channel

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gypg/lodestar/internal/model"
)

// 哨兵错误：用来确认执行停在预期的那一步，而不是被别处的 error 顺带满足。
var (
	errProxyConfigProbe = errors.New("proxy config probe failed")
	errStopBeforeDB     = errors.New("stop before db write")
)

// Create 此前完全不校验代理模式（只有 Update 校验）。于是经 create 落库的渠道
// 可以带着非法 proxy_mode、或 pool 模式却没有 proxy_config_id —— 保存时一切正常，
// 直到每个真实请求在 helper.ChannelHttpClient 取 client 时才失败。
//
// 这些用例只走到校验分支就返回（不碰 DB），故不需要初始化数据库：
// 校验必须发生在写库之前，这本身也是被守卫的性质之一。
func TestCreateValidatesProxyMode(t *testing.T) {
	t.Run("rejects an unsupported proxy mode", func(t *testing.T) {
		ch := &model.Channel{Name: "bad-mode", ProxyMode: "bogus"}
		err := Create(ch, context.Background())
		if err == nil {
			t.Fatal("Create 返回 nil error，want 校验失败")
		}
		if !strings.Contains(err.Error(), "unsupported proxy mode") {
			t.Fatalf("err = %v, want 含 'unsupported proxy mode'", err)
		}
	})

	t.Run("rejects inherit (channels have no parent to inherit from)", func(t *testing.T) {
		ch := &model.Channel{Name: "inherit", ProxyMode: model.ProxyUsageModeInherit}
		err := Create(ch, context.Background())
		if err == nil {
			t.Fatal("Create 接受了 inherit，want 拒绝")
		}
		// 断言具体错误：只断言 "err != nil" 的话，删掉校验后 Create 仍会因
		// GroupDefaultID 未注册而报错，用例照样绿（该变异实测存活过）。
		if !strings.Contains(err.Error(), "unsupported proxy mode") {
			t.Fatalf("err = %v, want 含 'unsupported proxy mode'", err)
		}
	})

	t.Run("rejects pool mode without a config id", func(t *testing.T) {
		ch := &model.Channel{Name: "pool-no-id", ProxyMode: model.ProxyUsageModePool}
		err := Create(ch, context.Background())
		if err == nil {
			t.Fatal("Create 返回 nil error，want 校验失败")
		}
		if !strings.Contains(err.Error(), "proxy config id is required") {
			t.Fatalf("err = %v, want 含 'proxy config id is required'", err)
		}
	})

	t.Run("rejects pool mode with a non-positive config id", func(t *testing.T) {
		zero := 0
		ch := &model.Channel{Name: "pool-zero", ProxyMode: model.ProxyUsageModePool, ProxyConfigID: &zero}
		err := Create(ch, context.Background())
		if err == nil {
			t.Fatal("Create 接受了 proxy_config_id=0，want 拒绝")
		}
		if !strings.Contains(err.Error(), "proxy config id is required") {
			t.Fatalf("err = %v, want 含 'proxy config id is required'", err)
		}
	})

	t.Run("pool mode validates the referenced config exists and is enabled", func(t *testing.T) {
		prev := ProxyURLForConfig
		t.Cleanup(func() { ProxyURLForConfig = prev })

		gotID := -1
		ProxyURLForConfig = func(id int, _ context.Context) (string, error) {
			gotID = id
			return "", errProxyConfigProbe
		}

		id := 42
		ch := &model.Channel{Name: "pool-probe", ProxyMode: model.ProxyUsageModePool, ProxyConfigID: &id}
		err := Create(ch, context.Background())
		if err == nil {
			t.Fatal("Create 忽略了代理池配置校验失败，want 返回 error")
		}
		if gotID != 42 {
			t.Errorf("ProxyURLForConfig 收到 id = %d, want 42", gotID)
		}
	})

	t.Run("empty proxy mode is normalized to direct for internal callers", func(t *testing.T) {
		// 远端站点导入（op/remotesite）构造 model.Channel 时不设这个字段。
		// 空值必须被兜底成 direct，否则这些内部调用方会被新校验挡掉，
		// 且 DB 里会留下非法的空枚举值。
		prev := GroupDefaultID
		t.Cleanup(func() { GroupDefaultID = prev })
		GroupDefaultID = func(context.Context) (int, error) { return 0, errStopBeforeDB }

		ch := &model.Channel{Name: "legacy-internal"}
		err := Create(ch, context.Background())
		if err != errStopBeforeDB {
			t.Fatalf("err = %v, want 走到 GroupDefaultID（即通过了代理模式校验）", err)
		}
		if ch.ProxyMode != model.ProxyUsageModeDirect {
			t.Fatalf("ch.ProxyMode = %q, want %q", ch.ProxyMode, model.ProxyUsageModeDirect)
		}
	})
}
