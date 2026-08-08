package conf

import "strings"

// DefaultTrustedProxies 返回默认可信代理网段（S-3）。
//
// gin.New() 的默认值是 ["0.0.0.0/0", "::/0"]（gin@v1.11.0 gin.go:215），
// 即无条件信任任何直连来源发来的 X-Forwarded-For / X-Real-IP。实测后果：
// 一个直连公网客户端只要带上 `X-Forwarded-For: 10.0.0.5`，c.ClientIP() 就返回
// 10.0.0.5，据此做的所有按 IP 决策全部失守——其中 API key 的 IP 白名单
// (middleware/auth.go:176) 是可被单个请求头完全绕过的访问控制。
//
// 这里收窄为回环 + RFC1918 私网 + RFC6598 CGNAT + 链路本地：
//   - 反代与应用同机（Cloudflare 隧道、nginx 转 127.0.0.1）走回环；
//   - 容器/K8s 内反代走私网（docker 默认 172.17/16、compose 自建网段亦在 172.16/12）；
//   - 公网直连客户端不在任何网段内，伪造的 XFF 被忽略，只认 TCP 源地址。
//
// 若反代在另一台公网主机上，需在配置里显式列出它的地址；若应用直接对公网提供
// 服务且前面没有任何代理，可显式配成空数组，彻底不认这两个头。
func DefaultTrustedProxies() []string {
	return []string{
		"127.0.0.0/8",    // IPv4 回环
		"::1/128",        // IPv6 回环
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918（含 docker 默认 bridge 172.17.0.0/16）
		"192.168.0.0/16", // RFC1918
		"100.64.0.0/10",  // RFC6598 CGNAT（Tailscale / 运营商级 NAT）
		"169.254.0.0/16", // 链路本地
		"fc00::/7",       // IPv6 唯一本地地址
		"fe80::/10",      // IPv6 链路本地
	}
}

// TrustedProxies 返回生效的可信代理列表，供 gin 的 SetTrustedProxies 使用。
//
// nil（从未配置）回落到 DefaultTrustedProxies()，显式空数组 `[]` 原样保留。
// 这个区分很重要：viper.SetDefault 只在走过 Load() 之后才填上默认值，任何绕过
// Load() 的路径拿到的是 nil；若把 nil 也当成"空列表"，就会静默变成完全不信任
// 代理，反代后面所有客户端的 IP 都退化成反代自己的地址——限流与 IP 白名单
// 会按反代地址聚合，是另一种失效方向。显式配 `[]` 则是运维明确表达
// "本服务直接对外，不认这两个头"，必须尊重。
//
// 逐项 TrimSpace 并丢空项：env 形式的
// `LODESTAR_SERVER_TRUSTED_PROXIES=127.0.0.1, 10.0.0.0/8` 会被 viper 按逗号切成
// 带前导空格的项，gin 的 net.ParseCIDR 不接受空格（实测报
// `invalid CIDR address:  10.0.0.0/8`），不归一化会直接启动失败。
//
// gin 侧 nil 与空切片行为一致，都是"不信任任何代理"：nil 走
// prepareTrustedCIDRs 的早退（gin.go:405）把 trustedCIDRs 置 nil，
// 而 isTrustedProxy 对 nil 恒返回 false（gin.go:460-462）。
func TrustedProxies() []string {
	configured := AppConfig.Server.TrustedProxies
	if configured == nil {
		return DefaultTrustedProxies()
	}

	cleaned := make([]string, 0, len(configured))
	for _, entry := range configured {
		if entry = strings.TrimSpace(entry); entry != "" {
			cleaned = append(cleaned, entry)
		}
	}
	// 配了内容但全是空白（`[" "]`）→ 视为写坏了，用默认值而不是静默变成不信任。
	// 真想不信任任何代理请显式写 `[]`。
	if len(cleaned) == 0 && len(configured) > 0 {
		return DefaultTrustedProxies()
	}
	return cleaned
}
