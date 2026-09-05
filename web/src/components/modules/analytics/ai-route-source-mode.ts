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

/**
 * Whether local mode is holding a model it cannot actually run with.
 *
 * The readiness banner needs base_url, api_key and model together, but local
 * mode only renders a model dropdown — the other two are meant to be derived
 * from the channel. When that derivation produced nothing, the panel used to
 * show no notice and no button on a later visit, because the only signal was an
 * interaction-time flag that resets on mount. The banner then stayed up with
 * nothing on screen to act on.
 *
 * Derived from the values themselves rather than from that flag, so a setup left
 * incomplete by an earlier session is recognised on arrival.
 */
export function isLocalSetupIncomplete(state: {
    mode: AIRouteSourceMode;
    model: string;
    baseURL: string;
    apiKey: string;
    lookupInFlight: boolean;
}): boolean {
    if (state.mode !== LOCAL) return false;
    // Nothing picked yet is not "incomplete" — it is untouched, and the dropdown
    // is the obvious next move without extra noise.
    if (state.model.trim() === '') return false;
    // A lookup in flight will fill these in; complaining mid-flight would flash
    // the notice on every successful pick.
    if (state.lookupInFlight) return false;
    return state.baseURL.trim() === '' || state.apiKey.trim() === '';
}
