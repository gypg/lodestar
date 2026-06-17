'use client';

import { useEffect, type ReactNode } from 'react';
import { NextIntlClientProvider } from 'next-intl';
import { DEFAULT_TIME_ZONE, normalizeLocale, normalizeTimeZone, useSettingStore, type Locale } from '@/stores/setting';
import { registerSettingStoreGetter } from '@/lib/utils';

import zh_hansMessages from '../../public/locale/zh_hans.json';
import zh_hantMessages from '../../public/locale/zh_hant.json';
import enMessages from '../../public/locale/en.json';

const messages: Record<Locale, typeof zh_hansMessages> = {
    'zh-Hans': zh_hansMessages,
    'zh-Hant': zh_hantMessages,
    en: enMessages,
};

export function LocaleProvider({ children }: { children: ReactNode }) {
    const { locale, timeZone } = useSettingStore();
    const currentLocale: Locale = normalizeLocale(locale);
    const currentTimeZone = normalizeTimeZone(timeZone) || DEFAULT_TIME_ZONE;

    useEffect(() => {
        registerSettingStoreGetter(() => {
            const s = useSettingStore.getState();
            return { chinaMode: s.chinaMode, exchangeRate: s.exchangeRate };
        });
    }, []);

    return (
        <NextIntlClientProvider
            locale={currentLocale}
            messages={messages[currentLocale]}
            timeZone={currentTimeZone}
        >
            {children}
        </NextIntlClientProvider>
    );
}
