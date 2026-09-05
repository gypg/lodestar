import { SettingKey } from '@/api/endpoints/setting';

export type AIRouteSourceMode = 'external' | 'local';

/** Wire values accepted for SettingKey.AIRouteSourceMode. Mirrors
 *  internal/model/setting.go AIRouteSourceMode* constants. */
const LOCAL: AIRouteSourceMode = 'local';
const EXTERNAL: AIRouteSourceMode = 'external';

type SettingLike = { key: string; value: string };

/**
 * Decide which source toggle position to restore from the persisted settings.
 *
 * Extracted from the component so the restore rule can be tested without a DOM:
 * the bug this guards against was the toggle resetting to "external" on every
 * mount while the model name loaded back from settings, which rendered a
 * local-mode setup as an external one.
 *
 * Anything other than the two known wire values is treated as "not chosen" and
 * falls back to external, matching the pre-existing default. The write side
 * rejects unknown values (Setting.Validate), so reaching the fallback means the
 * row predates this setting or was written out of band.
 */
export function resolveAIRouteSourceMode(
    settings: readonly SettingLike[] | undefined | null,
): AIRouteSourceMode {
    if (!settings) return EXTERNAL;
    const raw = settings.find((item) => item.key === SettingKey.AIRouteSourceMode)?.value;
    return raw === LOCAL || raw === EXTERNAL ? raw : EXTERNAL;
}
