package airoute

import (
	"fmt"
	"strings"

	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/transformer/outbound"
)

// ---------- Prompt building ----------

func buildAIRouteSystemPrompt(promptEndpointType string) string {
	endpointLabel := airoutePromptEndpointLabel(promptEndpointType)
	return fmt.Sprintf(`你是一个模型路由分析器。你的任务是根据给定的模型列表，识别哪些模型本质上是同一类模型，并为它们生成统一的路由映射。
本次输入模型全部属于 %s 能力类型。
要求：
1. 只输出 JSON，不要输出任何解释、Markdown、代码块标记。
2. 只分析当前这类能力模型，不要混入其他能力类型。
3. 将语义相同或同系列的模型归一到一个 requested_model。
4. requested_model 应尽量使用简洁、稳定、常见的名称。
5. items 中每个元素表示一个可用上游：
   - channel_id: 整数，必须来自输入列表
   - upstream_model: 原始模型名，必须来自输入列表中相同 channel_id 下的 model
   - priority: 数字，越小优先级越高
   - weight: 数字，默认 100
6. 如果一个模型名无法判断，不要强行归类。
7. 输出格式必须严格符合：
{
  "routes": [
    {
      "requested_model": "string",
      "items": [
        {
          "channel_id": 1,
          "upstream_model": "string",
          "priority": 1,
          "weight": 100
        }
      ]
    }
  ]
}`, endpointLabel)
}

func buildAIRouteUserPrompt(promptEndpointType string, targetGroupName string, payload []byte) string {
	endpointLabel := airoutePromptEndpointLabel(promptEndpointType)
	if strings.TrimSpace(targetGroupName) != "" {
		return fmt.Sprintf(
			"请分析以下 %s 模型列表，并生成路由表。\n本次目标分组名称为 %q，请优先输出 requested_model 为 %q 的路由；如果无法确定，可返回空 routes。\n模型列表：\n%s",
			endpointLabel,
			targetGroupName,
			targetGroupName,
			string(payload),
		)
	}
	return fmt.Sprintf("请分析以下 %s 模型列表，并生成完整路由表。\n模型列表：\n%s", endpointLabel, string(payload))
}

// ---------- Prompt bucket construction ----------

func buildAIRoutePromptBuckets(modelInputs []model.AIRouteModelInput, targetPromptEndpointType string) []aiRoutePromptBucket {
	targetGroupEndpointType := model.NormalizeEndpointType(targetPromptEndpointType)
	if strings.TrimSpace(targetPromptEndpointType) != "" {
		targetPromptEndpointType = normalizeAIRoutePromptEndpointType(targetPromptEndpointType)
	}

	type bucketState struct {
		bucket aiRoutePromptBucket
		seen   map[string]struct{}
	}

	states := make(map[string]*bucketState)
	for _, endpointType := range orderedAIRoutePromptEndpointTypes() {
		states[endpointType] = &bucketState{
			bucket: aiRoutePromptBucket{
				PromptEndpointType: endpointType,
				GroupEndpointType:  groupEndpointTypeForAIRouteBucket(endpointType),
				ModelInputs:        make([]aiRoutePromptModelInput, 0),
			},
			seen: make(map[string]struct{}),
		}
	}
	if targetGroupEndpointType == model.EndpointTypeDeepSeek || targetGroupEndpointType == model.EndpointTypeMimo {
		states[model.EndpointTypeChat].bucket.GroupEndpointType = targetGroupEndpointType
	}

	for _, input := range modelInputs {
		modelName := strings.TrimSpace(input.Model)
		if input.ChannelID <= 0 || modelName == "" {
			continue
		}

		promptEndpointType := inferAIRoutePromptEndpointType(modelName)
		if targetPromptEndpointType != "" && promptEndpointType != targetPromptEndpointType {
			continue
		}

		state, ok := states[promptEndpointType]
		if !ok {
			continue
		}

		key := fmt.Sprintf("%d\x00%s", input.ChannelID, strings.ToLower(modelName))
		if _, exists := state.seen[key]; exists {
			continue
		}
		state.seen[key] = struct{}{}

		state.bucket.ModelInputs = append(state.bucket.ModelInputs, aiRoutePromptModelInput{
			ChannelID: input.ChannelID,
			Model:     modelName,
		})
	}

	result := make([]aiRoutePromptBucket, 0)
	for _, endpointType := range orderedAIRoutePromptEndpointTypes() {
		state := states[endpointType]
		if state == nil || len(state.bucket.ModelInputs) == 0 {
			continue
		}
		result = append(result, splitAIRoutePromptBucket(state.bucket)...)
	}

	return result
}

func splitAIRoutePromptBucket(bucket aiRoutePromptBucket) []aiRoutePromptBucket {
	if len(bucket.ModelInputs) <= aiRouteMaxModelsPerRequest {
		return []aiRoutePromptBucket{bucket}
	}

	familyOrder := make([]string, 0)
	familyInputs := make(map[string][]aiRoutePromptModelInput)
	for _, input := range bucket.ModelInputs {
		identity := group.NormalizeModelIdentity(input.Model)
		key := strings.ToLower(strings.TrimSpace(identity.Canonical))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(input.Model))
		}
		if _, ok := familyInputs[key]; !ok {
			familyOrder = append(familyOrder, key)
		}
		familyInputs[key] = append(familyInputs[key], input)
	}

	result := make([]aiRoutePromptBucket, 0)
	currentInputs := make([]aiRoutePromptModelInput, 0, aiRouteMaxModelsPerRequest)

	flush := func() {
		if len(currentInputs) == 0 {
			return
		}
		next := bucket
		next.ModelInputs = append([]aiRoutePromptModelInput(nil), currentInputs...)
		result = append(result, next)
		currentInputs = make([]aiRoutePromptModelInput, 0, aiRouteMaxModelsPerRequest)
	}

	for _, key := range familyOrder {
		inputs := familyInputs[key]
		if len(inputs) >= aiRouteMaxModelsPerRequest {
			flush()
			for start := 0; start < len(inputs); start += aiRouteMaxModelsPerRequest {
				end := start + aiRouteMaxModelsPerRequest
				if end > len(inputs) {
					end = len(inputs)
				}
				next := bucket
				next.ModelInputs = append([]aiRoutePromptModelInput(nil), inputs[start:end]...)
				result = append(result, next)
			}
			continue
		}

		if len(currentInputs)+len(inputs) > aiRouteMaxModelsPerRequest {
			flush()
		}
		currentInputs = append(currentInputs, inputs...)
	}

	flush()
	return result
}

// ---------- Endpoint type detection & normalization ----------

func inferAIRoutePromptEndpointType(modelName string) string {
	identity := group.NormalizeModelIdentity(modelName)
	return normalizeAIRoutePromptEndpointType(identity.EndpointType)
}

func normalizeAIRoutePromptEndpointType(endpointType string) string {
	switch model.NormalizeEndpointType(endpointType) {
	case "", model.EndpointTypeAll, model.EndpointTypeChat, model.EndpointTypeDeepSeek, model.EndpointTypeMimo, model.EndpointTypeResponses, model.EndpointTypeMessages:
		return model.EndpointTypeChat
	default:
		return model.NormalizeEndpointType(endpointType)
	}
}

func groupEndpointTypeForAIRouteBucket(promptEndpointType string) string {
	promptEndpointType = normalizeAIRoutePromptEndpointType(promptEndpointType)
	if promptEndpointType == model.EndpointTypeChat {
		return model.EndpointTypeAll
	}
	return promptEndpointType
}

func normalizeAIRouteGroupEndpointType(endpointType string) string {
	endpointType = model.NormalizeEndpointType(endpointType)
	if endpointType == "" {
		return model.EndpointTypeAll
	}
	return endpointType
}

func orderedAIRoutePromptEndpointTypes() []string {
	return []string{
		model.EndpointTypeChat,
		model.EndpointTypeEmbeddings,
		model.EndpointTypeRerank,
		model.EndpointTypeModerations,
		model.EndpointTypeImageGeneration,
		model.EndpointTypeAudioSpeech,
		model.EndpointTypeAudioTranscription,
		model.EndpointTypeVideoGeneration,
		model.EndpointTypeMusicGeneration,
		model.EndpointTypeSearch,
	}
}

func airoutePromptEndpointLabel(endpointType string) string {
	switch normalizeAIRoutePromptEndpointType(endpointType) {
	case model.EndpointTypeChat:
		return "文本对话"
	case model.EndpointTypeEmbeddings:
		return "向量嵌入"
	case model.EndpointTypeRerank:
		return "重排序"
	case model.EndpointTypeModerations:
		return "内容审核"
	case model.EndpointTypeImageGeneration:
		return "图像生成"
	case model.EndpointTypeAudioSpeech:
		return "语音合成"
	case model.EndpointTypeAudioTranscription:
		return "音频转写"
	case model.EndpointTypeVideoGeneration:
		return "视频生成"
	case model.EndpointTypeMusicGeneration:
		return "音乐生成"
	case model.EndpointTypeSearch:
		return "搜索"
	default:
		return normalizeAIRoutePromptEndpointType(endpointType)
	}
}

func detectAIRoutePromptEndpointTypeForGroup(group model.Group) string {
	current := model.NormalizeEndpointType(group.EndpointType)
	switch current {
	case model.EndpointTypeDeepSeek,
		model.EndpointTypeMimo,
		model.EndpointTypeEmbeddings,
		model.EndpointTypeRerank,
		model.EndpointTypeModerations,
		model.EndpointTypeImageGeneration,
		model.EndpointTypeAudioSpeech,
		model.EndpointTypeAudioTranscription,
		model.EndpointTypeVideoGeneration,
		model.EndpointTypeMusicGeneration,
		model.EndpointTypeSearch:
		return current
	}

	detected := ""
	for _, item := range group.Items {
		endpointType := inferAIRoutePromptEndpointType(item.ModelName)
		if endpointType == model.EndpointTypeChat {
			continue
		}
		if detected == "" {
			detected = endpointType
			continue
		}
		if detected != endpointType {
			return model.EndpointTypeChat
		}
	}
	if detected != "" {
		return detected
	}
	return model.EndpointTypeChat
}

func aiRouteProviderName(provider outbound.OutboundType) string {
	switch provider {
	case outbound.OutboundTypeOpenAIChat:
		return "openai_chat"
	case outbound.OutboundTypeOpenAIResponse:
		return "openai_response"
	case outbound.OutboundTypeAnthropic:
		return "anthropic"
	case outbound.OutboundTypeGemini:
		return "gemini"
	case outbound.OutboundTypeVolcengine:
		return "volcengine"
	case outbound.OutboundTypeOpenAIEmbedding:
		return "openai_embedding"
	case outbound.OutboundTypeMimo:
		return "mimo"
	default:
		return fmt.Sprintf("provider_%d", provider)
	}
}
