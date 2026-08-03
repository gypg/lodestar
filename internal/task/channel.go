package task

import (
	"context"
	"time"

	"github.com/gypg/lodestar/internal/helper"
	"github.com/gypg/lodestar/internal/op/channel"
	"github.com/gypg/lodestar/internal/utils/log"
	"golang.org/x/sync/errgroup"
)

// delayFailureTracker 延迟探测任务的失败追踪器（进程生命周期内有效）
var delayFailureTracker = NewFailureTracker()

func ChannelBaseUrlDelayTask() {
	log.Debugf("channel base url delay task started")
	startTime := time.Now()
	defer func() {
		delayFailureTracker.Cleanup()
		log.Debugf("channel base url delay task finished, update time: %s", time.Since(startTime))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	channels, err := channel.List(ctx)
	if err != nil {
		log.Errorf("failed to list channels: %v", err)
		return
	}
	// 各 channel 的延迟探测彼此独立（按 channelID 更新各自缓存，无共享状态写
	// 冲突），且主要开销是网络探测。串行执行会让单轮耗时随 channel 数线性累加；
	// 用有界 worker pool 并发探测，单轮耗时接近最慢的单个 channel。
	// FailureTracker 自身用 mutex 保护，可安全并发。
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(syncFetchConcurrency())
	for _, ch := range channels {
		ch := ch
		if !ch.Enabled {
			continue
		}
		if delayFailureTracker.ShouldSkip(ch.ID) {
			log.Debugf("skipping channel %s (id=%d) — in cooldown", ch.Name, ch.ID)
			continue
		}
		g.Go(func() error {
			if err := helper.ChannelBaseUrlDelayUpdate(&ch, gctx); err != nil {
				delayFailureTracker.RecordFailure(ch.ID, ch.Name)
				return nil
			}
			delayFailureTracker.RecordSuccess(ch.ID)
			return nil
		})
	}
	_ = g.Wait()
}
