package setting

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/gypg/lodestar/internal/db"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/utils/cache"
	"github.com/gypg/lodestar/internal/utils/crypto"
)

var settingCache = cache.New[model.SettingKey, string](16)

// generation 在每次设置发生变更时自增。调用方可以缓存基于设置派生的
// 配置（如语义缓存运行时配置），只在代际变化时重新读取，避免在请求热
// 路径上反复读取多个设置并重建配置。
var generation atomic.Uint64

// Generation 返回当前设置代际。每当任意设置被写入（SetString/SetInt）或
// 缓存被整体刷新（RefreshCache）时该值都会改变。
func Generation() uint64 { return generation.Load() }

// GetCache returns the internal setting cache (for backward compatibility).
func GetCache() cache.Cache[model.SettingKey, string] { return settingCache }

func List(ctx context.Context) ([]model.Setting, error) {
	settings := make([]model.Setting, 0, settingCache.Len())
	for key, value := range settingCache.GetAll() {
		settings = append(settings, model.Setting{
			Key:   key,
			Value: value,
		})
	}
	return settings, nil
}

func GetString(key model.SettingKey) (string, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return "", fmt.Errorf("setting not found")
	}
	decrypted, err := crypto.DecryptSettingValue(string(key), setting)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt setting %s: %w", key, err)
	}
	return decrypted, nil
}

func SetString(key model.SettingKey, value string) error {
	valueCache, ok := settingCache.Get(key)
	if !ok {
		return fmt.Errorf("setting not found")
	}
	// Decrypt cached value before comparison so that "same value" checks work
	// even when the cache holds the encrypted form.
	decryptedCache, decErr := crypto.DecryptSettingValue(string(key), valueCache)
	if decErr != nil {
		return fmt.Errorf("failed to decrypt cached setting %s: %w", key, decErr)
	}
	if decryptedCache == value {
		return nil
	}
	// Encrypt sensitive values before persisting.
	persistValue, encErr := crypto.EncryptSettingValue(string(key), value)
	if encErr != nil {
		return fmt.Errorf("failed to encrypt setting %s: %w", key, encErr)
	}
	result := db.GetDB().Model(&model.Setting{Key: key}).Update("Value", persistValue)
	if result.Error != nil {
		return fmt.Errorf("failed to set setting: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("failed to set setting, key not found")
	}
	settingCache.Set(key, persistValue)
	generation.Add(1)
	return nil
}

func GetInt(key model.SettingKey) (int, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return 0, fmt.Errorf("setting not found")
	}
	return strconv.Atoi(setting)
}

func GetBool(key model.SettingKey) (bool, error) {
	setting, ok := settingCache.Get(key)
	if !ok {
		return false, fmt.Errorf("setting not found")
	}
	return strconv.ParseBool(setting)
}

func SetInt(key model.SettingKey, value int) error {
	valueCache, ok := settingCache.Get(key)
	if !ok {
		return fmt.Errorf("setting not found")
	}
	valueCacheNum, err := strconv.Atoi(valueCache)
	if err != nil {
		return fmt.Errorf("failed to set setting: %w", err)
	}
	if valueCacheNum == value {
		return nil
	}
	result := db.GetDB().Model(&model.Setting{Key: key}).Update("Value", value)
	if result.Error != nil {
		return fmt.Errorf("failed to set setting: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("failed to set setting, key not found")
	}
	settingCache.Set(key, strconv.Itoa(value))
	generation.Add(1)
	return nil
}

func RefreshCache(ctx context.Context) error {
	conn := db.GetDB().WithContext(ctx)

	var settings []model.Setting
	if err := conn.Find(&settings).Error; err != nil {
		return fmt.Errorf("failed to get settings: %w", err)
	}

	existingKeys := make(map[model.SettingKey]bool)
	for _, setting := range settings {
		existingKeys[setting.Key] = true
	}

	defaultSettings := model.DefaultSettings()
	missingSettings := make([]model.Setting, 0, len(defaultSettings))

	for _, defaultSetting := range defaultSettings {
		if !existingKeys[defaultSetting.Key] {
			missingSettings = append(missingSettings, defaultSetting)
		}
	}

	if len(missingSettings) > 0 {
		if err := conn.CreateInBatches(missingSettings, len(missingSettings)).Error; err != nil {
			return fmt.Errorf("failed to create missing settings: %w", err)
		}
		settings = append(settings, missingSettings...)
	}
	settingCache.Clear()
	for _, setting := range settings {
		settingCache.Set(setting.Key, setting.Value)
	}
	generation.Add(1)
	return nil
}
