package price

// 手工维护的价格预设，与 scripts/updatePrice.py 生成的 presets.go 分离：
// 生成脚本只重写 presets.go，本文件永远不会被脚本触碰，重跑脚本不会丢手工价。
// 数值口径与 presets.go 全部条目一致：USD / 1M tokens。
//
// 添加手工价：在 manualLLMPrices 中加条目即可，包加载时 init() 会合并进
// llmPrice。注意 UpdateLLMPrice（models.dev 后台刷新）收录官方数据后会覆盖
// 同名键，属预期。
//
// 当前为空：presets.go 现存的全部键均可由生成脚本复现（models.dev 数据 +
// Claude 别名规则，MODEL_ALIASES 为空），没有需要迁出的手工条目。
//
// 示例（需要时取消注释并按官方价目表填写）：
//
//	"deepseek-v4-flash": {Input: 0.14, Output: 0.28, CacheRead: 0.0028, CacheWrite: 0},

import "github.com/gypg/lodestar/internal/model"

// manualLLMPrices 手工维护的价格条目，见文件头注释。
var manualLLMPrices = map[string]model.LLMPrice{}

// applyManualPrices 将手工价合并进生成脚本产出的价格表（同名键覆盖）。
// 与 init() 分离成独立函数，便于测试单独验证合并语义。
func applyManualPrices(generated map[string]model.LLMPrice) map[string]model.LLMPrice {
	for k, v := range manualLLMPrices {
		generated[k] = v
	}
	return generated
}

func init() {
	applyManualPrices(llmPrice)
}
