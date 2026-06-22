package model

import (
	"fmt"
	"sync"
	"time"
)

// keyModelCooldown tracks per (keyID, model) cooldown timestamps.
// When a key gets a 429 for a specific model, only that (key, model) pair
// is cooled down, not the key globally.
var keyModelCooldown sync.Map // key: "keyID:model" -> value: int64 (unix timestamp of 429)

// RecordKeyModelCooldown records a 429 cooldown for a specific (keyID, model) pair.
func RecordKeyModelCooldown(keyID int, modelName string) {
	if keyID == 0 || modelName == "" {
		return
	}
	k := fmt.Sprintf("%d:%s", keyID, modelName)
	keyModelCooldown.Store(k, time.Now().Unix())
}

// IsKeyModelOnCooldown checks if a specific (keyID, model) pair is still in cooldown.
func IsKeyModelOnCooldown(keyID int, modelName string, cooldownSec int) bool {
	if keyID == 0 || modelName == "" || cooldownSec <= 0 {
		return false
	}
	k := fmt.Sprintf("%d:%s", keyID, modelName)
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
