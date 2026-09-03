package model

import "time"

// ModelProbeState 记录每个分组（= 模型广场里的一个模型）跨轮次的探测结果。
//
// 分组名是主键：本系统里"模型广场的一个模型"就是一个分组（GroupListModelCapabilities
// 按分组名聚合），探测按分组跑，隐藏/通知也按分组判定。
//
// 为什么不直接查 group_test_results 历史：连续失败计数是状态不是日志，重启后必须
// 保留（否则重启就永远凑不满阈值），跨轮递增/重置也要单行原子更新，独立一行最省心。
// 计数在探测成功时归零（对称性），通知去重靠 LastNotifiedFails（同一失败episode
// 只通知一次，发送失败下一轮自动重试）。
type ModelProbeState struct {
	GroupName           string    `json:"group_name" gorm:"primaryKey;size:255"`
	ConsecutiveFailures int       `json:"consecutive_failures" gorm:"not null;default:0"`
	LastProbedAt        time.Time `json:"last_probed_at"`
	LastNotifiedFails   int       `json:"last_notified_fails" gorm:"not null;default:0"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (ModelProbeState) TableName() string { return "model_probe_states" }
