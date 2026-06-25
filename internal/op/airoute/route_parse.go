package airoute

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gypg/lodestar/internal/model"
)

// ---------- AI response parsing ----------

func normalizeAIMessageContent(content any) (string, error) {
	switch value := content.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("AI返回结果为空")
		}
		return value, nil
	case []any:
		var builder strings.Builder
		for _, item := range value {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := record["text"].(string); ok {
				builder.WriteString(text)
			}
		}
		result := strings.TrimSpace(builder.String())
		if result == "" {
			return "", fmt.Errorf("AI返回结果为空")
		}
		return result, nil
	default:
		return "", fmt.Errorf("AI返回结果为空")
	}
}

func parseAIRouteResponseContent(content string) (model.AIRouteResponse, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return model.AIRouteResponse{}, fmt.Errorf("AI返回结果为空")
	}

	candidates := extractAIRouteJSONCandidates(content)
	for _, candidate := range candidates {
		if resp, ok := decodeAIRouteResponseCandidate(candidate); ok {
			return resp, nil
		}
	}

	return model.AIRouteResponse{}, fmt.Errorf("AI返回结果不是合法JSON")
}

func decodeAIRouteResponseCandidate(candidate string) (model.AIRouteResponse, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return model.AIRouteResponse{}, false
	}

	var routes []model.AIRouteEntry
	if json.Unmarshal([]byte(candidate), &routes) == nil {
		return model.AIRouteResponse{Routes: routes}, true
	}

	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(candidate), &envelope) != nil {
		return model.AIRouteResponse{}, false
	}

	if rawRoutes, ok := envelope["routes"]; ok {
		if routes, ok := decodeAIRouteRoutesRaw(rawRoutes); ok {
			return model.AIRouteResponse{Routes: routes}, true
		}
	}

	for _, key := range []string{"result", "data", "output"} {
		if rawNested, ok := envelope[key]; ok {
			if resp, ok := decodeAIRouteNestedRaw(rawNested); ok {
				return resp, true
			}
		}
	}

	var singleRoute model.AIRouteEntry
	if json.Unmarshal([]byte(candidate), &singleRoute) == nil && strings.TrimSpace(singleRoute.RequestedModel) != "" {
		return model.AIRouteResponse{Routes: []model.AIRouteEntry{singleRoute}}, true
	}

	return model.AIRouteResponse{}, false
}

func decodeAIRouteNestedRaw(raw json.RawMessage) (model.AIRouteResponse, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return model.AIRouteResponse{}, false
	}

	if strings.HasPrefix(trimmed, "\"") {
		var nested string
		if json.Unmarshal(raw, &nested) == nil {
			return decodeAIRouteResponseCandidate(nested)
		}
	}

	return decodeAIRouteResponseCandidate(trimmed)
}

func decodeAIRouteRoutesRaw(raw json.RawMessage) ([]model.AIRouteEntry, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, true
	}

	var routes []model.AIRouteEntry
	if json.Unmarshal(raw, &routes) == nil {
		return routes, true
	}

	if strings.HasPrefix(trimmed, "\"") {
		var nested string
		if json.Unmarshal(raw, &nested) == nil {
			return decodeAIRouteRoutesString(nested)
		}
	}

	return nil, false
}

func decodeAIRouteRoutesString(content string) ([]model.AIRouteEntry, bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, true
	}

	var routes []model.AIRouteEntry
	if json.Unmarshal([]byte(content), &routes) == nil {
		return routes, true
	}

	return nil, false
}

func extractAIRouteJSONCandidates(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	candidates := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)

	addCandidate := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	addCandidate(content)

	for i := 0; i < len(content); i++ {
		if content[i] != '{' && content[i] != '[' {
			continue
		}

		if candidate, ok := findBalancedJSONValue(content, i); ok {
			addCandidate(candidate)
		}
	}

	return candidates
}

func findBalancedJSONValue(content string, start int) (string, bool) {
	if start < 0 || start >= len(content) {
		return "", false
	}
	if content[start] != '{' && content[start] != '[' {
		return "", false
	}

	stack := make([]byte, 0, 4)
	inString := false
	escaped := false

	for i := start; i < len(content); i++ {
		ch := content[i]

		if inString {
			if escaped {
				escaped = false
				continue
			}
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}':
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return "", false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return content[start : i+1], true
			}
		case ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return "", false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return content[start : i+1], true
			}
		}
	}

	return "", false
}
