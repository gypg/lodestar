package model

/*
Lodestar commercial layer — invitation codes (registration gating).

When commercial_mode is on, an admin may additionally require a valid invite code
to register (register_invite_required setting). Same one-time, race-safe pattern
as top-up codes. Lets the operator control exactly who can sign up.
*/

type InviteCode struct {
	ID        int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Code      string `json:"code" gorm:"type:varchar(64);uniqueIndex;not null"`
	Used      bool   `json:"used" gorm:"default:false"`
	UsedBy    uint   `json:"used_by" gorm:"default:0"`
	CreatedAt int64  `json:"created_at" gorm:"bigint"`
	UsedAt    int64  `json:"used_at" gorm:"bigint;default:0"`
}

func (InviteCode) TableName() string { return "invite_codes" }
