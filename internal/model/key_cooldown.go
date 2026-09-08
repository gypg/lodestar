package model

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// keyModelCooldown tracks per (keyID, model) cooldown timestamps.
// When a key gets a 429 for a specific model, only that (key, model) pair
// is cooled down, not the key globally.
var keyModelCooldown sync.Map // key: "keyID:model" -> value: int64 (unix timestamp of 429)

// keyModelCooldownKey 生成冷却键：keyID:model。三处（Record/Is/Clear）必须走
// 同一条构造——model 段小写归一化（octopus #238），漏一处就是 WO-045 阻断 2
// 那种「Clear 是 no-op」的回归。
func keyModelCooldownKey(keyID int, modelName string) string {
	return fmt.Sprintf("%d:%s", keyID, strings.ToLower(modelName))
}

// RecordKeyModelCooldown records a 429 cooldown for a specific (keyID, model) pair.
func RecordKeyModelCooldown(keyID int, modelName string) {
	if keyID == 0 || modelName == "" {
		return
	}
	keyModelCooldown.Store(keyModelCooldownKey(keyID, modelName), time.Now().Unix())
}

// IsKeyModelOnCooldown checks if a specific (keyID, model) pair is still in cooldown.
func IsKeyModelOnCooldown(keyID int, modelName string, cooldownSec int) bool {
	if keyID == 0 || modelName == "" || cooldownSec <= 0 {
		return false
	}
	k := keyModelCooldownKey(keyID, modelName)
	val, ok := keyModelCooldown.Load(k)
	if !ok {
		return false
	}
	ts, ok := val.(int64)
	if !ok {
		return false
	}
	return time.Now().Unix()-ts < int64(cooldownSec)
}

// ClearKeyModelCooldown removes the 429 cooldown for a (keyID, model) pair so
// the key can be re-selected immediately. Used by the rate-limit hold path,
// which retries the same channel after a delay instead of switching keys.
func ClearKeyModelCooldown(keyID int, modelName string) {
	if keyID == 0 || modelName == "" {
		return
	}
	keyModelCooldown.Delete(keyModelCooldownKey(keyID, modelName))
}

// CleanupKeyModelCooldown removes expired cooldown entries.
func CleanupKeyModelCooldown(cooldownSec int) {
	if cooldownSec <= 0 {
		return
	}
	now := time.Now().Unix()
	keyModelCooldown.Range(func(key, value any) bool {
		ts, ok := value.(int64)
		if ok && now-ts >= int64(cooldownSec) {
			keyModelCooldown.Delete(key)
		}
		return true
	})
}
