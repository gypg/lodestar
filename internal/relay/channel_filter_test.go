package relay

import (
	"reflect"
	"testing"
)

func TestParseExcludedChannels(t *testing.T) {
	cases := []struct {
		in   string
		want map[int]struct{}
	}{
		{"", nil},
		{"   ", nil},
		{"2", set(2)},
		{"1,2,3", set(1, 2, 3)},
		{" 1 , 2 ", set(1, 2)},
		{"1,abc,3", set(1, 3)}, // 非法条目忽略
		{"0,-1", nil},          // 非正 ID 忽略
		{"abc", nil},           // 全非法 → nil
	}
	for _, tc := range cases {
		got := parseExcludedChannels(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseExcludedChannels(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func set(ids ...int) map[int]struct{} {
	if len(ids) == 0 {
		return nil
	}
	m := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}
