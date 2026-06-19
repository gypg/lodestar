'use client';

import { usePublicOverview } from '@/api/endpoints/public';
import { WinterLanding } from './winter-landing';
import { NewspaperLanding } from './newspaper-landing';

/** Picks winter vs newspaper cover from public overview `landing_layout`. */
export function LandingShell({
    variant,
    onEnterDashboard,
    onLogin,
}: {
    variant: 'home' | 'public';
    onEnterDashboard?: () => void;
    onLogin?: () => void;
}) {
    const { data: overview } = usePublicOverview(true);
    const layout = overview?.landing_layout === 'newspaper' ? 'newspaper' : 'winter';
    if (layout === 'newspaper') {
        return <NewspaperLanding variant={variant} onEnterDashboard={onEnterDashboard} onLogin={onLogin} />;
    }
    return <WinterLanding variant={variant} onEnterDashboard={onEnterDashboard} onLogin={onLogin} />;
}