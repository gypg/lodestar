/**
 * 策略预设（纯前端、无依赖）。
 *
 * 一个命名预设 = 推荐分组模式 + 一组全局旋钮（熔断/重试/限流冷却）+ 推荐密钥数
 * 范围 + 场景说明。设计参考 N-SLMCRS 的 kernel-rs/src/strategy.rs PRESETS
 * （Guardian/Balanced/Velocity/Fairshare/Adaptive），映射到 Lodestar 既有的
 * GroupMode 与 Retry/CircuitBreaker 设置：
 *
 * 概念映射与取舍（刻意不照搬的部分）：
 *   - StrictPriority（永远先打最健康的）在 Lodestar 无直接对应，用 Failover
 *     （按优先级顺序、失败才回退）近似，UI 注明。
 *   - LeastInflight（最少在途优先）无对应：新增需要动 balancer 核心（重），
 *     本期省略，Velocity 用 Random + 短冷却近似其低延迟诉求，UI 注明。
 *   - racing fanout（N 路并发先到先得）明确不引入；Guardian/Fairshare 本就
 *     fanout=1，语义由「重试 1 次即换下一家」近似。
 *   - RPM 头寸（rpm_headroom）无全局旋钮，Lodestar 由密钥级限流承担，省略。
 *
 * 旋钮数值相对后端默认值（internal/model/setting.go:142-151）调校：
 * retry=3 / threshold=5 / cooldown=60 / ratelimit_cooldown=300 / max_total=0。
 */

export type StrategyPresetId = 'guardian' | 'balanced' | 'velocity' | 'fairshare' | 'adaptive';

/** GroupMode 枚举值的本地镜像（web/src/api/endpoints/group.ts:23-29）。 */
export const GROUP_MODE = {
    RoundRobin: 1,
    Random: 2,
    Failover: 3,
    Weighted: 4,
    Auto: 5,
} as const;

/** 预设将批量写入的设置键（与 internal/model/setting.go 的 SettingKey 字符串一致）。 */
export const PRESET_SETTING_KEYS = {
    RelayRetryCount: 'relay_retry_count',
    RatelimitCooldown: 'ratelimit_cooldown',
    RelayMaxTotalAttempts: 'relay_max_total_attempts',
    CircuitBreakerThreshold: 'circuit_breaker_threshold',
    CircuitBreakerCooldown: 'circuit_breaker_cooldown',
} as const;

export interface StrategyPresetSettingWrite {
    key: string;
    value: string;
}

export interface StrategyPreset {
    id: StrategyPresetId;
    icon: string;
    /** 展示名 locale key（setting.strategyPresets.* 下）。 */
    nameKey: string;
    /** 一句话人设 locale key。 */
    characterKey: string;
    /** 场景说明 locale key。 */
    scenarioKey: string;
    /** 推荐的分组模式（GroupMode 值）。 */
    mode: number;
    /** 该模式的语义是近似（如 Failover≈StrictPriority）时的说明 locale key。 */
    approximationKey?: string;
    /** 推荐密钥数下界（0 = 无下界）。 */
    minKeys: number;
    /** 推荐密钥数上界（0 = 无上界）。 */
    maxKeys: number;
    /** 选定预设后批量写入的全局旋钮。 */
    settings: StrategyPresetSettingWrite[];
}

/** 默认预设 id（与 N-SLMCRS DEFAULT_ID = "balanced" 一致）。 */
export const DEFAULT_PRESET_ID: StrategyPresetId = 'balanced';

export const STRATEGY_PRESETS: StrategyPreset[] = [
    {
        id: 'guardian',
        icon: '🛡️',
        nameKey: 'setting.strategyPresets.name.guardian',
        characterKey: 'setting.strategyPresets.character.guardian',
        scenarioKey: 'setting.strategyPresets.scenario.guardian',
        // StrictPriority ≈ Failover：都保证"先打排最前的、失败才回退"。
        mode: GROUP_MODE.Failover,
        approximationKey: 'setting.strategyPresets.approximation.strictPriority',
        minKeys: 0,
        maxKeys: 3,
        settings: [
            { key: PRESET_SETTING_KEYS.CircuitBreakerThreshold, value: '20' },
            { key: PRESET_SETTING_KEYS.CircuitBreakerCooldown, value: '60' },
            { key: PRESET_SETTING_KEYS.RelayRetryCount, value: '3' },
            { key: PRESET_SETTING_KEYS.RatelimitCooldown, value: '600' },
            { key: PRESET_SETTING_KEYS.RelayMaxTotalAttempts, value: '8' },
        ],
    },
    {
        id: 'balanced',
        icon: '⚖️',
        nameKey: 'setting.strategyPresets.name.balanced',
        characterKey: 'setting.strategyPresets.character.balanced',
        scenarioKey: 'setting.strategyPresets.scenario.balanced',
        mode: GROUP_MODE.Weighted,
        minKeys: 4,
        maxKeys: 7,
        // 数值即后端出厂默认：应用本预设等价于回到均衡基线。
        settings: [
            { key: PRESET_SETTING_KEYS.CircuitBreakerThreshold, value: '5' },
            { key: PRESET_SETTING_KEYS.CircuitBreakerCooldown, value: '60' },
            { key: PRESET_SETTING_KEYS.RelayRetryCount, value: '3' },
            { key: PRESET_SETTING_KEYS.RatelimitCooldown, value: '300' },
            { key: PRESET_SETTING_KEYS.RelayMaxTotalAttempts, value: '0' },
        ],
    },
    {
        id: 'velocity',
        icon: '⚡',
        nameKey: 'setting.strategyPresets.name.velocity',
        characterKey: 'setting.strategyPresets.character.velocity',
        scenarioKey: 'setting.strategyPresets.scenario.velocity',
        // LeastInflight 无对应（见文件头注释），Random + 短冷却近似低延迟。
        mode: GROUP_MODE.Random,
        approximationKey: 'setting.strategyPresets.approximation.leastInflight',
        minKeys: 8,
        maxKeys: 0,
        settings: [
            { key: PRESET_SETTING_KEYS.CircuitBreakerThreshold, value: '5' },
            { key: PRESET_SETTING_KEYS.CircuitBreakerCooldown, value: '30' },
            { key: PRESET_SETTING_KEYS.RelayRetryCount, value: '2' },
            { key: PRESET_SETTING_KEYS.RatelimitCooldown, value: '60' },
            { key: PRESET_SETTING_KEYS.RelayMaxTotalAttempts, value: '0' },
        ],
    },
    {
        id: 'fairshare',
        icon: '🎯',
        nameKey: 'setting.strategyPresets.name.fairshare',
        characterKey: 'setting.strategyPresets.character.fairshare',
        scenarioKey: 'setting.strategyPresets.scenario.fairshare',
        mode: GROUP_MODE.RoundRobin,
        minKeys: 2,
        maxKeys: 0,
        settings: [
            { key: PRESET_SETTING_KEYS.CircuitBreakerThreshold, value: '5' },
            { key: PRESET_SETTING_KEYS.CircuitBreakerCooldown, value: '60' },
            // fanout=1 零浪费的近似：重试 1 次（换下一家）即止。
            { key: PRESET_SETTING_KEYS.RelayRetryCount, value: '1' },
            { key: PRESET_SETTING_KEYS.RatelimitCooldown, value: '300' },
            { key: PRESET_SETTING_KEYS.RelayMaxTotalAttempts, value: '0' },
        ],
    },
    {
        id: 'adaptive',
        icon: '🤖',
        nameKey: 'setting.strategyPresets.name.adaptive',
        characterKey: 'setting.strategyPresets.character.adaptive',
        scenarioKey: 'setting.strategyPresets.scenario.adaptive',
        mode: GROUP_MODE.Auto,
        minKeys: 0,
        maxKeys: 0,
        // Auto-Pilot 控制环对应 Auto 模式 + AutoStrategy 设置（独立卡片，不在预设内改）。
        settings: [
            { key: PRESET_SETTING_KEYS.CircuitBreakerThreshold, value: '5' },
            { key: PRESET_SETTING_KEYS.CircuitBreakerCooldown, value: '60' },
            { key: PRESET_SETTING_KEYS.RelayRetryCount, value: '3' },
            { key: PRESET_SETTING_KEYS.RatelimitCooldown, value: '300' },
            { key: PRESET_SETTING_KEYS.RelayMaxTotalAttempts, value: '0' },
        ],
    },
];

/** 按 id 查预设。 */
export function presetById(id: string): StrategyPreset | undefined {
    return STRATEGY_PRESETS.find((p) => p.id === id);
}

/** 默认预设（balanced）。 */
export function defaultPreset(): StrategyPreset {
    const p = presetById(DEFAULT_PRESET_ID);
    if (!p) throw new Error('balanced preset must exist');
    return p;
}

/**
 * 按密钥数推荐预设 id（UI"推荐"徽章用），与 N-SLMCRS recommend() 同阈值：
 * M≤3→guardian，4–7→balanced，≥8→velocity。
 */
export function recommendStrategyPreset(keyCount: number): StrategyPresetId {
    if (keyCount <= 3) return 'guardian';
    if (keyCount <= 7) return 'balanced';
    return 'velocity';
}

/** keyCount 是否落在预设的推荐密钥数范围内（无界边为 0）。 */
export function keysInRange(preset: StrategyPreset, keyCount: number): boolean {
    if (preset.minKeys > 0 && keyCount < preset.minKeys) return false;
    if (preset.maxKeys > 0 && keyCount > preset.maxKeys) return false;
    return true;
}

/** 推荐密钥数范围的紧凑标签（纯数字符号，无需 i18n）：如 "≤ 3"、"4 – 7"、"≥ 8"、""。 */
export function keyRangeLabel(preset: StrategyPreset): string {
    const { minKeys, maxKeys } = preset;
    if (minKeys > 0 && maxKeys > 0) return `${minKeys} – ${maxKeys}`;
    if (minKeys > 0) return `≥ ${minKeys}`;
    if (maxKeys > 0) return `≤ ${maxKeys}`;
    return '';
}

/** 选定预设后要批量写入的设置（顺序即写入顺序）。 */
export function presetSettingWrites(preset: StrategyPreset): StrategyPresetSettingWrite[] {
    return preset.settings.map(({ key, value }) => ({ key, value }));
}
