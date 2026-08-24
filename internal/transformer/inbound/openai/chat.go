package openai

import (
	"context"
	"encoding/json"

	"github.com/gypg/lodestar/internal/transformer/model"
)

type ChatInbound struct {
	// streamChunks stores stream chunks for aggregation
	streamChunks []*model.InternalLLMResponse
	// storedResponse stores the non-stream response
	storedResponse *model.InternalLLMResponse
}

func (i *ChatInbound) TransformRequest(ctx context.Context, body []byte) (*model.InternalLLMRequest, error) {
	var request model.InternalLLMRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	request.RawAPIFormat = model.APIFormatOpenAIChatCompletion
	return &request, nil
}

func (i *ChatInbound) TransformResponse(ctx context.Context, response *model.InternalLLMResponse) ([]byte, error) {
	// Store the response for later retrieval
	i.storedResponse = response

	body, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (i *ChatInbound) TransformStream(ctx context.Context, stream *model.InternalLLMResponse) ([]byte, error) {
	if stream.Object == "[DONE]" {
		return []byte("data: [DONE]\n\n"), nil
	}

	// Store the chunk for aggregation
	i.streamChunks = append(i.streamChunks, stream)

	var body []byte
	var err error

	// OpenAI Chat Completion stream chunks must always carry a "choices" array,
	// and every choice must carry a "delta" field — even an empty object on the
	// final finish_reason chunk. Strict clients (e.g. RikkaHub) treat a missing
	// or null choices[0].delta as a protocol violation and throw. Since
	// model.Choice.Delta is omitempty, stream chunks are serialized through a
	// dedicated type that always emits both fields per the OpenAI SSE spec.
	if stream.Object == "chat.completion.chunk" {
		body, err = marshalChatChunk(stream)
	} else {
		body, err = json.Marshal(stream)
	}

	if err != nil {
		return nil, err
	}
	return []byte("data: " + string(body) + "\n\n"), nil
}

// streamChoice renders a chat.completion.chunk choice. The Delta field shadows
// model.Choice.Delta (which is omitempty) so that "delta" is always emitted,
// matching the OpenAI streaming spec — including an empty {} on the terminal
// finish_reason chunk.
type streamChoice struct {
	model.Choice
	Delta *model.Message `json:"delta"`
}

// marshalChatChunk serializes an OpenAI chat completion stream chunk, ensuring
// "choices" is always an array (possibly empty) and each choice always carries
// a non-null "delta" field. It does not mutate the input chunk, so the stored
// internal representation used for aggregation stays untouched.
func marshalChatChunk(stream *model.InternalLLMResponse) ([]byte, error) {
	choices := make([]streamChoice, 0, len(stream.Choices))
	for _, choice := range stream.Choices {
		delta := choice.Delta
		if delta == nil {
			delta = &model.Message{}
		}
		choices = append(choices, streamChoice{Choice: choice, Delta: delta})
	}

	type alias model.InternalLLMResponse
	aux := &struct {
		*alias
		Choices []streamChoice `json:"choices"`
	}{
		alias:   (*alias)(stream),
		Choices: choices,
	}
	return json.Marshal(aux)
}

// GetInternalResponse returns the complete internal response for logging, statistics, etc.
// For streaming: aggregates all stored stream chunks into a complete response
// For non-streaming: returns the stored response
func (i *ChatInbound) GetInternalResponse(ctx context.Context) (*model.InternalLLMResponse, error) {
	// Non-stream: hand back what we stored.
	if i.storedResponse != nil {
		return i.storedResponse, nil
	}

	// Stream: fold the accumulated chunks. Shared with the other inbound
	// adapters on purpose -- this logic used to exist as three copies and they
	// had drifted (see model.AggregateStreamChunks).
	result := model.AggregateStreamChunks(i.streamChunks)
	i.streamChunks = nil

	return result, nil
}
