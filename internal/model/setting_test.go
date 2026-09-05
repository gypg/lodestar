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

// TestSettingValidateMaxExpectedRequestCost 钉死并发闸门配置的写入边界。
//
// 这个键原本在 Validate() 里没有分支，落到函数末尾的 return nil —— 任意字符串都存得
// 进去。"NaN"/"Inf" 这两类尤其致命：strconv.ParseFloat 认它们，闸门里的
// `headroom <= inflight*limit` 于是恒为 false，连"余额为负必须拒"都失效。
// 后果与运行时兜底见 internal/op/billing/inflight.go。
func TestSettingValidateMaxExpectedRequestCost(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "default seed", value: "0.5"},
		{name: "zero disables the bound", value: "0"},
		{name: "integer form", value: "2"},
		{name: "large but finite", value: "1000"},
		{name: "negative", value: "-1", wantErr: true},
		{name: "not a number", value: "abc", wantErr: true},
		{name: "empty", value: "", wantErr: true},
		{name: "leading space", value: " 0.5", wantErr: true},
		{name: "nan upper", value: "NaN", wantErr: true},
		{name: "nan lower", value: "nan", wantErr: true},
		{name: "inf", value: "Inf", wantErr: true},
		{name: "plus inf", value: "+Inf", wantErr: true},
		{name: "infinity word", value: "Infinity", wantErr: true},
		{name: "minus inf", value: "-Inf", wantErr: true},
		{name: "overflows float64", value: "1e400", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting := Setting{
				Key:   SettingKeyMaxExpectedRequestCost,
				Value: tt.value,
			}

			err := setting.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("Validate(%q) error = nil, want non-nil", tt.value)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate(%q) error = %v, want nil", tt.value, err)
			}
		})
	}
}

// TestDefaultSettingSeedsValidate 钉死出厂默认值全部过得了自己的校验器。
// 加新分支时最容易踩的就是把 seed 值判成非法 —— 那样全新部署会在第一次改设置时炸。
func TestDefaultSettingSeedsValidate(t *testing.T) {
	for _, seed := range DefaultSettings() {
		s := seed
		if err := s.Validate(); err != nil {
			t.Errorf("默认值 %s=%q 过不了 Validate(): %v", s.Key, s.Value, err)
		}
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

// TestSettingValidateAIRouteSourceMode 钉死来源开关的取值域。
//
// 这个键的唯一读者是分析中心的来源开关：前端按值恢复 local/external。任何第三种
// 字符串都会让恢复逻辑落进 else 分支显示成"外部连接"，与运营者选的相反 —— 而这正是
// 这批修复要消灭的症状，所以写入端必须先把它挡住，不能只靠前端兜底。
// 空串是合法的"从未选过"，用于首次进入时不写脏值。
func TestSettingValidateAIRouteSourceMode(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "local", value: AIRouteSourceModeLocal},
		{name: "external", value: AIRouteSourceModeExternal},
		{name: "never chosen", value: ""},
		{name: "unknown word", value: "hybrid", wantErr: true},
		{name: "wrong case", value: "Local", wantErr: true},
		{name: "padded", value: " local", wantErr: true},
		{name: "boolean lookalike", value: "true", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setting := Setting{Key: SettingKeyAIRouteSourceMode, Value: tt.value}

			err := setting.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate(%q) error = %v, wantErr = %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

// TestAIRouteSourceModeConstantsMatchWireValues 钉死常量的字面值。
//
// 前端 SettingKey.AIRouteSourceMode 写的是同样两个字符串，两端靠字面量对齐而非共享
// 类型。把常量改名不会破坏编译，但会让已存的行读不回来，所以这里断言字面值本身。
func TestAIRouteSourceModeConstantsMatchWireValues(t *testing.T) {
	if AIRouteSourceModeLocal != "local" {
		t.Fatalf("AIRouteSourceModeLocal = %q, want \"local\"", AIRouteSourceModeLocal)
	}
	if AIRouteSourceModeExternal != "external" {
		t.Fatalf("AIRouteSourceModeExternal = %q, want \"external\"", AIRouteSourceModeExternal)
	}
	if SettingKeyAIRouteSourceMode != "ai_route_source_mode" {
		t.Fatalf("SettingKeyAIRouteSourceMode = %q, want \"ai_route_source_mode\"", SettingKeyAIRouteSourceMode)
	}
}
