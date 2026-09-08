package handlers

import "testing"

// WO-044 #239：page 无上界的深 OFFSET 是 DB 消耗面（低权 logs:read 可触发）。
func TestParsePagination(t *testing.T) {
	tests := []struct {
		name              string
		rawPage, rawSize  string
		wantPage, wantLen int
	}{
		{"defaults", "", "", 1, 20},
		{"normal", "3", "50", 3, 50},
		{"page zero clamps to 1", "0", "20", 1, 20},
		{"page negative clamps to 1", "-5", "20", 1, 20},
		{"page over cap clamps", "50000", "20", 1000, 20},
		{"pageSize over cap clamps", "1", "5000", 1, 20},
		{"pageSize zero defaults", "1", "0", 1, 20},
		{"garbage page", "abc", "20", 1, 20},
		{"cap boundary allowed", "1000", "100", 1000, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, size := parsePagination(tt.rawPage, tt.rawSize)
			if page != tt.wantPage || size != tt.wantLen {
				t.Fatalf("parsePagination(%q, %q) = (%d, %d), want (%d, %d)",
					tt.rawPage, tt.rawSize, page, size, tt.wantPage, tt.wantLen)
			}
		})
	}
}
