package helper

import (
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
)

// defaultRatelimitCooldownSeconds 与 model.SettingKeyRatelimitCooldown 的出厂值
// （internal/model/setting.go）和 internal/relay 的 defaultRatelimitCooldown 一致。
// 只在设置读不出来时兜底，正常路径永远读实际值。
const defaultRatelimitCooldownSeconds = 300

// ratelimitCooldownSeconds 返回 Key 错误冷却时长（秒）；0 表示关闭冷却。
//
// key 选取函数（Channel.GetChannelKeyExcludingWithCooldownForModel）只接受显式
// 传入的秒数，不自己读设置 —— internal/op/setting 依赖 internal/model，model 反向
// 读设置会成环。所以每个调用方都必须自己读，漏读就等于把这个旋钮焊死。
//
// 历史缺陷：Channel 上曾有个无参 GetChannelKey() 写死 300，helper 的四个调用点全
// 用它，于是把 ratelimit_cooldown 调成 0（关闭）对模型拉取和分组探测完全无效，
// 冷却中的 key 仍被跳过 300 秒。那两个写死 300 的便捷包装已删除，以免再被误用。
func ratelimitCooldownSeconds() int {
	v, err := setting.GetInt(model.SettingKeyRatelimitCooldown)
	if err != nil || v < 0 {
		return defaultRatelimitCooldownSeconds
	}
	return v
}
