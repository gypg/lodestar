package model

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

type SettingKey string

const (
	SettingKeyProxyURL                             SettingKey = "proxy_url"
	SettingKeyStatsSaveInterval                    SettingKey = "stats_save_interval"                      // 将统计信息写入数据库的周期(分钟)
	SettingKeyModelInfoUpdateInterval              SettingKey = "model_info_update_interval"               // 模型信息更新间隔(小时)
	SettingKeySyncLLMInterval                      SettingKey = "sync_llm_interval"                        // LLM 同步间隔(小时)
	SettingKeyRelayLogKeepPeriod                   SettingKey = "relay_log_keep_period"                    // 日志保存时间范围(天)
	SettingKeyRelayLogKeepCount                    SettingKey = "relay_log_keep_count"                     // 日志保留条数(0=不按条数)
	SettingKeyRelayLogKeepEnabled                  SettingKey = "relay_log_keep_enabled"                   // 是否保留历史日志
	SettingKeyCORSAllowOrigins                     SettingKey = "cors_allow_origins"                       // 跨域白名单(逗号分隔, 如 "example.com,example2.com"). 为空不允许跨域, "*"允许所有
	SettingKeyRelayRetryCount                      SettingKey = "relay_retry_count"                        // 单个候选渠道内 Key 级最大重试次数
	SettingKeyRelayRouteRetries                    SettingKey = "relay_route_retries"                      // 路由级最大重试次数（全部渠道遍历一轮算一次）
	SettingKeyCircuitBreakerThreshold              SettingKey = "circuit_breaker_threshold"                // 熔断触发阈值（连续失败次数）
	SettingKeyCircuitBreakerCooldown               SettingKey = "circuit_breaker_cooldown"                 // 熔断基础冷却时间（秒）
	SettingKeyCircuitBreakerMaxCooldown            SettingKey = "circuit_breaker_max_cooldown"             // 熔断最大冷却时间（秒），指数退避上限
	SettingKeyPublicAPIBaseURL                     SettingKey = "public_api_base_url"                      // 对外可访问的 API 基础地址，用于生成示例
	SettingKeyAlertNotifyLanguage                  SettingKey = "alert_notify_language"                    // 告警通知发送语言
	SettingKeyRatelimitCooldown                    SettingKey = "ratelimit_cooldown"                       // Key 错误冷却时间（秒），0=关闭
	SettingKeyRelayMaxTotalAttempts                SettingKey = "relay_max_total_attempts"                 // 所有候选渠道的最大总尝试次数，0 表示不限制
	SettingKeyAutoStrategyMinSamples               SettingKey = "auto_strategy_min_samples"                // Auto策略最小样本数阈值
	SettingKeyAutoStrategyTimeWindow               SettingKey = "auto_strategy_time_window"                // Auto策略时间窗口（秒）
	SettingKeyAutoStrategySampleThreshold          SettingKey = "auto_strategy_sample_threshold"           // Auto策略滑动窗口大小
	SettingKeyAutoStrategyLatencyWeight            SettingKey = "auto_strategy_latency_weight"             // Auto策略延迟权重（0-100）
	SettingKeySemanticCacheEnabled                 SettingKey = "semantic_cache_enabled"                   // 语义缓存开关
	SettingKeySemanticCacheTTL                     SettingKey = "semantic_cache_ttl"                       // 语义缓存 TTL（秒）
	SettingKeySemanticCacheThreshold               SettingKey = "semantic_cache_threshold"                 // 语义缓存相似度阈值（0-1）
	SettingKeySemanticCacheMaxEntries              SettingKey = "semantic_cache_max_entries"               // 语义缓存最大条目数
	SettingKeySemanticCacheEmbeddingBaseURL        SettingKey = "semantic_cache_embedding_base_url"        // 语义缓存 embedding 服务 Base URL
	SettingKeySemanticCacheEmbeddingAPIKey         SettingKey = "semantic_cache_embedding_api_key"         // 语义缓存 embedding 服务 API Key
	SettingKeySemanticCacheEmbeddingModel          SettingKey = "semantic_cache_embedding_model"           // 语义缓存 embedding 模型名称
	SettingKeySemanticCacheEmbeddingTimeoutSeconds SettingKey = "semantic_cache_embedding_timeout_seconds" // 语义缓存 embedding 请求超时（秒）
	SettingKeyNavOrder                             SettingKey = "nav_order"                                // 顶级页面顺序(JSON)
	SettingKeyNavVisible                           SettingKey = "nav_visible"                              // 顶级页面显示状态(JSON)
	SettingKeyAIRouteGroupID                       SettingKey = "ai_route_group_id"                        // AI路由目标分组 ID
	SettingKeyAIRouteBaseURL                       SettingKey = "ai_route_base_url"                        // AI路由分析服务 Base URL
	SettingKeyAIRouteAPIKey                        SettingKey = "ai_route_api_key"                         // AI路由分析服务 API Key
	SettingKeyAIRouteModel                         SettingKey = "ai_route_model"                           // AI路由分析模型名称
	SettingKeyAIRouteTimeoutSeconds                SettingKey = "ai_route_timeout_seconds"                 // AI路由分析单次请求超时（秒）
	SettingKeyAIRouteParallelism                   SettingKey = "ai_route_parallelism"                     // AI路由分析批次最大并发数
	SettingKeyAIRouteServices                      SettingKey = "ai_route_services"                        // AI路由分析服务池(JSON)
	SettingKeyStatsTimezoneOffset                  SettingKey = "stats_timezone_offset"                    // 统计时区偏移（小时），当前为整型偏移；未来计划新增 stats_timezone (IANA) 配置项，此处为定义与校验入口
	SettingKeyJWTDefaultExpiryMinutes              SettingKey = "jwt_default_expiry_minutes"               // 默认JWT过期时间（分钟）
	SettingKeyJWTRememberMeExpiryDays              SettingKey = "jwt_remember_me_expiry_days"              // 记住我JWT过期时间（天）
	SettingKeyLoginRateLimitWindow                 SettingKey = "login_rate_limit_window"                  // 登录限流时间窗口（分钟）
	SettingKeyLoginRateLimitMaxFailed              SettingKey = "login_rate_limit_max_failed"              // 登录限流最大失败次数
	SettingKeyStreamSessionTTLMinutes              SettingKey = "stream_session_ttl_minutes"               // 流会话TTL（分钟）
	SettingKeyStreamSessionMaxEvents               SettingKey = "stream_session_max_events"                // 流会话最大事件数
	SettingKeyStreamSessionMaxBytesMB              SettingKey = "stream_session_max_bytes_mb"              // 流会话最大字节数（MB）
	SettingKeyNotifyHTTPTimeoutSeconds             SettingKey = "notify_http_timeout_seconds"              // 通知HTTP请求超时（秒）
	SettingKeyFailureHintTTLUnauthorized           SettingKey = "failure_hint_ttl_unauthorized"            // 认证失败提示缓存TTL（秒）
	SettingKeyFailureHintTTLRateLimit              SettingKey = "failure_hint_ttl_rate_limit"              // 限流失败提示缓存TTL（秒）
	SettingKeyFailureHintTTLNetwork                SettingKey = "failure_hint_ttl_network"                 // 网络失败提示缓存TTL（秒）
	SettingKeyWebDAVConfig                         SettingKey = "webdav_config"                            // WebDAV 云备份配置（JSON）
	SettingKeySiteSyncInterval                     SettingKey = "site_sync_interval"                       // 站点账号同步间隔（小时）
	SettingKeySiteCheckinInterval                  SettingKey = "site_checkin_interval"                    // 站点自动签到间隔（小时）
	SettingKeyStatsSiteModelBackfilled             SettingKey = "stats_site_model_backfilled"              // 站点模型统计回填标记
	SettingKeyProjectedChannelAutoGroupEnabled     SettingKey = "projected_channel_auto_group_enabled"     // 站点投影渠道自动分组全局开关
	SettingKeyResponseFilterEnabled                SettingKey = "response_filter_enabled"                  // 输出结果关键词拦截开关
	SettingKeyResponseFilterKeywords               SettingKey = "response_filter_keywords"                 // 拦截关键词列表(JSON 数组)
	SettingKeyResponseFilterAction                 SettingKey = "response_filter_action"                   // 拦截动作: block(阻断) / replace(替换为*)
	SettingKeyResponseFilterErrorMessage           SettingKey = "response_filter_error_message"            // 阻断时返回的错误信息
	SettingKeyLogLevel                             SettingKey = "log_level"                                // 应用日志级别: debug, info, warn, error
	SettingKeyLogExcludedGroups                    SettingKey = "log_excluded_groups"                      // 在日志列表/实时流中屏蔽的分组名称列表(JSON 数组)
	SettingKeyWebAuthnRPID                         SettingKey = "webauthn_rp_id"                           // WebAuthn RP ID（域名，不含协议/端口）
	SettingKeyWebAuthnRPName                       SettingKey = "webauthn_rp_name"                         // WebAuthn RP 展示名
	SettingKeyWebAuthnOrigins                      SettingKey = "webauthn_origins"                         // WebAuthn 允许的 Origin 列表（逗号分隔，完整 scheme://host[:port]）
	SettingKeyCustomThemes                         SettingKey = "custom_themes"                            // Lodestar 自定义主题预设(JSON 数组), 可经设置 API 上传, 全站可选
	SettingKeyCommercialMode                       SettingKey = "commercial_mode"                          // Lodestar 商业模式开关: false=自用(关闭公开注册), true=商业(开放公开注册)
	SettingKeySiteName                             SettingKey = "site_name"                                // Lodestar 站点名称(对外展示/封面刊头)
	SettingKeySiteDescription                      SettingKey = "site_description"                         // Lodestar 站点简介(关于本站)
	SettingKeySiteAnnouncement                     SettingKey = "site_announcement"                        // Lodestar 站点公告(对外公开展示)
	SettingKeySiteFooter                           SettingKey = "site_footer"                              // Lodestar 页脚文案
	SettingKeyLandingAmbientMode                   SettingKey = "landing_ambient_mode"                     // Lodestar 封面氛围: photo | classic | color4bg (失败回退 photo)
	SettingKeyEpayEnabled                          SettingKey = "epay_enabled"                             // Lodestar 易支付开关
	SettingKeyPayAddress                           SettingKey = "pay_address"                              // 易支付网关地址 (https://xxx)
	SettingKeyEpayPID                              SettingKey = "epay_pid"                                 // 易支付商户 PID
	SettingKeyEpayKey                              SettingKey = "epay_key"                                 // 易支付商户密钥
	SettingKeyTopupRate                            SettingKey = "topup_rate"                               // 充值汇率: 每 1 USD 额度对应的支付金额(网关货币)
	SettingKeyPaymentCallbackBase                  SettingKey = "payment_callback_base"                    // 支付回调站点基址 (https://your-site)，用于 notify/return
	SettingKeyMaintenanceMode                      SettingKey = "maintenance_mode"                         // Lodestar 维护模式: true=对非管理员显示维护页
	SettingKeyRegisterInviteRequired               SettingKey = "register_invite_required"                 // Lodestar 注册需邀请码(仅商业模式下生效)
	SettingKeySMTPEnabled                          SettingKey = "smtp_enabled"                             // Lodestar SMTP 邮件开关
	SettingKeySMTPHost                             SettingKey = "smtp_host"                                // SMTP 服务器
	SettingKeySMTPPort                             SettingKey = "smtp_port"                                // SMTP 端口(587 STARTTLS)
	SettingKeySMTPUser                             SettingKey = "smtp_user"                                // SMTP 用户名
	SettingKeySMTPPass                             SettingKey = "smtp_pass"                                // SMTP 密码/授权码
	SettingKeySMTPFrom                             SettingKey = "smtp_from"                                // 发件人地址
	SettingKeyRegisterEmailRequired                SettingKey = "register_email_required"                  // Lodestar 注册需邮箱验证(仅商业模式下生效)
	SettingKeySiteBannerEnabled                    SettingKey = "site_banner_enabled"
	SettingKeySiteBannerText                       SettingKey = "site_banner_text"
	SettingKeySiteBannerTone                       SettingKey = "site_banner_tone"
	SettingKeyBillingExpr                          SettingKey = "billing_expr"                             // Lodestar 表达式计费(JSON: {"model":"expr",...})
)

type Setting struct {
	Key   SettingKey `json:"key" gorm:"primaryKey"`
	Value string     `json:"value" gorm:"not null"`
}

func DefaultSettings() []Setting {
	return []Setting{
		{Key: SettingKeyProxyURL, Value: ""},
		{Key: SettingKeyStatsSaveInterval, Value: "10"},          // 默认10分钟保存一次统计信息
		{Key: SettingKeyCORSAllowOrigins, Value: ""},             // CORS 默认不允许跨域，设置为 "*" 才允许所有来源
		{Key: SettingKeyModelInfoUpdateInterval, Value: "24"},    // 默认24小时更新一次模型信息
		{Key: SettingKeySyncLLMInterval, Value: "24"},            // 默认24小时同步一次LLM
		{Key: SettingKeyRelayLogKeepPeriod, Value: "7"},          // 默认日志保存7天
		{Key: SettingKeyRelayLogKeepCount, Value: "0"},           // 默认不按条数保留(0=禁用)
		{Key: SettingKeyRelayLogKeepEnabled, Value: "true"},      // 默认保留历史日志
		{Key: SettingKeyRelayRetryCount, Value: "3"},             // 默认单个渠道内 Key 级重试3次
		{Key: SettingKeyRelayRouteRetries, Value: "2"},           // 默认路由级重试2次（全部渠道遍历两轮）
		{Key: SettingKeyCircuitBreakerThreshold, Value: "5"},     // 默认连续失败5次触发熔断
		{Key: SettingKeyCircuitBreakerCooldown, Value: "60"},     // 默认基础冷却60秒
		{Key: SettingKeyCircuitBreakerMaxCooldown, Value: "600"}, // 默认最大冷却600秒（10分钟）
		{Key: SettingKeyRatelimitCooldown, Value: "300"},         // 默认 Key 错误冷却300秒（5分钟），0=关闭
		{Key: SettingKeyRelayMaxTotalAttempts, Value: "0"},       // 默认不限制所有候选渠道的总尝试次数
		{Key: SettingKeyPublicAPIBaseURL, Value: ""},
		{Key: SettingKeyAlertNotifyLanguage, Value: "en"},
		{Key: SettingKeyAutoStrategyMinSamples, Value: "10"},       // 默认最小样本数10次
		{Key: SettingKeyAutoStrategyTimeWindow, Value: "300"},      // 默认时间窗口300秒（5分钟）
		{Key: SettingKeyAutoStrategySampleThreshold, Value: "100"}, // 默认滑动窗口大小100条
		{Key: SettingKeyAutoStrategyLatencyWeight, Value: "30"},    // 默认延迟权重30%
		{Key: SettingKeySemanticCacheEnabled, Value: "false"},      // 默认关闭语义缓存
		{Key: SettingKeySemanticCacheTTL, Value: "3600"},           // 默认TTL 1小时
		{Key: SettingKeySemanticCacheThreshold, Value: "98"},       // 默认相似度阈值 0.98（0-100）
		{Key: SettingKeySemanticCacheMaxEntries, Value: "1000"},    // 默认最大1000条
		{Key: SettingKeySemanticCacheEmbeddingBaseURL, Value: ""},
		{Key: SettingKeySemanticCacheEmbeddingAPIKey, Value: ""},
		{Key: SettingKeySemanticCacheEmbeddingModel, Value: ""},
		{Key: SettingKeySemanticCacheEmbeddingTimeoutSeconds, Value: "10"},
		{Key: SettingKeyNavOrder, Value: `["home","hub","channel","group","model","analytics","log","alert","ops","apikey","setting","user"]`},
		{Key: SettingKeyNavVisible, Value: `["home","hub","channel","group","model","analytics","log","alert","ops","apikey","setting","user"]`},
		{Key: SettingKeyCustomThemes, Value: "[]"}, // Lodestar 自定义主题预设, 默认空数组
		{Key: SettingKeyCommercialMode, Value: "false"}, // Lodestar 默认自用模式(关闭公开注册)
		{Key: SettingKeySiteName, Value: "Lodestar"},
		{Key: SettingKeySiteDescription, Value: "高自定义 · 自用优先 · 可聚合的个人 AI 中转站"},
		{Key: SettingKeySiteAnnouncement, Value: ""},
		{Key: SettingKeySiteFooter, Value: ""},
		{Key: SettingKeyLandingAmbientMode, Value: "photo"},
		{Key: SettingKeySiteBannerEnabled, Value: "false"},
		{Key: SettingKeySiteBannerText, Value: ""},
		{Key: SettingKeySiteBannerTone, Value: "info"},
		{Key: SettingKeyBillingExpr, Value: "{}"},
		{Key: SettingKeyEpayEnabled, Value: "false"},
		{Key: SettingKeyPayAddress, Value: ""},
		{Key: SettingKeyEpayPID, Value: ""},
		{Key: SettingKeyEpayKey, Value: ""},
		{Key: SettingKeyTopupRate, Value: "1"},
		{Key: SettingKeyPaymentCallbackBase, Value: ""},
		{Key: SettingKeyMaintenanceMode, Value: "false"},
		{Key: SettingKeyRegisterInviteRequired, Value: "false"},
		{Key: SettingKeySMTPEnabled, Value: "false"},
		{Key: SettingKeySMTPHost, Value: ""},
		{Key: SettingKeySMTPPort, Value: "587"},
		{Key: SettingKeySMTPUser, Value: ""},
		{Key: SettingKeySMTPPass, Value: ""},
		{Key: SettingKeySMTPFrom, Value: ""},
		{Key: SettingKeyRegisterEmailRequired, Value: "false"},
		{Key: SettingKeyAIRouteGroupID, Value: "0"},
		{Key: SettingKeyAIRouteBaseURL, Value: ""},
		{Key: SettingKeyAIRouteAPIKey, Value: ""},
		{Key: SettingKeyAIRouteModel, Value: ""},
		{Key: SettingKeyAIRouteTimeoutSeconds, Value: "180"},
		{Key: SettingKeyAIRouteParallelism, Value: "3"},
		{Key: SettingKeyAIRouteServices, Value: "[]"},
		{Key: SettingKeyStatsTimezoneOffset, Value: "0"},
		{Key: SettingKeyJWTDefaultExpiryMinutes, Value: "15"},    // 默认15分钟
		{Key: SettingKeyJWTRememberMeExpiryDays, Value: "30"},    // 默认30天
		{Key: SettingKeyLoginRateLimitWindow, Value: "10"},       // 默认10分钟
		{Key: SettingKeyLoginRateLimitMaxFailed, Value: "5"},     // 默认5次
		{Key: SettingKeyStreamSessionTTLMinutes, Value: "30"},    // 默认30分钟
		{Key: SettingKeyStreamSessionMaxEvents, Value: "4096"},   // 默认4096条
		{Key: SettingKeyStreamSessionMaxBytesMB, Value: "4"},     // 默认4MB
		{Key: SettingKeyNotifyHTTPTimeoutSeconds, Value: "10"},   // 默认10秒
		{Key: SettingKeyFailureHintTTLUnauthorized, Value: "10"}, // 默认10秒
		{Key: SettingKeyFailureHintTTLRateLimit, Value: "5"},     // 默认5秒
		{Key: SettingKeyFailureHintTTLNetwork, Value: "2"},       // 默认2秒
		{Key: SettingKeyWebDAVConfig, Value: `{"enabled":false,"base_url":"","username":"","password":"","remote_path":"/octopus-backup/","interval_hours":6,"include_stats":true,"include_logs":false,"max_backups":10}`},
		{Key: SettingKeySiteSyncInterval, Value: "12"},
		{Key: SettingKeySiteCheckinInterval, Value: "24"},
		{Key: SettingKeyStatsSiteModelBackfilled, Value: "false"},
		{Key: SettingKeyProjectedChannelAutoGroupEnabled, Value: "0"}, // 默认不自动分组
		{Key: SettingKeyResponseFilterEnabled, Value: "false"},
		{Key: SettingKeyResponseFilterKeywords, Value: "[]"},
		{Key: SettingKeyResponseFilterAction, Value: "block"},
		{Key: SettingKeyResponseFilterErrorMessage, Value: "The response contains blocked keywords and has been intercepted."},
		{Key: SettingKeyLogLevel, Value: "info"},
		{Key: SettingKeyLogExcludedGroups, Value: "[]"},
		{Key: SettingKeyWebAuthnRPID, Value: ""},
		{Key: SettingKeyWebAuthnRPName, Value: "Octopus"},
		{Key: SettingKeyWebAuthnOrigins, Value: ""},
	}
}

func (s *Setting) Validate() error {
	switch s.Key {
	case SettingKeyModelInfoUpdateInterval, SettingKeySyncLLMInterval, SettingKeyRelayLogKeepPeriod, SettingKeyRelayLogKeepCount,
		SettingKeySiteSyncInterval, SettingKeySiteCheckinInterval,
		SettingKeyRelayRetryCount, SettingKeyRelayRouteRetries, SettingKeyCircuitBreakerThreshold, SettingKeyCircuitBreakerCooldown,
		SettingKeyCircuitBreakerMaxCooldown, SettingKeyRatelimitCooldown, SettingKeyRelayMaxTotalAttempts,
		SettingKeySemanticCacheTTL, SettingKeySemanticCacheThreshold, SettingKeySemanticCacheMaxEntries,
		SettingKeySemanticCacheEmbeddingTimeoutSeconds,
		SettingKeyAutoStrategyMinSamples, SettingKeyAutoStrategyTimeWindow, SettingKeyAutoStrategySampleThreshold,
		SettingKeyAutoStrategyLatencyWeight,
		SettingKeyAIRouteGroupID, SettingKeyAIRouteTimeoutSeconds, SettingKeyAIRouteParallelism,
		SettingKeyStatsTimezoneOffset,
		SettingKeyJWTDefaultExpiryMinutes, SettingKeyJWTRememberMeExpiryDays,
		SettingKeyLoginRateLimitWindow, SettingKeyLoginRateLimitMaxFailed,
		SettingKeyStreamSessionTTLMinutes, SettingKeyStreamSessionMaxEvents, SettingKeyStreamSessionMaxBytesMB,
		SettingKeyNotifyHTTPTimeoutSeconds,
		SettingKeyFailureHintTTLUnauthorized, SettingKeyFailureHintTTLRateLimit, SettingKeyFailureHintTTLNetwork:
		v, err := strconv.Atoi(s.Value)
		if err != nil {
			return fmt.Errorf("setting value must be an integer")
		}
		if s.Key == SettingKeyRelayRetryCount && v < 1 {
			return fmt.Errorf("relay retry count must be greater than 0")
		}
		if s.Key == SettingKeyRelayRouteRetries && v < 1 {
			return fmt.Errorf("relay route retries must be greater than 0")
		}
		if (s.Key == SettingKeyRatelimitCooldown || s.Key == SettingKeyRelayMaxTotalAttempts) && v < 0 {
			return fmt.Errorf("setting value must be greater than or equal to 0")
		}
		if (s.Key == SettingKeyAutoStrategyMinSamples || s.Key == SettingKeyAutoStrategyTimeWindow || s.Key == SettingKeyAutoStrategySampleThreshold) && v < 1 {
			return fmt.Errorf("auto strategy setting must be greater than 0")
		}
		if s.Key == SettingKeyAutoStrategyLatencyWeight && (v < 0 || v > 100) {
			return fmt.Errorf("auto strategy latency weight must be between 0 and 100")
		}
		if s.Key == SettingKeySemanticCacheTTL && v < 1 {
			return fmt.Errorf("semantic cache TTL must be greater than 0")
		}
		if s.Key == SettingKeySemanticCacheThreshold && (v < 0 || v > 100) {
			return fmt.Errorf("semantic cache threshold must be between 0 and 100")
		}
		if s.Key == SettingKeySemanticCacheMaxEntries && v < 1 {
			return fmt.Errorf("semantic cache max entries must be greater than 0")
		}
		if s.Key == SettingKeySemanticCacheEmbeddingTimeoutSeconds && v < 1 {
			return fmt.Errorf("semantic cache embedding timeout must be greater than 0")
		}
		if s.Key == SettingKeyAIRouteGroupID && v < 0 {
			return fmt.Errorf("ai route group id must be greater than or equal to 0")
		}
		if s.Key == SettingKeyAIRouteTimeoutSeconds && v < 1 {
			return fmt.Errorf("ai route timeout must be greater than 0")
		}
		if s.Key == SettingKeyAIRouteParallelism && v < 1 {
			return fmt.Errorf("ai route parallelism must be greater than 0")
		}
		if s.Key == SettingKeyStatsTimezoneOffset && (v < -12 || v > 14) {
			return fmt.Errorf("stats timezone offset must be between -12 and 14")
		}
		switch s.Key {
		case SettingKeyJWTDefaultExpiryMinutes, SettingKeyJWTRememberMeExpiryDays,
			SettingKeyLoginRateLimitWindow, SettingKeyLoginRateLimitMaxFailed,
			SettingKeyStreamSessionTTLMinutes, SettingKeyStreamSessionMaxEvents, SettingKeyStreamSessionMaxBytesMB,
			SettingKeyNotifyHTTPTimeoutSeconds,
			SettingKeyFailureHintTTLUnauthorized, SettingKeyFailureHintTTLRateLimit, SettingKeyFailureHintTTLNetwork:
			if v < 1 {
				return fmt.Errorf("setting value must be greater than 0")
			}
		}
		return nil
	case SettingKeyRelayLogKeepEnabled, SettingKeySemanticCacheEnabled:
		if s.Value != "true" && s.Value != "false" {
			return fmt.Errorf("setting value must be true or false")
		}
		return nil
	case SettingKeyProxyURL, SettingKeySemanticCacheEmbeddingBaseURL, SettingKeyAIRouteBaseURL:
		if s.Value == "" {
			return nil
		}
		parsedURL, err := url.Parse(s.Value)
		if err != nil {
			if s.Key == SettingKeySemanticCacheEmbeddingBaseURL {
				return fmt.Errorf("semantic cache embedding base URL is invalid: %w", err)
			}
			if s.Key == SettingKeyAIRouteBaseURL {
				return fmt.Errorf("ai route base URL is invalid: %w", err)
			}
			return fmt.Errorf("proxy URL is invalid: %w", err)
		}
		if s.Key == SettingKeySemanticCacheEmbeddingBaseURL {
			if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
				return fmt.Errorf("semantic cache embedding base URL scheme must be http or https")
			}
			if parsedURL.Host == "" {
				return fmt.Errorf("semantic cache embedding base URL must have a host")
			}
			return nil
		}
		if s.Key == SettingKeyAIRouteBaseURL {
			if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
				return fmt.Errorf("ai route base URL scheme must be http or https")
			}
			if parsedURL.Host == "" {
				return fmt.Errorf("ai route base URL must have a host")
			}
			return nil
		}

		validSchemes := map[string]bool{
			"http":   true,
			"https":  true,
			"socks5": true,
		}
		if !validSchemes[parsedURL.Scheme] {
			return fmt.Errorf("proxy URL scheme must be http, https, socks, or socks5")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("proxy URL must have a host")
		}
		return nil
	case SettingKeyPublicAPIBaseURL:
		if s.Value == "" {
			return nil
		}
		parsedURL, err := url.Parse(s.Value)
		if err != nil {
			return fmt.Errorf("public API base URL is invalid: %w", err)
		}
		if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
			return fmt.Errorf("public API base URL scheme must be http or https")
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("public API base URL must have a host")
		}
		return nil
	case SettingKeyAlertNotifyLanguage:
		switch s.Value {
		case "zh-Hans", "zh-Hant", "en":
			return nil
		default:
			return fmt.Errorf("alert notify language must be zh-Hans, zh-Hant, or en")
		}
	case SettingKeyNavOrder, SettingKeyNavVisible:
		var navOrder []string
		if err := json.Unmarshal([]byte(s.Value), &navOrder); err != nil {
			return fmt.Errorf("nav setting must be a valid JSON array of strings")
		}
		return nil
	case SettingKeyAIRouteServices:
		return ValidateAIRouteServiceConfigs(s.Value)
	case SettingKeyWebDAVConfig:
		var cfg map[string]any
		if err := json.Unmarshal([]byte(s.Value), &cfg); err != nil {
			return fmt.Errorf("webdav config must be a valid JSON object")
		}
		return nil
	case SettingKeyResponseFilterEnabled:
		if s.Value != "true" && s.Value != "false" {
			return fmt.Errorf("setting value must be true or false")
		}
		return nil
	case SettingKeyResponseFilterKeywords:
		var keywords []string
		if err := json.Unmarshal([]byte(s.Value), &keywords); err != nil {
			return fmt.Errorf("response filter keywords must be a valid JSON array of strings")
		}
		return nil
	case SettingKeyLogExcludedGroups:
		var groups []string
		if err := json.Unmarshal([]byte(s.Value), &groups); err != nil {
			return fmt.Errorf("log excluded groups must be a valid JSON array of strings")
		}
		return nil
	case SettingKeyResponseFilterAction:
		switch s.Value {
		case "block", "replace":
			return nil
		default:
			return fmt.Errorf("response filter action must be block or replace")
		}
	case SettingKeyResponseFilterErrorMessage:
		return nil
	case SettingKeyLogLevel:
		switch s.Value {
		case "debug", "info", "warn", "error":
			return nil
		default:
			return fmt.Errorf("log level must be one of: debug, info, warn, error")
		}
	case SettingKeyProjectedChannelAutoGroupEnabled:
		_, ok := ParseAutoGroupSettingValue(s.Value)
		if !ok {
			return fmt.Errorf("projected channel auto group mode must be 0 (none), 1 (fuzzy), 2 (exact), or 3 (regex)")
		}
		return nil
	}

	return nil
}
