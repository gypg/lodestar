'use client';

import { PageWrapper } from '@/components/common/PageWrapper';
import { SettingWallet } from '@/components/modules/setting/SettingWallet';

export function Wallet() {
    return (
        <PageWrapper className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-xl pb-3 md:pb-6">
            <SettingWallet />
        </PageWrapper>
    );
}
