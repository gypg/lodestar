package model

/*
Lodestar — 客户预警防重状态（WO-026 阶段 C）。

每用户一行：低余额与订阅到期两个维度各自记"上次发送时的档位"。存档位而非布尔
的原因：余额继续跌到更低档位要能再发（更紧急），回升清档位后跌回同档要能再发
（新 episode），同一档位内不动不重发——布尔只能表达前两条的一半。
*/

// UserAlertState 每用户一行的预警防重状态。
type UserAlertState struct {
	UserID uint `gorm:"primaryKey"`

	// 低余额维度：上次发送时的余额档位（threshold 起按半阈值递减）；0 = 从未发过。
	LastBalanceTier   float64 `gorm:"default:0"`
	LastBalanceSentAt int64   `gorm:"default:0;bigint"`

	// 到期维度：LastExpirySentAt 非 0 = 已发送过（WO-030 缺陷 A：daysLeft 在最后
	// 24 小时为 0，不能当"从未发送"哨兵用）；LastExpiryDaysSent 记发送时点的剩余
	// 天数。旧版本行可能只有 days 非零、sentAt 为零——同样视为已发（见
	// customeralert.expiryAlertNeeded 的兼容逻辑），双零才是真正的未发送。
	LastExpiryDaysSent int64 `gorm:"default:0"`
	LastExpirySentAt   int64 `gorm:"default:0;bigint"`

	UpdatedAt int64 `gorm:"bigint"`
}

func (UserAlertState) TableName() string { return "user_alert_states" }
