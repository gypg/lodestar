package helper

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gypg/lodestar/internal/client"
	"github.com/gypg/lodestar/internal/model"
	ch "github.com/gypg/lodestar/internal/op/channel"
	grp "github.com/gypg/lodestar/internal/op/group"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/xregexp"
	"github.com/gypg/lodestar/internal/utils/xstrings"
)

// resolveChannelProxy 解析渠道最终使用的代理来源。
//
// 优先级：自定义 URL（channel_proxy）> proxy_mode。自定义 URL 优先是为了兼容
// 既有数据 —— R-10 之前渠道代理完全由 proxy + channel_proxy 决定，proxy_mode
// 那一列是死的（更新白名单里没有它，前端也从不发）。迁移 018 已把既有语义回填进
// proxy_mode，但配了自定义 URL 的渠道回填成 direct（自定义 URL 不属于代理池），
// 故这里必须让 channel_proxy 继续生效，否则它们会静默失去代理。
//
// 返回 (自定义代理 URL, 是否走系统代理, error)。两者都空/false 即直连。
func resolveChannelProxy(ctx context.Context, channel *model.Channel) (string, bool, error) {
	if custom := trimmedChannelProxy(channel); custom != "" {
		return custom, false, nil
	}
	switch channel.ProxyMode {
	case "", model.ProxyUsageModeDirect:
		// 空值兜底为 direct：老库在迁移跑之前可能仍是空串。
		// 但 proxy=true 说明这是迁移前的"用系统代理"语义，尊重它。
		if channel.Proxy {
			return "", true, nil
		}
		return "", false, nil
	case model.ProxyUsageModeSystem:
		return "", true, nil
	case model.ProxyUsageModePool:
		if channel.ProxyConfigID == nil || *channel.ProxyConfigID <= 0 {
			return "", false, fmt.Errorf("channel %d: proxy config id is required when proxy mode is pool", channel.ID)
		}
		// 复用 op/channel 里那个由 op 注入的回调（见 op/channel.go 的 init），
		// helper 不能直接 import op —— 虽然当前 op 不 import helper，
		// 但 helper 已依赖 op/channel，走同一个 seam 更稳且零新增接线。
		proxyURL, err := ch.ProxyURLForConfig(*channel.ProxyConfigID, ctx)
		if err != nil {
			return "", false, fmt.Errorf("channel %d: %w", channel.ID, err)
		}
		return proxyURL, false, nil
	default:
		return "", false, fmt.Errorf("channel %d: unsupported proxy mode: %s", channel.ID, channel.ProxyMode)
	}
}

func trimmedChannelProxy(channel *model.Channel) string {
	if channel == nil || channel.ChannelProxy == nil {
		return ""
	}
	return strings.TrimSpace(*channel.ChannelProxy)
}

func ChannelHttpClient(channel *model.Channel) (*http.Client, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	customURL, useSystem, err := resolveChannelProxy(context.Background(), channel)
	if err != nil {
		return nil, err
	}
	if customURL != "" {
		return client.GetHTTPClientCustomProxy(customURL)
	}
	return client.GetHTTPClientSystemProxy(useSystem)
}

// ChannelShortTimeoutHttpClient 返回一个短超时(30s)的 HTTP 客户端
// 用于后台任务(延迟探测、模型同步)，避免在 endpoint 不可达时 goroutine 堆积
func ChannelShortTimeoutHttpClient(channel *model.Channel) (*http.Client, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	customURL, useSystem, err := resolveChannelProxy(context.Background(), channel)
	if err != nil {
		return nil, err
	}
	if customURL != "" {
		return client.GetHTTPClientCustomProxyWithTimeout(customURL, 30*time.Second)
	}
	return client.GetHTTPClientShortTimeout(useSystem)
}

// ChannelBaseUrlDelayUpdate 更新 channel 的 base URL 延迟信息（使用短超时客户端）
// 返回 error 表示所有 base URL 都探测失败
func ChannelBaseUrlDelayUpdate(channel *model.Channel, ctx context.Context) error {
	if channel == nil {
		return errors.New("channel is nil")
	}
	newBaseUrls := make([]model.BaseUrl, 0, len(channel.BaseUrls))
	allFailed := true

	for _, baseUrl := range channel.BaseUrls {
		if baseUrl.URL == "" {
			continue
		}
		httpClient, err := ChannelShortTimeoutHttpClient(channel)
		if err != nil {
			log.Warnf("failed to get http client (channel=%d): %v", channel.ID, err)
			continue
		}
		delay, err := GetUrlDelay(httpClient, baseUrl.URL, ctx)
		if err != nil {
			log.Warnf("failed to get url delay (channel=%d, url=%s): %v", channel.ID, baseUrl.URL, err)
			continue
		}
		allFailed = false
		newBaseUrls = append(newBaseUrls, model.BaseUrl{
			URL:        baseUrl.URL,
			Delay:      delay,
			SuffixMode: baseUrl.SuffixMode,
		})
	}
	if len(newBaseUrls) > 0 {
		ch.BaseUrlUpdate(channel.ID, newBaseUrls)
	}

	if allFailed && len(channel.BaseUrls) > 0 {
		return fmt.Errorf("all base URLs failed for channel %d", channel.ID)
	}
	return nil
}

func ChannelAutoGroup(channel *model.Channel, ctx context.Context) {
	if channel == nil {
		return
	}
	if channel.AutoGroup == model.AutoGroupTypeNone {
		return
	}
	groups, err := grp.GroupList(ctx)
	if err != nil {
		log.Warnf("get group list failed: %v", err)
		return
	}

	channelModelNames := xstrings.SplitTrimCompact(",", channel.Model, channel.CustomModel)
	if len(channelModelNames) == 0 {
		return
	}

	for _, group := range groups {
		matchedModelNames := make([]string, 0, len(channelModelNames))

		switch channel.AutoGroup {
		case model.AutoGroupTypeExact:
			for _, modelName := range channelModelNames {
				if strings.EqualFold(modelName, group.Name) {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}

		case model.AutoGroupTypeFuzzy:
			groupNameLower := strings.ToLower(strings.TrimSpace(group.Name))
			if groupNameLower == "" {
				continue
			}
			for _, modelName := range channelModelNames {
				if strings.Contains(strings.ToLower(modelName), groupNameLower) {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}

		case model.AutoGroupTypeRegex:
			if group.MatchRegex == "" {
				for _, modelName := range channelModelNames {
					if strings.EqualFold(modelName, group.Name) {
						matchedModelNames = append(matchedModelNames, modelName)
					}
				}
				break
			}

			re, err := xregexp.CompileECMAScript(group.MatchRegex)
			if err != nil {
				log.Warnf("compile regex failed (channel=%d group=%d regex=%q): %v", channel.ID, group.ID, group.MatchRegex, err)
				continue
			}
			for _, modelName := range channelModelNames {
				matched, err := re.MatchString(modelName)
				if err != nil {
					log.Warnf("match regex failed (channel=%d group=%d regex=%q model=%q): %v", channel.ID, group.ID, group.MatchRegex, modelName, err)
					continue
				}
				if matched {
					matchedModelNames = append(matchedModelNames, modelName)
				}
			}
		}

		if len(matchedModelNames) > 0 {
			items := make([]model.GroupIDAndLLMName, 0, len(matchedModelNames))
			for _, modelName := range matchedModelNames {
				items = append(items, model.GroupIDAndLLMName{
					ChannelID: channel.ID,
					ModelName: modelName,
				})
			}
			if err := grp.GroupItemBatchAdd(group.ID, items, ctx); err != nil {
				log.Warnf("group item batch add failed (channel=%d group=%d): %v", channel.ID, group.ID, err)
			}
		}
	}
}
