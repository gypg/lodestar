package model

/*
GGZERO commercial layer — online payment orders (top-up via 易支付/Epay).

Ported from new-api's TopUp order model, adapted to octopus float-USD balance:
a user pays `Money` (gateway currency) to credit `AmountUSD` to their balance.
*/

type PaymentOrder struct {
	ID           int     `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID       uint    `json:"user_id" gorm:"index;not null"`
	AmountUSD    float64 `json:"amount_usd" gorm:"type:real;not null"` // credit added to balance on success
	Money        float64 `json:"money" gorm:"type:real"`               // amount actually paid (gateway currency)
	TradeNo      string  `json:"trade_no" gorm:"type:varchar(128);uniqueIndex;not null"`
	Method       string  `json:"method" gorm:"type:varchar(32)"` // alipay / wxpay
	Provider     string  `json:"provider" gorm:"type:varchar(32);default:'epay'"`
	Status       string  `json:"status" gorm:"type:varchar(16);default:'pending'"` // pending / success
	CreateTime   int64   `json:"create_time" gorm:"bigint"`
	CompleteTime int64   `json:"complete_time" gorm:"bigint;default:0"`
}

func (PaymentOrder) TableName() string { return "payment_orders" }
