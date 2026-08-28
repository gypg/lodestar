'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import {
    Sun, User, Database, RotateCcw, Zap, Wand2,
    ScrollText, Monitor, RefreshCw, ChevronsUpDown,
    Info, Sparkles, FolderX, Cloud, ShieldAlert, Eraser, Fingerprint,
    Shield, Bot,
} from 'lucide-react';
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog';
import { SettingAppearance } from './Appearance';
import { SettingAccount } from './Account';
import { SettingBackup } from './Backup';
import { SettingSystem } from './System';
import { SettingInfo } from './Info';
import { SettingLLMSync } from './LLMSync';
import { SettingLog } from './Log';
import { SettingCircuitBreaker } from './CircuitBreaker';
import { SettingRetry } from './Retry';
import { SettingAutoStrategy } from './AutoStrategy';
import { SettingStrategyPresets } from './StrategyPresets';
import { SettingSemanticCache } from './SemanticCache';
import { SettingWebDAV } from './WebDAV';
import { SettingImageBed } from './ImageBed';
import { SettingWebAuthn } from './WebAuthn';
import { SettingRouteGroupDanger } from './RouteGroupDanger';
import { SettingPurgeUnavailableModels } from './PurgeUnavailableModels';
import { SettingResponseFilter } from './ResponseFilter';
import { SettingTwoFA } from './TwoFA';
import { SettingAIRoute } from './AIRoute';
import { DEFAULT_SETTING_ORDER } from './SettingOrder';
import { useCurrentUser } from '@/api/endpoints/user';
import { hasPermission, type Permission } from '@/lib/permissions';

/**
 * `requires` is the permission a role must hold for the panel to be listed at all.
 *
 * Undefined means "self-scoped": the panel only ever touches the signed-in user's
 * own account, so every role gets it. Everything else operates the deployment and
 * must not be offered to an end customer — the backend already refuses those calls,
 * but a control that renders, takes a click and then reports "insufficient
 * permission" reads as a broken product, and it also leaks how the site is run.
 *
 * Gated on the permission rather than on a role name because `viewer` is read-only
 * STAFF and does hold settings:read: an admin||editor check would hide operational
 * panels from a role that is meant to see them.
 */
type SettingItemDef = {
    id: string;
    icon: React.ReactNode;
    titleKey: string;
    component: React.ReactNode;
    requires?: Permission;
};

const SETTING_ITEM_DEFS: SettingItemDef[] = [
    // Self-scoped: own credentials, own passkeys, own 2FA, own theme.
    { id: 'account',           icon: <User className="h-5 w-5" />,              titleKey: 'account.title',         component: <SettingAccount /> },
    { id: 'appearance',        icon: <Sun className="h-5 w-5" />,              titleKey: 'appearance.title',     component: <SettingAppearance /> },
    { id: 'twofa',           icon: <Shield className="h-5 w-5" />,            titleKey: 'twofa.title',          component: <SettingTwoFA /> },

    // Operational: relay behaviour, upstream config, backups, destructive tools.
    { id: 'info',              icon: <Info className="h-5 w-5" />,              titleKey: 'info.title',           component: <SettingInfo />, requires: 'settings:read' },
    { id: 'auto-strategy',     icon: <Sparkles className="h-5 w-5" />,         titleKey: 'autoStrategy.title',   component: <SettingAutoStrategy />, requires: 'settings:write' },
    { id: 'strategy-presets',  icon: <Wand2 className="h-5 w-5" />,            titleKey: 'strategyPresets.title', component: <SettingStrategyPresets />, requires: 'settings:write' },
    { id: 'semantic-cache',    icon: <Database className="h-5 w-5" />,          titleKey: 'semanticCache.title',  component: <SettingSemanticCache />, requires: 'settings:write' },
    { id: 'retry',             icon: <RotateCcw className="h-5 w-5" />,        titleKey: 'retry.title',          component: <SettingRetry />, requires: 'settings:write' },
    { id: 'log',               icon: <ScrollText className="h-5 w-5" />,        titleKey: 'log.title',           component: <SettingLog />, requires: 'settings:write' },
    { id: 'system',            icon: <Monitor className="h-5 w-5" />,           titleKey: 'system.title',         component: <SettingSystem />, requires: 'settings:write' },
    { id: 'llmsync',           icon: <RefreshCw className="h-5 w-5" />,        titleKey: 'llmSync.title',        component: <SettingLLMSync />, requires: 'settings:write' },
    { id: 'circuit-breaker',   icon: <Zap className="h-5 w-5" />,              titleKey: 'circuitBreaker.title', component: <SettingCircuitBreaker />, requires: 'settings:write' },
    { id: 'response-filter',   icon: <ShieldAlert className="h-5 w-5" />,      titleKey: 'responseFilter.title', component: <SettingResponseFilter />, requires: 'settings:write' },
    { id: 'image-bed',         icon: <Cloud className="h-5 w-5" />,             titleKey: 'imageBed.title',       component: <SettingImageBed />, requires: 'settings:write' },
    // Writes WebAuthnRPID/RPName/Origins -- deployment config, not the user's own
    // passkeys (those are in Account, which every role gets).
    { id: 'webauthn',          icon: <Fingerprint className="h-5 w-5" />,      titleKey: 'webauthn.title',       component: <SettingWebAuthn />, requires: 'settings:write' },
    { id: 'ai-route',          icon: <Bot className="h-5 w-5" />,               titleKey: 'aiRoute.title',        component: <SettingAIRoute />, requires: 'settings:write' },
    { id: 'backup',            icon: <Database className="h-5 w-5" />,          titleKey: 'backup.title',         component: <SettingBackup />, requires: 'settings:write' },
    { id: 'webdav',            icon: <Cloud className="h-5 w-5" />,             titleKey: 'webdav.title',         component: <SettingWebDAV />, requires: 'settings:write' },
    { id: 'purge-unavailable', icon: <Eraser className="h-5 w-5" />,           titleKey: 'purgeUnavailable.title', component: <SettingPurgeUnavailableModels />, requires: 'groups:write' },
    { id: 'route-group-danger',icon: <FolderX className="h-5 w-5" />,          titleKey: 'routeGroups.title',    component: <SettingRouteGroupDanger />, requires: 'groups:write' },
];

const SETTING_ITEM_MAP = new Map<string, SettingItemDef>(
    SETTING_ITEM_DEFS.map((def) => [def.id, def])
);

function getOrderedItems(order: string[]): SettingItemDef[] {
    const seen = new Set<string>();
    const result: SettingItemDef[] = [];
    for (const id of order) {
        const def = SETTING_ITEM_MAP.get(id);
        if (def && !seen.has(id)) {
            seen.add(id);
            result.push(def);
        }
    }
    // append any missing defaults
    for (const def of SETTING_ITEM_DEFS) {
        if (!seen.has(def.id)) {
            result.push(def);
        }
    }
    return result;
}

function loadOrder(): string[] {
    try {
        const raw = localStorage.getItem('lodestar-setting-order');
        if (!raw) return [...DEFAULT_SETTING_ORDER];
        const parsed = JSON.parse(raw);
        if (!Array.isArray(parsed)) return [...DEFAULT_SETTING_ORDER];
        const filtered = parsed.filter((id: unknown) =>
            typeof id === 'string' && SETTING_ITEM_MAP.has(id)
        );
        const missing = DEFAULT_SETTING_ORDER.filter((id) => !filtered.includes(id));
        return [...filtered, ...missing];
    } catch (e) { console.error(e);
        return [...DEFAULT_SETTING_ORDER];
    }
}

export function Setting() {
    const t = useTranslations('setting');
    const [openId, setOpenId] = useState<string | null>(null);
    const { data: currentUser } = useCurrentUser();
    // Filter before ordering: the saved order is a list of ids from localStorage and
    // may name panels this role cannot have, so filtering after would let a stale
    // order resurrect them.
    const items = getOrderedItems(loadOrder()).filter(
        (item) => item.requires === undefined || hasPermission(currentUser?.role, item.requires),
    );
    const activeItem = items.find((item) => item.id === openId);

    return (
        <div className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-xl">
            <div className="pb-3 md:pb-6 px-4 md:px-6 pt-4">
                <div className="space-y-2 max-w-2xl mx-auto">
                    {items.map((item) => (
                        <button
                            key={item.id}
                            type="button"
                            onClick={() => setOpenId(item.id)}
                            className="w-full flex items-center gap-3 rounded-xl border border-border/35 bg-card px-4 py-3.5 min-h-[3.25rem] text-left shadow-sm transition-colors hover:bg-accent/40 active:bg-accent/60"
                        >
                            <span className="shrink-0 text-muted-foreground">{item.icon}</span>
                            <span className="flex-1 text-sm font-semibold text-card-foreground truncate">
                                {t(item.titleKey)}
                            </span>
                            <ChevronsUpDown className="h-4 w-4 shrink-0 text-muted-foreground" />
                        </button>
                    ))}
                </div>
            </div>

            <Dialog open={openId !== null} onOpenChange={(open) => { if (!open) setOpenId(null); }}>
                <DialogContent aria-describedby={undefined} className="w-[100vw] sm:w-[min(95vw,720px)] lg:w-[min(95vw,1040px)] sm:max-w-[min(95vw,720px)] lg:max-w-[min(95vw,1040px)] max-h-[100dvh] sm:max-h-[90vh] overflow-y-auto pt-12 p-0 sm:p-0 gap-0 rounded-none sm:rounded-2xl">
                    <DialogTitle className="sr-only">{activeItem ? t(activeItem.titleKey) : ''}</DialogTitle>
                    {activeItem && activeItem.component}
                </DialogContent>
            </Dialog>
        </div>
    );
}
