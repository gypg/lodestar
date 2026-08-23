package model

import "testing"

func TestSettingValidateAlertNotifyLanguage(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "simplified chinese", value: "zh-Hans"},
		{name: "traditional chinese", value: "zh-Hant"},
		{name: "english", value: "en"},
		{name: "invalid locale", value: "ja", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting := Setting{
				Key:   SettingKeyAlertNotifyLanguage,
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

func TestSettingValidateNavOrder(t *testing.T) {
	tests := []struct {
		name    string
		key     SettingKey
		value   string
		wantErr bool
	}{
		{name: "valid nav order array", key: SettingKeyNavOrder, value: `["home","setting"]`},
		{name: "valid nav visible array", key: SettingKeyNavVisible, value: `["home","setting"]`},
		{name: "malformed json", key: SettingKeyNavOrder, value: `["home"`, wantErr: true},
		{name: "non array value", key: SettingKeyNavVisible, value: `{"home":1}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting := Setting{
				Key:   tt.key,
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

// S-5: 通用设置端点（handlers/setting.go 走 Validate）也能写 webdav_config，
// 所以 base_url 的 SSRF 校验必须落在验证器里，否则 handlers/webdav.go 的检查
// 会被 /api/v1/setting 绕过。
func TestSettingValidateWebDAVConfigRejectsUnsafeBaseURL(t *testing.T) {
	withBaseURL := func(baseURL string) string {
		return `{"enabled":true,"base_url":"` + baseURL + `","remote_path":"/lodestar-backup/","interval_hours":6,"max_backups":10}`
	}

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "loopback", value: withBaseURL("http://127.0.0.1:8080/dav"), wantErr: true},
		{name: "localhost", value: withBaseURL("http://localhost/dav"), wantErr: true},
		{name: "private range", value: withBaseURL("http://192.168.1.10/dav"), wantErr: true},
		{name: "cloud metadata", value: withBaseURL("http://169.254.169.254/latest/meta-data/"), wantErr: true},
		{name: "file scheme", value: withBaseURL("file:///etc/passwd"), wantErr: true},
		{name: "trailing slash loopback", value: withBaseURL("http://127.0.0.1/"), wantErr: true},
		// 空 base_url = 未配置，必须仍可保存（默认种子就是空）。
		{name: "empty base url", value: withBaseURL("")},
		{name: "base url absent", value: `{"enabled":false,"interval_hours":6}`},
		// 字面公网 IP 不走 DNS，测试不依赖网络。
		{name: "public ip", value: withBaseURL("https://8.8.8.8/dav")},
		{name: "malformed json", value: `{"enabled":`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting := Setting{
				Key:   SettingKeyWebDAVConfig,
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

// 默认种子必须通过自己的验证器，否则全新部署会在设置页保存时报错。
func TestDefaultWebDAVConfigSeedIsValid(t *testing.T) {
	for _, s := range DefaultSettings() {
		if s.Key != SettingKeyWebDAVConfig {
			continue
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("default webdav_config seed fails Validate(): %v", err)
		}
		return
	}
	t.Fatal("no default seed found for webdav_config")
}

// 系统代理设置与代理池条目是同一个概念的两个入口，必须接受同一组 scheme。
// 它们曾经不一致：NormalizeProxyURL 收 socks、SettingKeyProxyURL 的验证器
// 不收，但后者的错误消息却把 socks 列为合法——于是"哪个能填"取决于走哪个入口。
func TestProxySchemeValidatorsAgree(t *testing.T) {
	tests := []struct {
		scheme  string
		wantErr bool
	}{
		{scheme: "http"},
		{scheme: "https"},
		{scheme: "socks"},
		{scheme: "socks5"},
		{scheme: "socks4", wantErr: true},
		{scheme: "ftp", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			raw := tt.scheme + "://proxy.example.com:1080"

			_, poolErr := NormalizeProxyURL(raw)
			proxySetting := Setting{Key: SettingKeyProxyURL, Value: raw}
			settingErr := proxySetting.Validate()

			if (poolErr != nil) != tt.wantErr {
				t.Fatalf("NormalizeProxyURL(%q) error = %v, wantErr = %v", raw, poolErr, tt.wantErr)
			}
			if (settingErr != nil) != tt.wantErr {
				t.Fatalf("SettingKeyProxyURL Validate(%q) error = %v, wantErr = %v", raw, settingErr, tt.wantErr)
			}
		})
	}
}
