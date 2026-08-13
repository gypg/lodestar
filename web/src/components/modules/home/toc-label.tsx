'use client';

/*
Lodestar — 落地页章节目录的标签解析。

两个落地页（winter / pretext）共用同一份目录条目。目录表只存导航 id 与栏目缩写，
标签在这里用**字面量 key** 取：i18n 门只检查「t 由 useTranslations() 在同一/外层
作用域绑定 + key 是字面量」的调用，把 key 存进表或把 t 传进 helper 都会让门看不见。
所以这是一个自己调 useTranslations() 的组件，而不是接收 t 的纯函数。
*/

import { useTranslations } from 'next-intl';
import type { NavItem } from '@/components/modules/navbar';

/** 落地页目录里出现的导航项（HOME_TOC 的 id 全集）。 */
export type HomeTocId = Extract<
    NavItem,
    'hub' | 'channel' | 'group' | 'model' | 'analytics' | 'log' | 'alert' | 'ops' | 'apikey' | 'setting' | 'user'
>;

export function HomeTocLabel({ id }: { id: HomeTocId }) {
    const t = useTranslations('landing.toc');

    switch (id) {
        case 'hub':
            return <>{t('hub')}</>;
        case 'channel':
            return <>{t('channel')}</>;
        case 'group':
            return <>{t('group')}</>;
        case 'model':
            return <>{t('model')}</>;
        case 'analytics':
            return <>{t('analytics')}</>;
        case 'log':
            return <>{t('log')}</>;
        case 'alert':
            return <>{t('alert')}</>;
        case 'ops':
            return <>{t('ops')}</>;
        case 'apikey':
            return <>{t('apikey')}</>;
        case 'setting':
            return <>{t('setting')}</>;
        case 'user':
            return <>{t('user')}</>;
    }
}
