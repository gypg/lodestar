package model

// ModelCapability describes which endpoints a model supports,
// whether it belongs to the conversation family, and whether
// it is currently available.
//
// Available 是 WO-028 的实测标记：定时探测连续失败达阈值 → false；
// 探测关闭或该模型从未探测 → true（回退到探测引入前的行为）。
// 它从不反映渠道配置（那是路由层的事），也从不影响路由，仅供呈现。
type ModelCapability struct {
	Name         string   `json:"name"`
	Endpoints    []string `json:"endpoints"`
	Conversation bool     `json:"conversation"`
	Available    bool     `json:"available"`
}
