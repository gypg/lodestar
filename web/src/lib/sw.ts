export const SW_MESSAGE_TYPE = {
    SKIP_WAITING: 'SKIP_WAITING',
    CLEAR_CACHE: 'CLEAR_CACHE',
    CACHE_CLEARED: 'CACHE_CLEARED',
} as const;

export type SwMessageType = (typeof SW_MESSAGE_TYPE)[keyof typeof SW_MESSAGE_TYPE];

// Keep in sync with `web/public/sw.js`
export const LODESTAR_CACHE_PREFIX = 'lodestar-';
export const LODESTAR_CACHE_VERSION = 'v3';
// Font cache is version-independent and should persist across updates
export const LODESTAR_FONT_CACHE_NAME = 'lodestar-font';

export function isLodestarCacheName(name: string) {
    return name.startsWith(LODESTAR_CACHE_PREFIX);
}

export function isFontCacheName(name: string) {
    return name === LODESTAR_FONT_CACHE_NAME;
}


