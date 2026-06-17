import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';

export type Locale = 'zh-Hans' | 'zh-Hant' | 'en';
export const DEFAULT_TIME_ZONE = 'Asia/Shanghai';
export const DEFAULT_EXCHANGE_RATE = 7.2;

export function normalizeLocale(locale: string | null | undefined): Locale {
    switch (locale) {
        case 'zh-Hans':
        case 'zh_Hans':
        case 'zh-hans':
        case 'zh_hans':
        case 'zh-CN':
        case 'zh_CN':
            return 'zh-Hans';
        case 'zh-Hant':
        case 'zh_Hant':
        case 'zh-hant':
        case 'zh_hant':
        case 'zh-TW':
        case 'zh_TW':
        case 'zh-HK':
        case 'zh_HK':
            return 'zh-Hant';
        case 'en':
        case 'en-US':
        case 'en_US':
            return 'en';
        default:
            return 'zh-Hans';
    }
}

export function normalizeTimeZone(timeZone: string | null | undefined): string {
    if (!timeZone) {
        return DEFAULT_TIME_ZONE;
    }

    try {
        Intl.DateTimeFormat(undefined, { timeZone });
        return timeZone;
    } catch {
        return DEFAULT_TIME_ZONE;
    }
}

interface SettingState {
    locale: Locale;
    timeZone: string;
    chinaMode: boolean;
    exchangeRate: number;
    setLocale: (locale: Locale) => void;
    setTimeZone: (timeZone: string) => void;
    setChinaMode: (enabled: boolean) => void;
    setExchangeRate: (rate: number) => void;
}

function normalizeExchangeRate(rate: number | null | undefined): number {
    if (typeof rate !== 'number' || !isFinite(rate) || rate <= 0) {
        return DEFAULT_EXCHANGE_RATE;
    }
    return rate;
}

export const useSettingStore = create<SettingState>()(
    persist(
        (set) => ({
            locale: 'zh-Hans',
            timeZone: DEFAULT_TIME_ZONE,
            chinaMode: false,
            exchangeRate: DEFAULT_EXCHANGE_RATE,
            setLocale: (locale) => set({ locale: normalizeLocale(locale) }),
            setTimeZone: (timeZone) => set({ timeZone: normalizeTimeZone(timeZone) }),
            setChinaMode: (chinaMode) => set({ chinaMode }),
            setExchangeRate: (exchangeRate) => set({ exchangeRate: normalizeExchangeRate(exchangeRate) }),
        }),
        {
            name: 'octopus-settings',
            storage: createJSONStorage(() => localStorage),
            merge: (persistedState, currentState) => {
                const typed = (persistedState as Partial<SettingState> | null) ?? null;
                return {
                    ...currentState,
                    ...typed,
                    locale: normalizeLocale(typed?.locale),
                    timeZone: normalizeTimeZone(typed?.timeZone),
                    chinaMode: typed?.chinaMode ?? false,
                    exchangeRate: normalizeExchangeRate(typed?.exchangeRate),
                };
            },
        }
    )
);
