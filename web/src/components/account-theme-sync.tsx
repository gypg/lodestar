'use client';

/*
GGZERO — 账户主题同步。

登录后从账户读取保存的主题预设并应用到本地（服务端优先），让用户选的主题跨设备一致。
仅在已认证的应用外壳内挂载，因此 useUserPreferences 只在登录后请求。
保存动作发生在主题选择器（设置→外观）点击时，这里只负责"加载即应用"，避免回写环路。
*/

import { useEffect, useRef } from 'react';
import { useUserPreferences } from '@/api/endpoints/user';
import { useThemePresetStore } from '@/stores/theme-preset';

export function AccountThemeSync() {
    const { data } = useUserPreferences();
    const setPreset = useThemePresetStore((s) => s.setPreset);
    const appliedRef = useRef(false);

    useEffect(() => {
        if (appliedRef.current || !data) return;
        appliedRef.current = true;
        if (data.themePreset) {
            setPreset(data.themePreset);
        }
    }, [data, setPreset]);

    return null;
}
