package model

/*
Lodestar commercial layer — top-up codes (redeemable credit).

Ported in concept from new-api's redemption codes, adapted to Lodestar's
float-USD balance: an admin generates codes worth N USD; a user redeems a code
to credit their balance. This is the no-payment-provider monetization path
(sell/distribute codes); Epay/Stripe integration is a later sub-phase.
*/

type TopupCode struct {
	ID        int     `json:"id" gorm:"primaryKey;autoIncrement"`
	Code      string  `json:"code" gorm:"type:varchar(64);uniqueIndex;not null"`
	Quota     float64 `json:"quota" gorm:"type:real;not null"` // USD credited on redeem
	Used      bool    `json:"used" gorm:"default:false"`
	UsedBy    uint    `json:"used_by" gorm:"default:0"`
	CreatedAt int64   `json:"created_at" gorm:"bigint"`
	UsedAt    int64   `json:"used_at" gorm:"bigint;default:0"`
}

func (TopupCode) TableName() string { return "topup_codes" }
