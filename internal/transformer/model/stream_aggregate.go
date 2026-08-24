package model

// AggregateStreamChunks folds accumulated streaming chunks into one complete
// response, the way non-streaming responses arrive.
//
// It returns nil for an empty slice, so callers can hand the result straight back.
//
// This lived as three copies — one per inbound adapter (openai chat, openai
// responses, anthropic messages) — and they had drifted. The openai-chat copy
// accumulated three things the other two dropped on the floor:
//
//   - delta.Content.MultipleContent (multipart output: images, audio)
//   - delta.Images (Gemini image generation via the OpenAI-compatible endpoint)
//   - reasoning read through GetReasoningContent(), which also covers the
//     `reasoning` spelling used by OpenRouter and Ollama cloud; the other two
//     tested ReasoningContent != nil and so saw only `reasoning_content`
//
// The aggregate feeds relay logging (metrics.SetInternalResponse) and the
// semantic cache, not the client — clients get the streamed SSE directly. So the
// drift cost log fidelity for Anthropic-format and Responses-format streaming
// clients, and would poison the semantic cache wherever that is enabled. Billing
// was never affected: SetInternalResponse reads only resp.Usage.
func AggregateStreamChunks(chunks []*InternalLLMResponse) *InternalLLMResponse {
	if len(chunks) == 0 {
		return nil
	}

	// Use the first chunk as the base.
	firstChunk := chunks[0]
	result := &InternalLLMResponse{
		ID:                firstChunk.ID,
		Object:            "chat.completion",
		Created:           firstChunk.Created,
		Model:             firstChunk.Model,
		SystemFingerprint: firstChunk.SystemFingerprint,
		ServiceTier:       firstChunk.ServiceTier,
	}

	choicesMap := make(map[int]*Choice)

	for _, chunk := range chunks {
		// Some providers only send ID and Model in later chunks.
		if chunk.ID != "" {
			result.ID = chunk.ID
		}
		if chunk.Model != "" {
			result.Model = chunk.Model
		}

		// Keep usage from the last chunk that carries it.
		if chunk.Usage != nil {
			result.Usage = chunk.Usage
		}

		for _, choice := range chunk.Choices {
			existingChoice, exists := choicesMap[choice.Index]
			if !exists {
				existingChoice = &Choice{
					Index:   choice.Index,
					Message: &Message{},
				}
				choicesMap[choice.Index] = existingChoice
			}

			if choice.Delta != nil {
				delta := choice.Delta

				if delta.Role != "" {
					existingChoice.Message.Role = delta.Role
				}

				// Text content.
				if delta.Content.Content != nil {
					if existingChoice.Message.Content.Content == nil {
						existingChoice.Message.Content.Content = new(string)
					}
					*existingChoice.Message.Content.Content += *delta.Content.Content
				}

				// Multipart output (images, audio, …).
				if len(delta.Content.MultipleContent) > 0 {
					existingChoice.Message.Content.MultipleContent = append(
						existingChoice.Message.Content.MultipleContent,
						delta.Content.MultipleContent...,
					)
				}

				// Gemini image generation via the OpenAI-compatible endpoint.
				if len(delta.Images) > 0 {
					existingChoice.Message.Content.MultipleContent = append(
						existingChoice.Message.Content.MultipleContent,
						delta.Images...,
					)
				}

				// Reasoning, via the accessor so both the `reasoning_content` and
				// `reasoning` spellings are picked up.
				if delta.GetReasoningContent() != "" {
					if existingChoice.Message.ReasoningContent == nil {
						existingChoice.Message.ReasoningContent = new(string)
					}
					*existingChoice.Message.ReasoningContent += delta.GetReasoningContent()
				}

				for _, toolCall := range delta.ToolCalls {
					existingChoice.Message.ToolCalls = mergeToolCall(existingChoice.Message.ToolCalls, toolCall)
				}

				if delta.Refusal != "" {
					existingChoice.Message.Refusal = delta.Refusal
				}
			}

			if choice.FinishReason != nil {
				existingChoice.FinishReason = choice.FinishReason
			}

			if choice.Logprobs != nil {
				if existingChoice.Logprobs == nil {
					existingChoice.Logprobs = &LogprobsContent{}
				}
				existingChoice.Logprobs.Content = append(existingChoice.Logprobs.Content, choice.Logprobs.Content...)
			}
		}
	}

	result.Choices = SortedChoicesByIndex(choicesMap)
	return result
}

// mergeToolCall folds a tool-call delta into the accumulated slice, matching on
// Index. Name and Arguments accumulate; ID and Type are overwritten when present.
//
// This was also duplicated byte-for-byte between the openai and anthropic inbound
// packages, and was only ever called from the aggregator above.
func mergeToolCall(toolCalls []ToolCall, delta ToolCall) []ToolCall {
	for i, tc := range toolCalls {
		if tc.Index == delta.Index {
			if delta.ID != "" {
				toolCalls[i].ID = delta.ID
			}
			if delta.Type != "" {
				toolCalls[i].Type = delta.Type
			}
			if delta.Function.Name != "" {
				toolCalls[i].Function.Name += delta.Function.Name
			}
			if delta.Function.Arguments != "" {
				toolCalls[i].Function.Arguments += delta.Function.Arguments
			}
			return toolCalls
		}
	}

	return append(toolCalls, delta)
}
