package relay

import (
	"strconv"
	"strings"
)

// parseExcludedChannels 将逗号分隔的渠道 ID 字符串解析为集合。
// 空串或无有效 ID 时返回 nil（表示不排除任何渠道）。非法或非正的条目被忽略。
func parseExcludedChannels(s string) map[int]struct{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	set := make(map[int]struct{})
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			continue
		}
		set[id] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}
