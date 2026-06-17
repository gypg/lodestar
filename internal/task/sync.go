package task

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/lingyuins/octopus/internal/helper"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/channel"
	"github.com/lingyuins/octopus/internal/op/group"
	"github.com/lingyuins/octopus/internal/op/llm"

	"github.com/lingyuins/octopus/internal/utils/diff"
	"github.com/lingyuins/octopus/internal/utils/log"
	"github.com/lingyuins/octopus/internal/utils/xstrings"
)

// syncFetchConcurrency bounds how many channels are probed for their model
// list in parallel during SyncModelsTask. Each probe is a network request with
// a short timeout, so a bounded pool keeps the batch wall-clock near the
// slowest single probe without opening an unbounded number of connections.
func syncFetchConcurrency() int {
	if n := runtime.GOMAXPROCS(0) * 2; n > 8 {
		return n
	}
	return 8
}

var lastSyncModelsTime = time.Now()

// syncFailureTracker 模型同步任务的失败追踪器（进程生命周期内有效）
var syncFailureTracker = NewFailureTracker()

// SyncModelsTask 同步模型任务
func SyncModelsTask() {
	log.Debugf("sync models task started")
	startTime := time.Now()
	defer func() {
		syncFailureTracker.Cleanup()
		log.Debugf("sync models task finished, sync time: %s", time.Since(startTime))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	channels, err := channel.List(ctx)
	if err != nil {
		log.Errorf("failed to list channels: %v", err)
		return
	}
	totalNewModels := make([]string, 0, 128)
	seenTotalNewModels := make(map[string]struct{}, 128)

	// 阶段一：并发抓取各 channel 的模型列表（网络 IO）。抓取彼此独立，
	// 串行执行会让单轮耗时随 channel 数线性累加；用有界 worker pool 并发抓取，
	// 单轮耗时接近最慢的单个 channel。FailureTracker 自身用 mutex 保护，可安全并发。
	type fetchResult struct {
		ch          model.Channel
		fetchModels []string
	}
	var (
		fetchMu      sync.Mutex
		fetchResults = make([]fetchResult, 0, len(channels))
	)
	fg, fgctx := errgroup.WithContext(ctx)
	fg.SetLimit(syncFetchConcurrency())
	for _, ch := range channels {
		ch := ch
		if !ch.Enabled || !ch.AutoSync {
			continue
		}
		if syncFailureTracker.ShouldSkip(ch.ID) {
			log.Debugf("skipping channel %s (id=%d) — in cooldown", ch.Name, ch.ID)
			continue
		}
		fg.Go(func() error {
			fetchModels, err := helper.FetchModelsShortTimeout(fgctx, ch)
			if err != nil {
				log.Warnf("failed to fetch models for channel %s: %v", ch.Name, err)
				syncFailureTracker.RecordFailure(ch.ID, ch.Name)
				return nil
			}
			syncFailureTracker.RecordSuccess(ch.ID)
			fetchMu.Lock()
			fetchResults = append(fetchResults, fetchResult{ch: ch, fetchModels: fetchModels})
			fetchMu.Unlock()
			return nil
		})
	}
	_ = fg.Wait()

	// 阶段二：串行处理抓取结果（DB 更新、自动分组、totalNewModels 累加），
	// 避免对共享状态的并发写。
	for _, fr := range fetchResults {
		ch := fr.ch
		fetchModels := fr.fetchModels
		oldModels := xstrings.SplitTrimCompact(",", ch.Model)
		newModels := xstrings.TrimCompact(fetchModels)
		for _, m := range newModels {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			m = strings.ToLower(m)
			if _, ok := seenTotalNewModels[m]; ok {
				continue
			}
			seenTotalNewModels[m] = struct{}{}
			totalNewModels = append(totalNewModels, m)
		}
		deletedModels, addedModels := diff.Diff(oldModels, newModels)
		if len(deletedModels) > 0 || len(addedModels) > 0 {
			fetchModelStr := strings.Join(newModels, ",")
			if _, err := channel.Update(&model.ChannelUpdateRequest{
				ID:    ch.ID,
				Model: &fetchModelStr,
			}, ctx); err != nil {
				log.Errorf("failed to update channel %s: %v", ch.Name, err)
				continue
			}
		}
		// 批量删除消失的模型对应的 GroupItem
		if len(deletedModels) > 0 {
			log.Infof("deleted channel %s models: %v", ch.Name, deletedModels)
			keys := make([]model.GroupIDAndLLMName, len(deletedModels))
			for i, m := range deletedModels {
				keys[i] = model.GroupIDAndLLMName{ChannelID: ch.ID, ModelName: m}
			}
			if err := group.GroupItemBatchDelByChannelAndModels(keys, ctx); err != nil {
				log.Errorf("failed to batch delete group items for channel %s: %v", ch.Name, err)
			}
		}

		// 自动分组
		if len(newModels) > 0 {
			helper.ChannelAutoGroup(&ch, ctx)
		}
	}
	llmPrice, err := llm.List(ctx)
	if err != nil {
		log.Errorf("failed to list models price: %v", err)
		return
	}
	llmPriceNames := make([]string, 0, len(llmPrice))
	for _, price := range llmPrice {
		llmPriceNames = append(llmPriceNames, price.Name)
	}

	deletedNorm, addedNorm := diff.Diff(llmPriceNames, totalNewModels)
	if len(deletedNorm) > 0 {
		if err := helper.LLMPriceDeleteFromDBWithNoPrice(deletedNorm, ctx); err != nil {
			log.Errorf("failed to batch delete models price: %v", err)
		}
	}
	if len(addedNorm) > 0 {
		if err := helper.LLMPriceAddToDB(addedNorm, ctx); err != nil {
			log.Errorf("failed to add models price: %v", err)
		}
	}
	lastSyncModelsTime = time.Now()
}

func GetLastSyncModelsTime() time.Time {
	return lastSyncModelsTime
}
