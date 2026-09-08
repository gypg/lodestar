package model

import "testing"

// WO-044 #237：语义缓存 embedding base_url 出站携带全量对话文本，威胁模型与
// WebDAV base_url 相同（settings:write 可控 URL），写入边界必须过同一道 SSRF
// 校验。只拦写边界——存量配置不被打断（Validate 仅在写入时触发）。
func TestSettingValidateSemanticCacheEmbeddingBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "empty = not configured, allowed", value: ""},
		{name: "loopback rejected", value: "http://127.0.0.1:11434", wantErr: true},
		{name: "localhost rejected", value: "http://localhost:11434", wantErr: true},
		{name: "private range rejected", value: "http://192.168.1.10:11434", wantErr: true},
		{name: "cloud metadata rejected", value: "http://169.254.169.254/latest/meta-data/", wantErr: true},
		{name: "file scheme rejected", value: "file:///etc/passwd", wantErr: true},
		{name: "public ip allowed (no DNS in test)", value: "https://8.8.8.8/v1"},
		{name: "missing host rejected", value: "http://", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting := Setting{
				Key:   SettingKeySemanticCacheEmbeddingBaseURL,
				Value: tt.value,
			}
			err := setting.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}
