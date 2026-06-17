package balancer

import (
	"testing"

	"github.com/lingyuins/octopus/internal/model"
)

func TestNewIteratorExcludesChannels(t *testing.T) {
	group := model.Group{
		Items: []model.GroupItem{
			{ChannelID: 1, ModelName: "m"},
			{ChannelID: 2, ModelName: "m"},
			{ChannelID: 3, ModelName: "m"},
		},
	}

	t.Run("partial exclusion", func(t *testing.T) {
		excluded := map[int]struct{}{2: {}}
		iter := NewIterator(group, 0, "m", excluded)

		got := make(map[int]bool)
		for iter.Next() {
			got[iter.Item().ChannelID] = true
		}
		if len(got) != 2 {
			t.Fatalf("got %d candidates, want 2", len(got))
		}
		if got[2] {
			t.Fatal("excluded channel 2 should not appear in candidates")
		}
		if !got[1] || !got[3] {
			t.Fatal("non-excluded channels 1 and 3 should remain")
		}
	})

	t.Run("nil excludes nothing", func(t *testing.T) {
		iter := NewIterator(group, 0, "m", nil)
		if iter.Len() != 3 {
			t.Fatalf("Len = %d, want 3", iter.Len())
		}
	})

	t.Run("exclude all yields empty", func(t *testing.T) {
		all := map[int]struct{}{1: {}, 2: {}, 3: {}}
		iter := NewIterator(group, 0, "m", all)
		if iter.Len() != 0 {
			t.Fatalf("Len = %d, want 0 when all channels excluded", iter.Len())
		}
	})
}
