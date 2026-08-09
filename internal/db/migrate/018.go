package migrate

import (
	"fmt"

	"github.com/gypg/lodestar/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 18,
		Up:      migrateChannelProxyMode,
		Down:    stubDown(18),
	})
}

// migrateChannelProxyMode 把渠道既有的 proxy / channel_proxy 语义回填到 proxy_mode。
//
// R-10 之前 channels.proxy_mode 这一列是死的：ChannelUpdate 的 selectFields 白名单
// 里没有它，前端也从不发，代理选择完全由 helper/channel.go 读 proxy + channel_proxy
// 决定。现在 proxy_mode 成为权威字段，若不回填，所有既有渠道都会带着建表默认值
// 'direct' 被当成"不走代理"，线上正在用代理的渠道会**静默失去代理**。
//
// 映射规则与 helper.ChannelHttpClient 修改前的分支逐一对应：
//   - proxy=false                      → direct（原来走 GetHTTPClientSystemProxy(false)）
//   - proxy=true 且 channel_proxy 为空 → system（原来走 GetHTTPClientSystemProxy(true)）
//   - proxy=true 且 channel_proxy 非空 → direct + 保留 channel_proxy
//     自定义 URL 仍由 channel_proxy 承载，pool 模式是另一套（指向代理池配置 ID），
//     不能把自定义 URL 塞进 pool。
//
// 只回填 proxy_mode 仍是初始默认值的行，避免覆盖用户后来真正配过的值（幂等）。
func migrateChannelProxyMode(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.Channel{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&model.Channel{}, "proxy_mode") {
		return nil
	}

	// proxy=true 且未配自定义 URL → 用系统代理。
	if err := db.Model(&model.Channel{}).
		Where("proxy = ?", true).
		Where("channel_proxy IS NULL OR TRIM(channel_proxy) = ''").
		Where("proxy_mode = ? OR proxy_mode IS NULL OR proxy_mode = ''", string(model.ProxyUsageModeDirect)).
		Update("proxy_mode", string(model.ProxyUsageModeSystem)).Error; err != nil {
		return err
	}

	// 其余行（proxy=false，或 proxy=true 但带自定义 URL）保持 direct；
	// 仅把 NULL/空串规整成显式 'direct'，让列值始终合法。
	return db.Model(&model.Channel{}).
		Where("proxy_mode IS NULL OR proxy_mode = ''").
		Update("proxy_mode", string(model.ProxyUsageModeDirect)).Error
}
