import type { BaseUrl } from '@/api/endpoints/channel';

export type BaseUrlSuffixMode = NonNullable<BaseUrl['suffix_mode']>;

export function getEffectiveBaseUrlSuffixMode(suffixMode?: BaseUrl['suffix_mode']): BaseUrlSuffixMode {
    return suffixMode === 'custom' ? 'custom' : 'openai_compat';
}

export function isOpenAICompatBaseUrlSuffixMode(suffixMode?: BaseUrl['suffix_mode']): boolean {
    return getEffectiveBaseUrlSuffixMode(suffixMode) === 'openai_compat';
}
