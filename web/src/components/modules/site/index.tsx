"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type DragEvent,
} from "react";
import { useTranslations } from "next-intl";
import { AnimatePresence, motion } from "motion/react";
import {
  Site as SiteRecord,
  SiteAccount,
  SiteCredentialType,
  SitePlatform,
  useCheckinAllSites,
  useCheckinSiteAccount,
  useArchiveSite,
  useArchivedSiteList,
  useDeleteSite,
  useDeleteSiteAccount,
  useEnableSite,
  useEnableSiteAccount,
  useImportAllAPIHub,
  useImportMetAPI,
  useRestoreSite,
  useSiteBatchAction,
  useSiteList,
  useSyncAllSites,
  useSyncSiteAccount,
  useUpdateSite,
} from "@/api/endpoints/site";
import { PageWrapper } from "@/components/common/PageWrapper";
import { toast } from "@/components/common/Toast";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/animate-ui/components/animate/tooltip";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import {
  useSearchStore,
  useToolbarViewOptionsStore,
} from "@/components/modules/toolbar";
import { cn } from "@/lib/utils";
import { useSettingStore } from "@/stores/setting";
import { CheckinPanel } from "./CheckinPanel";
import { SiteEditDialog } from "./SiteEditDialog";
import { AccountEditDialog } from "./AccountEditDialog";
import {
  accountHasCheckinEnabled,
  accountMatchesCheckinFilters,
  deriveCheckinStatus,
  sitePlatformSupportsCheckin,
  type CheckinFilterStatus,
} from "./checkin-status";
import { translateSiteMessage } from "./site-message";
import { nextSiteCardHeights } from "./card-measure";
import { useSiteUIStore } from "./ui-store";
import { SettingSiteAutomation } from "@/components/modules/setting/SiteAutomation";
import {
  isSiteJumpTarget,
  type PendingJump,
  type SiteJumpTarget,
  useJumpStore,
} from "@/stores/jump";
import {
  CalendarCheck2,
  CheckSquare,
  ChevronDown,
  CircleAlert,
  FileJson,
  FilterX,
  Link2,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Power,
  Plus,
  RefreshCw,
  Settings,
  Square,
  Archive,
  ArchiveRestore,
  Trash2,
  TriangleAlert,
  Upload,
  Waypoints,
  X,
} from "lucide-react";

const PLATFORM_LABELS: Record<SitePlatform, string> = {
  [SitePlatform.NewAPI]: "New API",
  [SitePlatform.AnyRouter]: "AnyRouter",
  [SitePlatform.OneAPI]: "One API",
  [SitePlatform.OneHub]: "One Hub",
  [SitePlatform.DoneHub]: "Done Hub",
  [SitePlatform.Sub2API]: "Sub2API",
  [SitePlatform.OpenAI]: "OpenAI",
  [SitePlatform.Claude]: "Claude",
  [SitePlatform.Gemini]: "Gemini",
  [SitePlatform.SAPI]: "SAPI",
};

// Access Token / API Key 是专有名词不翻译；用户名 / 密码需要走 t()，故不放这张表。
const CREDENTIAL_LABELS: Partial<Record<SiteCredentialType, string>> = {
  [SiteCredentialType.AccessToken]: "Access Token",
  [SiteCredentialType.APIKey]: "API Key",
};

type HealthTone = "default" | "danger" | "muted" | "warning";

// 健康状态只描述"是哪一种 + 相关数量"，文案在渲染处用 t() 取。
// 不在这里存 key，也不把 t 传进来——两种写法都能让 i18n 门全绿却不做检查。
type HealthKind =
  | "siteDisabled"
  | "failed"
  | "disabled"
  | "partial"
  | "unconfigured"
  | "idle"
  | "ok";

type SiteSummary = {
  accountCount: number;
  keyCount: number;
  modelCount: number;
  groupCount: number;
  balance: number;
  todayIncome: number;
  failedAccountCount: number;
  partialAccountCount: number;
  disabledAccountCount: number;
  enabledAccountCount: number;
  healthKind: HealthKind;
  healthCount: number;
  healthTone: HealthTone;
};

type VisibleSite = {
  site: SiteRecord;
  summary: SiteSummary;
  visibleAccounts: SiteAccount[];
  forceExpanded: boolean;
  hasFilteredAccounts: boolean;
};

const MENU_BUTTON_CLASS =
  "flex w-full items-center gap-2 rounded-xl px-3 py-2 text-sm text-left transition-colors hover:bg-muted/60";

type SitePendingJump = PendingJump & { target: SiteJumpTarget };
type ImportSource = "all-api-hub" | "metapi";
type SiteImportResult = {
  created_sites: number;
  reused_sites: number;
  created_accounts: number;
  updated_accounts: number;
  skipped_accounts: number;
  scheduled_sync_accounts?: number;
  warnings: string[];
  imported_tokens?: number;
  imported_groups?: number;
  imported_models?: number;
  disabled_models?: number;
};

// 返回 null 表示"没有可显示的时间"，由调用点决定用哪条文案。
function formatDateTime(value?: string | null) {
  if (!value) return null;
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) {
    return null;
  }
  return date.toLocaleString();
}

// 归一到 locale 里 site.executionStatus.* 的分支名，文案在渲染处取。
function statusLabelKind(status: string) {
  switch (status) {
    case "partial":
    case "success":
    case "failed":
    case "skipped":
      return status;
    case "idle":
    default:
      return "idle";
  }
}

function SiteMetric({
  label,
  value,
}: {
  label: string;
  value: number | string;
}) {
  return (
    <div className="rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg font-semibold">{value}</div>
    </div>
  );
}

// 取不到上游 message 时返回 null，兜底文案由调用点用 t() 提供。
function getErrorMessage(error: unknown) {
  if (error instanceof Error) return error.message;
  if (typeof error === "object" && error !== null && "message" in error) {
    const message = (error as { message?: unknown }).message;
    if (typeof message === "string") return message;
  }
  return null;
}

function formatBalance(value: number) {
  if (value === 0) return "0";
  if (value >= 1000000) return `${(value / 1000000).toFixed(2)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(2)}K`;
  return value.toFixed(2);
}

function normalizeSearchTerm(value: string) {
  return value.trim().toLowerCase();
}

function matchesSearch(value: string | null | undefined, query: string) {
  return (value ?? "").toLowerCase().includes(query);
}

function normalizedStatus(status?: string | null) {
  return status || "idle";
}

function accountHasSyncFailure(account: SiteAccount) {
  return normalizedStatus(account.last_sync_status) === "failed";
}

function accountHasCheckinFailure(
  site: SiteRecord,
  account: SiteAccount,
) {
  return deriveCheckinStatus(site, account) === "failed";
}

function accountHasHealthFailure(
  site: SiteRecord,
  account: SiteAccount,
) {
  return accountHasSyncFailure(account) || accountHasCheckinFailure(site, account);
}

function statusDotClass(status: string) {
  switch (status) {
    case "success":
      return "bg-emerald-500";
    case "partial":
      return "bg-amber-500";
    case "failed":
      return "bg-destructive";
    case "skipped":
      return "bg-amber-500";
    default:
      return "bg-muted-foreground/40";
  }
}

function badgeToneClass(tone: HealthTone) {
  switch (tone) {
    case "danger":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "muted":
      return "border-border bg-muted/40 text-muted-foreground";
    case "warning":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "default":
    default:
      return "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
  }
}

function cardToneClass(tone: HealthTone) {
  switch (tone) {
    case "danger":
      return "border-destructive/25 bg-gradient-to-br from-destructive/[0.07] via-card to-card";
    case "muted":
      return "border-slate-400/25 bg-gradient-to-br from-slate-500/[0.06] via-card to-card dark:border-slate-600/35";
    case "warning":
      return "border-amber-500/25 bg-gradient-to-br from-amber-500/[0.07] via-card to-card";
    case "default":
    default:
      return "border-border/70 bg-card";
  }
}

function buildSiteSummary(site: SiteRecord): SiteSummary {
  let keyCount = 0;
  let modelCount = 0;
  let groupCount = 0;
  let balance = 0;
  let todayIncome = 0;
  let failedAccountCount = 0;
  let partialAccountCount = 0;
  let disabledAccountCount = 0;
  let enabledAccountCount = 0;

  for (const account of site.accounts) {
    keyCount += account.tokens.length;
    modelCount += account.models.length;
    groupCount += account.user_groups.length;
    balance += account.balance;
    todayIncome +=
      typeof account.today_income === "number" ? account.today_income : 0;

    if (account.enabled) enabledAccountCount += 1;
    else disabledAccountCount += 1;

    if (accountHasHealthFailure(site, account)) {
      failedAccountCount += 1;
    } else if (normalizedStatus(account.last_sync_status) === "partial") {
      partialAccountCount += 1;
    }
  }

  if (!site.enabled) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthKind: "siteDisabled",
      healthCount: 0,
      healthTone: "muted",
    };
  }

  if (failedAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthKind: "failed",
      healthCount: failedAccountCount,
      healthTone: "danger",
    };
  }

  if (disabledAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthKind: "disabled",
      healthCount: disabledAccountCount,
      healthTone: "muted",
    };
  }

  if (partialAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthKind: "partial",
      healthCount: partialAccountCount,
      healthTone: "warning",
    };
  }

  if (site.accounts.length === 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthKind: "unconfigured",
      healthCount: 0,
      healthTone: "warning",
    };
  }

  const allIdle = site.accounts.every(
    (account) =>
      account.enabled &&
      normalizedStatus(account.last_sync_status) === "idle" &&
      (!accountHasCheckinEnabled(account, site.platform) ||
        deriveCheckinStatus(site, account) === "idle"),
  );

  return {
    accountCount: site.accounts.length,
    keyCount,
    modelCount,
    groupCount,
    balance,
    todayIncome,
    failedAccountCount,
    partialAccountCount,
    disabledAccountCount,
    enabledAccountCount,
    healthKind: allIdle ? "idle" : "ok",
    healthCount: 0,
    healthTone: allIdle ? "warning" : "default",
  };
}

function CompactMetric({
  label,
  value,
}: {
  label: string;
  value: number | string;
}) {
  return (
    <span className="inline-flex items-baseline gap-1 text-xs text-muted-foreground">
      <span>{label}</span>
      <span className="font-semibold text-foreground">{value}</span>
    </span>
  );
}

function isCloudflareProtectionMessage(message?: string | null) {
  const lowered = (message ?? "").toLowerCase();
  return lowered.includes("cloudflare") || message?.includes("Cloudflare 保护") === true;
}

function ExecutionSummary({
  kind,
  status,
  at,
  message,
}: {
  kind: "sync" | "checkin";
  status: string;
  at?: string | null;
  message?: string | null;
}) {
  const t = useTranslations();
  const at_ = formatDateTime(at);
  const statusKind = statusLabelKind(status);
  const text = [
    kind === "sync"
      ? at_
        ? t("site.execution.lastSyncAt", { at: at_ })
        : t("site.execution.lastSyncNever")
      : at_
        ? t("site.execution.lastCheckinAt", { at: at_ })
        : t("site.execution.lastCheckinNever"),
    statusKind === "partial"
      ? t("site.executionStatus.partial")
      : statusKind === "success"
        ? t("site.executionStatus.success")
        : statusKind === "failed"
          ? t("site.executionStatus.failed")
          : statusKind === "skipped"
            ? t("site.executionStatus.skipped")
            : t("site.executionStatus.idle"),
  ];
  if (message) {
    text.push(message);
  }

  const cloudflareProtected = isCloudflareProtectionMessage(message);
  const summary = text.join(" · ");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-start gap-2 text-xs text-muted-foreground">
          <span
            className={cn(
              "mt-1 size-2 shrink-0 rounded-full",
              cloudflareProtected ? "bg-amber-500" : statusDotClass(status),
            )}
          />
          <span className="min-w-0 truncate">
            {cloudflareProtected ? `${t("site.cloudflareProtected")} · ` : ""}
            {summary}
          </span>
        </div>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">{summary}</TooltipContent>
    </Tooltip>
  );
}

function StaticSummary({
  tone = "muted",
  text,
}: {
  tone?: "muted" | "warning";
  text: string;
}) {
  return (
    <div
      className={cn(
        "flex items-start gap-2 text-xs",
        tone === "warning" ? "text-amber-700 dark:text-amber-300" : "text-muted-foreground",
      )}
    >
      <span
        className={cn(
          "mt-1 size-2 shrink-0 rounded-full",
          tone === "warning" ? "bg-amber-500" : "bg-muted-foreground/40",
        )}
      />
      <span className="min-w-0 truncate">{text}</span>
    </div>
  );
}

function IconActionButton({
  label,
  className,
  ...props
}: ComponentProps<typeof Button> & { label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          size="icon-sm"
          variant="outline"
          className={cn("rounded-xl", className)}
          aria-label={label}
          title={label}
          {...props}
        />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function estimateVisibleSiteCardHeight(item: VisibleSite, expanded: boolean) {
  if (item.forceExpanded || expanded) {
    return 360 + item.visibleAccounts.length * 190;
  }
  if (item.site.accounts.length === 0) {
    return 280;
  }
  return 310;
}

export function Site() {
  const t = useTranslations();
  const tProxy = useTranslations('proxyPool');
  const locale = useSettingStore((state) => state.locale);

  // 在组件内闭合 t/locale，而不是把 t 当参数传进模块级 helper。
  const siteErrorMessage = useCallback(
    (error: unknown) =>
      translateSiteMessage(locale, getErrorMessage(error), t) ||
      t("site.toast.actionFailed"),
    [locale, t],
  );

  const { data: sites, isLoading, error } = useSiteList();
  const updateSite = useUpdateSite();
  const enableSite = useEnableSite();
  const deleteSite = useDeleteSite();
  const archiveSite = useArchiveSite();
  const restoreSite = useRestoreSite();
  const enableSiteAccount = useEnableSiteAccount();
  const deleteSiteAccount = useDeleteSiteAccount();
  const syncSiteAccount = useSyncSiteAccount();
  const checkinSiteAccount = useCheckinSiteAccount();
  const syncAllSites = useSyncAllSites();
  const checkinAllSites = useCheckinAllSites();
  const importAllAPIHub = useImportAllAPIHub();
  const importMetAPI = useImportMetAPI();
  const batchAction = useSiteBatchAction();

  const [siteDialogOpen, setSiteDialogOpen] = useState(false);
  const [importDialogOpen, setImportDialogOpen] = useState(false);
  const [archivedDialogOpen, setArchivedDialogOpen] = useState(false);
  const {
    data: archivedSites,
    isLoading: archivedLoading,
    error: archivedError,
  } = useArchivedSiteList(archivedDialogOpen);
  const [importPayloadText, setImportPayloadText] = useState("");
  const [importFile, setImportFile] = useState<File | null>(null);
  const importFileInputRef = useRef<HTMLInputElement | null>(null);
  const importDragDepthRef = useRef(0);
  const [isImportDragging, setIsImportDragging] = useState(false);
  const [importSource, setImportSource] =
    useState<ImportSource>("all-api-hub");
  const [lastImportResult, setLastImportResult] =
    useState<SiteImportResult | null>(null);
  const [editingSite, setEditingSite] = useState<SiteRecord | null>(null);

  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [accountSite, setAccountSite] = useState<SiteRecord | null>(null);
  const [editingAccount, setEditingAccount] = useState<SiteAccount | null>(
    null,
  );

  // Batch selection
  const [selectedSiteIds, setSelectedSiteIds] = useState<number[]>([]);
  const [batchMode, setBatchMode] = useState(false);

  // Delete confirmation
  const [deleteConfirm, setDeleteConfirm] = useState<{
    type: "site" | "account" | "archive-site";
    id: number;
    name: string;
  } | null>(null);
  const [expandedSiteIds, setExpandedSiteIds] = useState<Set<number>>(
    () => new Set(),
  );
  const [syncingAccountIds, setSyncingAccountIds] = useState<Set<number>>(
    () => new Set(),
  );
  const [checkinAccountIds, setCheckinAccountIds] = useState<Set<number>>(
    () => new Set(),
  );
  const [siteCardHeights, setSiteCardHeights] = useState<Record<number, number>>(
    {},
  );
  const [statusDayKey, setStatusDayKey] = useState(() => {
    const now = new Date();
    return `${now.getFullYear()}-${now.getMonth()}-${now.getDate()}`;
  });
  const cardObserversRef = useRef<Map<number, ResizeObserver>>(new Map());
  const cardElementsRef = useRef<Map<number, HTMLElement>>(new Map());
  const accountElementsRef = useRef<Map<number, HTMLElement>>(new Map());
  const [highlightedSiteId, setHighlightedSiteId] = useState<number | null>(
    null,
  );
  const [highlightedAccountId, setHighlightedAccountId] = useState<number | null>(
    null,
  );

  const searchTerm = useSearchStore((state) => state.getSearchTerm("site"));
  const setSearchTerm = useSearchStore((state) => state.setSearchTerm);
  const siteSortField = useToolbarViewOptionsStore((state) =>
    state.getSortField("site"),
  );
  const siteSortOrder = useToolbarViewOptionsStore((state) =>
    state.getSortOrder("site"),
  );
  const checkinFilterStatuses = useSiteUIStore(
    (state) => state.checkinFilterStatuses,
  );
  const setCheckinFilterStatuses = useSiteUIStore(
    (state) => state.setCheckinFilterStatuses,
  );
  const setSiteHandlers = useSiteUIStore((state) => state.setHandlers);
  const resetSiteHandlers = useSiteUIStore((state) => state.resetHandlers);
  const pendingJump = useJumpStore((state) => state.pending);
  const clearPendingJump = useJumpStore((state) => state.clearPending);
  const requestJump = useJumpStore((state) => state.requestJump);

  const pendingSiteJump =
    pendingJump && isSiteJumpTarget(pendingJump.target)
      ? (pendingJump as SitePendingJump)
      : null;
  const forcedSiteId = pendingSiteJump?.target.siteId ?? null;

  // 高度测量只服务于桌面双列 masonry 的分栏估算。
  //
  // 两个坑（React error #185 的成因，别回退）：
  // 1. ref 回调必须是每个 siteID 稳定的同一个函数引用。内联 `ref={(node) => ...}`
  //    每次渲染都是新函数，React 会先用 null 卸载再重新挂载，于是每渲染一次就重测
  //    一次并可能 setState，构成 measure → render → measure 死循环。
  // 2. 移动端(`md:hidden`)与桌面(`hidden md:grid`)两套 DOM 同时挂载，隐藏那套
  //    测出来恒为 0。若两者写同一个 siteID，会在 0 与真实高度之间反复互相覆盖。
  //    故只有桌面列表注册测量，且 0 高度（display:none）一律不写入。
  const measureRefsRef = useRef<Map<number, (node: HTMLElement | null) => void>>(
    new Map(),
  );

  const applySiteCardHeight = useCallback((siteID: number, rawHeight: number) => {
    // 0/负/非有限高度丢弃 + 同高返回同一引用，两条不变量都在 card-measure.ts
    // 里有测试守卫（node:test 无法渲染 React，故抽成纯函数）。
    setSiteCardHeights((current) =>
      nextSiteCardHeights(current, siteID, rawHeight),
    );
  }, []);

  const getSiteCardMeasureRef = useCallback(
    (siteID: number) => {
      const cache = measureRefsRef.current;
      const cached = cache.get(siteID);
      if (cached) return cached;

      const ref = (node: HTMLElement | null) => {
        const observers = cardObserversRef.current;
        const elements = cardElementsRef.current;
        const currentNode = elements.get(siteID);

        if (currentNode === node) {
          return;
        }

        if (currentNode) {
          observers.get(siteID)?.disconnect();
          observers.delete(siteID);
          elements.delete(siteID);
        }

        if (!node) {
          return;
        }

        elements.set(siteID, node);
        const observer = new ResizeObserver((entries) => {
          applySiteCardHeight(
            siteID,
            entries[0]?.contentRect.height ?? node.getBoundingClientRect().height,
          );
        });
        observer.observe(node);
        observers.set(siteID, observer);

        applySiteCardHeight(siteID, node.getBoundingClientRect().height);
      };

      cache.set(siteID, ref);
      return ref;
    },
    [applySiteCardHeight],
  );

  const setAccountElementRef = useCallback(
    (accountId: number, node: HTMLElement | null) => {
      const elements = accountElementsRef.current;
      if (node) {
        elements.set(accountId, node);
        return;
      }
      elements.delete(accountId);
    },
    [],
  );

  const flashTarget = useCallback(
    (target: "site" | "account", id: number) => {
      if (target === "site") {
        setHighlightedSiteId(id);
        window.setTimeout(() => {
          setHighlightedSiteId((current) => (current === id ? null : current));
        }, 1800);
        return;
      }

      setHighlightedAccountId(id);
      window.setTimeout(() => {
        setHighlightedAccountId((current) => (current === id ? null : current));
      }, 1800);
    },
    [],
  );

  const inventory = useMemo(() => {
    let totalBalance = 0;
    let totalBalanceUsed = 0;
    let enabledAccounts = 0;
    let totalAccounts = 0;

    for (const site of sites ?? []) {
      for (const account of site.accounts) {
        totalAccounts += 1;
        if (site.enabled && account.enabled) {
          enabledAccounts += 1;
        }
        totalBalance += typeof account.balance === "number" ? account.balance : 0;
        totalBalanceUsed +=
          typeof account.balance_used === "number" ? account.balance_used : 0;
      }
    }

    return {
      totalBalance,
      totalBalanceUsed,
      enabledAccounts,
      totalAccounts,
    };
  }, [sites]);

  const normalizedQuery = useMemo(
    () => normalizeSearchTerm(searchTerm),
    [searchTerm],
  );

  const visibleSites = useMemo<VisibleSite[]>(() => {
    const hasSearch = normalizedQuery.length > 0;

    const list = (sites ?? []).flatMap((site) => {
      const summary = buildSiteSummary(site);
      const isForcedTarget = forcedSiteId === site.id;

      const hasCheckinFilters = checkinFilterStatuses.length > 0;

      const siteMatchesQuery =
        !hasSearch ||
        matchesSearch(site.name, normalizedQuery) ||
        matchesSearch(site.base_url, normalizedQuery) ||
        matchesSearch(PLATFORM_LABELS[site.platform], normalizedQuery);

      const accountMatchesQuery = (account: SiteAccount) =>
        matchesSearch(account.name, normalizedQuery);

      const matchedAccountsBySearch = hasSearch
        ? site.accounts.filter(accountMatchesQuery)
        : site.accounts;

      let visibleAccounts = site.accounts;
      let forceExpanded = hasCheckinFilters || isForcedTarget;

      if (hasCheckinFilters && !isForcedTarget) {
        visibleAccounts = visibleAccounts.filter((account) =>
          accountMatchesCheckinFilters(site, account, checkinFilterStatuses),
        );
      }

      if (hasSearch && !siteMatchesQuery && !isForcedTarget) {
        visibleAccounts = visibleAccounts.filter(accountMatchesQuery);
        forceExpanded = visibleAccounts.length > 0 || forceExpanded;
      }

      if (isForcedTarget) {
        visibleAccounts = site.accounts;
      }

      const visible =
        isForcedTarget
          ? true
          : hasCheckinFilters
            ? visibleAccounts.length > 0
            : !hasSearch || siteMatchesQuery || matchedAccountsBySearch.length > 0;

      if (!visible) {
        return [];
      }

      return [
        {
          site,
          summary,
          visibleAccounts,
          forceExpanded,
          hasFilteredAccounts: visibleAccounts.length !== site.accounts.length,
        },
      ];
    });

    if (siteSortField === "default") {
      return list;
    }

    return [...list].sort((a, b) => {
      if (a.site.is_pinned !== b.site.is_pinned) {
        return a.site.is_pinned ? -1 : 1;
      }

      let diff = 0;
      if (siteSortField === "balance") {
        diff = a.summary.balance - b.summary.balance;
      } else {
        diff = a.site.name.localeCompare(b.site.name);
      }

      if (diff !== 0) {
        return siteSortOrder === "asc" ? diff : -diff;
      }

      return a.site.sort_order - b.site.sort_order || a.site.id - b.site.id;
    });
  }, [
    sites,
    normalizedQuery,
    checkinFilterStatuses,
    forcedSiteId,
    siteSortField,
    siteSortOrder,
  ]);

  const hasActiveFilters =
    normalizedQuery.length > 0 || checkinFilterStatuses.length > 0;
  const visibleAccountCount = visibleSites.reduce(
    (sum, item) => sum + item.visibleAccounts.length,
    0,
  );

  function openCreateSiteDialog() {
    setEditingSite(null);
    setSiteDialogOpen(true);
  }

  function openEditSiteDialog(site: SiteRecord) {
    setEditingSite(site);
    setSiteDialogOpen(true);
  }

  function closeSiteDialog(open: boolean) {
    setSiteDialogOpen(open);
    if (!open) {
      setEditingSite(null);
    }
  }

  function openCreateAccountDialog(site: SiteRecord) {
    setAccountSite(site);
    setEditingAccount(null);
    setAccountDialogOpen(true);
  }

  function openEditAccountDialog(site: SiteRecord, account: SiteAccount) {
    setAccountSite(site);
    setEditingAccount(account);
    setAccountDialogOpen(true);
  }

  function closeAccountDialog(open: boolean) {
    setAccountDialogOpen(open);
    if (!open) {
      setAccountSite(null);
      setEditingAccount(null);
    }
  }

  async function handleToggleSite(site: SiteRecord) {
    try {
      await enableSite.mutateAsync({ id: site.id, enabled: !site.enabled });
      toast.success(
        site.enabled ? t("site.toast.siteDisabled") : t("site.toast.siteEnabled"),
      );
    } catch (toggleError) {
      toast.error(siteErrorMessage(toggleError));
    }
  }

  async function handleDeleteSite(site: SiteRecord) {
    setDeleteConfirm({ type: "site", id: site.id, name: site.name });
  }

  async function handleArchiveSite(site: SiteRecord) {
    setDeleteConfirm({ type: "archive-site", id: site.id, name: site.name });
  }

  async function handleRestoreSite(siteId: number, siteName: string) {
    try {
      await restoreSite.mutateAsync(siteId);
      toast.success(t("site.toast.siteRestored", { name: siteName }));
    } catch (err) {
      toast.error(siteErrorMessage(err));
    }
  }

  async function handleToggleAccount(account: SiteAccount) {
    try {
      await enableSiteAccount.mutateAsync({
        id: account.id,
        enabled: !account.enabled,
      });
      toast.success(
        account.enabled
          ? t("site.toast.accountDisabled")
          : t("site.toast.accountEnabled"),
      );
    } catch (toggleError) {
      toast.error(siteErrorMessage(toggleError));
    }
  }

  async function handleDeleteAccount(account: SiteAccount) {
    setDeleteConfirm({ type: "account", id: account.id, name: account.name });
  }

  async function handleSyncAccount(account: SiteAccount) {
    setSyncingAccountIds((current) => new Set(current).add(account.id));
    try {
      const result = await syncSiteAccount.mutateAsync(account.id);
      const summary = t("site.toast.syncAccountSummary", {
        message: result.message,
        groups: result.group_count,
        keys: result.token_count,
        models: result.model_count,
      });
      if (result.status === "partial") {
        toast.warning(summary);
      } else {
        toast.success(summary);
      }
    } catch (syncError) {
      toast.error(siteErrorMessage(syncError));
    } finally {
      setSyncingAccountIds((current) => {
        const next = new Set(current);
        next.delete(account.id);
        return next;
      });
    }
  }

  async function handleCheckinAccount(account: SiteAccount) {
    setCheckinAccountIds((current) => new Set(current).add(account.id));
    try {
      const result = await checkinSiteAccount.mutateAsync(account.id);
      const checkinKind = statusLabelKind(result.status);
      const statusText =
        checkinKind === "partial"
          ? t("site.executionStatus.partial")
          : checkinKind === "success"
            ? t("site.executionStatus.success")
            : checkinKind === "failed"
              ? t("site.executionStatus.failed")
              : checkinKind === "skipped"
                ? t("site.executionStatus.skipped")
                : t("site.executionStatus.idle");
      const message = result.reward
        ? t("site.toast.checkinWithReward", {
            status: statusText,
            message: result.message,
            reward: result.reward,
          })
        : t("site.toast.checkinResult", {
            status: statusText,
            message: result.message,
          });
      if (result.status === "failed") {
        toast.error(message);
      } else {
        toast.success(message);
      }
    } catch (checkinError) {
      toast.error(siteErrorMessage(checkinError));
    } finally {
      setCheckinAccountIds((current) => {
        const next = new Set(current);
        next.delete(account.id);
        return next;
      });
    }
  }

  async function handleImportSites() {
    const hasFile = !!importFile;
    const hasText = !!importPayloadText.trim();
    if (!hasFile && !hasText) {
      toast.error(t("site.toast.importInputRequired"));
      return;
    }

    try {
      const payload = {
        file: importFile,
        text: importPayloadText,
      };
      const result =
        importSource === "metapi"
          ? await importMetAPI.mutateAsync(payload)
          : await importAllAPIHub.mutateAsync(payload);
      setLastImportResult(result);
      setImportFile(null);
      setImportPayloadText("");
      toast.success(
        t("site.toast.importDone", {
          createdSites: result.created_sites,
          createdAccounts: result.created_accounts,
          updatedAccounts: result.updated_accounts,
        }),
      );
    } catch (importError) {
      toast.error(siteErrorMessage(importError));
    }
  }

  function setSelectedImportFile(file: File | null) {
    setImportFile(file);
    setLastImportResult(null);
    setIsImportDragging(false);
    importDragDepthRef.current = 0;
    if (!file && importFileInputRef.current) {
      importFileInputRef.current.value = "";
    }
  }

  function isImportFileDrag(event: DragEvent<HTMLDivElement>) {
    return Array.from(event.dataTransfer.types).includes("Files");
  }

  function handleImportDragEnter(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    importDragDepthRef.current += 1;
    setIsImportDragging(true);
  }

  function handleImportDragLeave(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    importDragDepthRef.current = Math.max(0, importDragDepthRef.current - 1);
    if (importDragDepthRef.current === 0) {
      setIsImportDragging(false);
    }
  }

  function handleImportDragOver(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
  }

  function handleImportDrop(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    setSelectedImportFile(event.dataTransfer.files?.[0] ?? null);
  }

  async function confirmDelete() {
    if (!deleteConfirm) return;
    try {
      if (deleteConfirm.type === "site") {
        await deleteSite.mutateAsync(deleteConfirm.id);
        toast.success(t("site.toast.siteDeleted"));
        setSelectedSiteIds((prev) =>
          prev.filter((id) => id !== deleteConfirm.id),
        );
        setExpandedSiteIds((current) => {
          const next = new Set(current);
          next.delete(deleteConfirm.id);
          return next;
        });
      } else if (deleteConfirm.type === "archive-site") {
        await archiveSite.mutateAsync(deleteConfirm.id);
        toast.success(t("site.toast.siteArchived"));
        setSelectedSiteIds((prev) =>
          prev.filter((id) => id !== deleteConfirm.id),
        );
        setExpandedSiteIds((current) => {
          const next = new Set(current);
          next.delete(deleteConfirm.id);
          return next;
        });
      } else {
        await deleteSiteAccount.mutateAsync(deleteConfirm.id);
        toast.success(t("site.toast.accountDeleted"));
      }
    } catch (deleteError) {
      toast.error(siteErrorMessage(deleteError));
    }
    setDeleteConfirm(null);
  }

  function toggleSiteSelection(siteId: number) {
    setSelectedSiteIds((prev) =>
      prev.includes(siteId)
        ? prev.filter((id) => id !== siteId)
        : [...prev, siteId],
    );
  }

  async function handleBatchAction(action: string) {
    if (selectedSiteIds.length === 0) {
      toast.error(t("site.toast.selectSiteFirst"));
      return;
    }
    try {
      const result = await batchAction.mutateAsync({
        ids: selectedSiteIds,
        action,
      });
      const successCount = result.success_ids.length;
      const failedCount = result.failed_items.length;
      toast.success(
        t("site.toast.batchDone", {
          success: successCount,
          failed: failedCount,
        }),
      );
      if (action === "delete") {
        setSelectedSiteIds([]);
      }
    } catch (batchError) {
      toast.error(siteErrorMessage(batchError));
    }
  }

  async function handleTogglePin(site: SiteRecord) {
    try {
      await updateSite.mutateAsync({ id: site.id, is_pinned: !site.is_pinned });
      toast.success(
        site.is_pinned ? t("site.toast.unpinned") : t("site.toast.pinned"),
      );
    } catch (pinError) {
      toast.error(siteErrorMessage(pinError));
    }
  }

  function handleCheckinFilterChange(status: CheckinFilterStatus) {
    if (status === "all") {
      setCheckinFilterStatuses([]);
      return;
    }

    setCheckinFilterStatuses((current) =>
      current.includes(status)
        ? current.filter((item) => item !== status)
        : [...current, status],
    );
  }

  function clearFilters() {
    setSearchTerm("site", "");
    setCheckinFilterStatuses([]);
  }

  function jumpToSiteChannel(siteId: number) {
    requestJump({ kind: "site-channel-card", siteId });
  }

  function jumpToSiteChannelAccount(siteId: number, accountId: number) {
    requestJump({ kind: "site-channel-account", siteId, accountId });
  }

  function toggleSiteExpanded(siteId: number, forceExpanded: boolean) {
    if (forceExpanded) return;
    setExpandedSiteIds((current) => {
      const next = new Set(current);
      if (next.has(siteId)) next.delete(siteId);
      else next.add(siteId);
      return next;
    });
  }

  useEffect(() => {
    setSiteHandlers({
      openCreateDialog: () => {
        setEditingSite(null);
        setSiteDialogOpen(true);
      },
      openImportDialog: () => setImportDialogOpen(true),
      openArchivedDialog: () => setArchivedDialogOpen(true),
      syncAll: () => {
        syncAllSites.mutate(undefined, {
          onSuccess: () => toast.success(t("site.toast.syncAllTriggered")),
          onError: (error) => toast.error(siteErrorMessage(error)),
        });
      },
      checkinAll: () => {
        checkinAllSites.mutate(undefined, {
          onSuccess: () => toast.success(t("site.toast.checkinAllTriggered")),
          onError: (error) => toast.error(siteErrorMessage(error)),
        });
      },
    });

    return () => {
      resetSiteHandlers();
    };
  }, [
    setSiteHandlers,
    resetSiteHandlers,
    syncAllSites,
    checkinAllSites,
    locale,
    t,
    siteErrorMessage,
  ]);

  useEffect(() => {
    const updateDayKey = () => {
      const now = new Date();
      setStatusDayKey(`${now.getFullYear()}-${now.getMonth()}-${now.getDate()}`);
    };

    updateDayKey();
    const timer = window.setInterval(updateDayKey, 60_000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const observerMap = cardObserversRef.current;
    const elementMap = cardElementsRef.current;
    const accountMap = accountElementsRef.current;
    return () => {
      for (const observer of observerMap.values()) {
        observer.disconnect();
      }
      observerMap.clear();
      elementMap.clear();
      accountMap.clear();
    };
  }, []);

  useEffect(() => {
    if (!pendingSiteJump) return;

    const { requestId, target } = pendingSiteJump;
    const targetSiteId = target.siteId;
    const siteVisible = visibleSites.some((item) => item.site.id === targetSiteId);
    if (!siteVisible) return;

    if (target.kind === "site-account") {
      setExpandedSiteIds((current) => {
        if (current.has(target.siteId)) return current;
        const next = new Set(current);
        next.add(target.siteId);
        return next;
      });
    }

    const node =
      target.kind === "site-account"
        ? accountElementsRef.current.get(target.accountId)
        : cardElementsRef.current.get(target.siteId);
    if (!node) return;

    const timer = window.setTimeout(() => {
      node.scrollIntoView({ behavior: "smooth", block: "center" });
      flashTarget("site", target.siteId);
      if (target.kind === "site-account") {
        flashTarget("account", target.accountId);
      }
      clearPendingJump(requestId);
    }, 80);

    return () => window.clearTimeout(timer);
  }, [pendingSiteJump, visibleSites, clearPendingJump, flashTarget]);

  const masonryColumns = useMemo<[VisibleSite[], VisibleSite[]]>(() => {
    const left: VisibleSite[] = [];
    const right: VisibleSite[] = [];
    let leftHeight = 0;
    let rightHeight = 0;

    for (const item of visibleSites) {
      const isExpanded = item.forceExpanded || expandedSiteIds.has(item.site.id);
      const estimatedHeight =
        siteCardHeights[item.site.id] ??
        estimateVisibleSiteCardHeight(item, isExpanded);
      if (leftHeight <= rightHeight) {
        left.push(item);
        leftHeight += estimatedHeight;
      } else {
        right.push(item);
        rightHeight += estimatedHeight;
      }
    }

    return [left, right];
  }, [visibleSites, expandedSiteIds, siteCardHeights]);

  const renderSiteCard = ({
    site,
    summary,
    visibleAccounts,
    forceExpanded,
    hasFilteredAccounts,
  }: VisibleSite) => {
    const isExpanded = forceExpanded || expandedSiteIds.has(site.id);

    return (
      <section
        key={site.id}
        className={cn(
          "rounded-[28px] border bg-card p-5 transition-colors",
          cardToneClass(summary.healthTone),
          highlightedSiteId === site.id &&
            "ring-2 ring-primary/35 ring-offset-2 ring-offset-background",
        )}
      >
        <div className="flex items-start gap-3">
          {batchMode ? (
            <button
              type="button"
              className="mt-1 shrink-0 text-muted-foreground transition-colors hover:text-foreground"
              title={
                selectedSiteIds.includes(site.id)
                  ? t("site.deselectSite")
                  : t("site.selectSite")
              }
              onClick={() => toggleSiteSelection(site.id)}
            >
              {selectedSiteIds.includes(site.id) ? (
                <CheckSquare className="size-5 text-primary" />
              ) : (
                <Square className="size-5" />
              )}
            </button>
          ) : null}

          <div className="min-w-0 flex-1">
            <div className="flex items-start gap-3">
              <div
                className="min-w-0 flex-1 cursor-pointer text-left"
                role="button"
                tabIndex={0}
                onClick={() => toggleSiteExpanded(site.id, forceExpanded)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    toggleSiteExpanded(site.id, forceExpanded);
                  }
                }}
              >
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="truncate text-lg font-semibold">{site.name}</h2>
                  {site.is_pinned ? (
                    <Badge variant="outline" className="text-amber-600">
                      <Pin className="mr-1 size-3" />
                      {t("site.actions.pin")}
                    </Badge>
                  ) : null}
                  <Badge variant="outline">
                    {PLATFORM_LABELS[site.platform]}
                  </Badge>
                  <Badge
                    variant="outline"
                    className={badgeToneClass(summary.healthTone)}
                  >
                    {summary.healthKind === "failed"
                      ? t("site.health.failed", { count: summary.healthCount })
                      : summary.healthKind === "disabled"
                        ? t("site.health.disabled", {
                            count: summary.healthCount,
                          })
                        : summary.healthKind === "partial"
                          ? t("site.health.partial", {
                              count: summary.healthCount,
                            })
                          : summary.healthKind === "siteDisabled"
                            ? t("site.health.siteDisabled")
                            : summary.healthKind === "unconfigured"
                              ? t("site.health.unconfigured")
                              : summary.healthKind === "idle"
                                ? t("site.health.idle")
                                : t("site.health.ok")}
                  </Badge>
                </div>

                <div className="mt-2 flex items-center gap-2 text-sm text-muted-foreground">
                  <Link2 className="size-4 shrink-0" />
                  <a
                    href={site.base_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="truncate hover:text-foreground hover:underline transition-colors"
                    onClick={(e) => e.stopPropagation()}
                  >
                    {site.base_url}
                  </a>
                </div>

                <div className="mt-3 flex flex-wrap gap-x-4 gap-y-2">
                  <CompactMetric label={t("site.metrics.accounts")} value={summary.accountCount} />
                  <CompactMetric label="Key" value={summary.keyCount} />
                  <CompactMetric label={t("site.metrics.models")} value={summary.modelCount} />
                  <CompactMetric label={t("site.metrics.balance")} value={formatBalance(summary.balance)} />
                  <CompactMetric
                    label={t("site.metrics.todayIncome")}
                    value={formatBalance(summary.todayIncome)}
                  />
                </div>

                <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span>
                    {site.proxy_mode === "pool"
                      ? tProxy('mode.pool')
                      : site.proxy_mode === "system"
                        ? tProxy('mode.system')
                        : tProxy('mode.direct')}
                  </span>
                  {site.custom_header.length > 0 ? (
                    <span>{t("site.customHeaderCount", { count: site.custom_header.length })}</span>
                  ) : null}
                  {site.external_checkin_url ? <span>{t("site.checkinMode.manual")}</span> : null}
                </div>
              </div>

              <div className="flex items-center gap-1">
                {site.accounts.length === 0 ? (
                  <IconActionButton
                    label={t("site.addAccount")}
                    onClick={() => openCreateAccountDialog(site)}
                  >
                    <Plus className="size-4" />
                  </IconActionButton>
                ) : null}

                <Popover>
                  <PopoverTrigger asChild>
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="outline"
                      className="rounded-xl"
                      aria-label={t('site.moreSiteActions')}
                      title={t('site.moreSiteActions')}
                    >
                      <MoreHorizontal className="size-4" />
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent
                    align="end"
                    className="w-52 rounded-2xl border border-border/60 bg-card p-2"
                  >
                    <div className="grid gap-1">
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => jumpToSiteChannel(site.id)}
                      >
                        <Waypoints className="size-4" />
                        <span>{t("site.viewSiteChannels")}</span>
                      </button>
                      {site.accounts.length > 0 ? (
                        <button
                          type="button"
                          className={MENU_BUTTON_CLASS}
                          onClick={() => openCreateAccountDialog(site)}
                        >
                          <Plus className="size-4" />
                          <span>{t("site.addAccount")}</span>
                        </button>
                      ) : null}
                      <div className="my-1 border-t border-border/60" />
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => openEditSiteDialog(site)}
                      >
                        <Pencil className="size-4" />
                        <span>{t("site.editSite")}</span>
                      </button>
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => handleTogglePin(site)}
                      >
                        {site.is_pinned ? (
                          <PinOff className="size-4" />
                        ) : (
                          <Pin className="size-4" />
                        )}
                        <span>{site.is_pinned ? t("site.actions.unpin") : t("site.actions.pin")}</span>
                      </button>
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => handleToggleSite(site)}
                      >
                        <Power className="size-4" />
                        <span>{site.enabled ? t("site.actions.disableSite") : t("site.actions.enableSite")}</span>
                      </button>
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => handleArchiveSite(site)}
                      >
                        <Archive className="size-4" />
                        <span>{t("site.actions.archiveSite")}</span>
                      </button>
                      <button
                        type="button"
                        className={cn(MENU_BUTTON_CLASS, "text-destructive")}
                        onClick={() => handleDeleteSite(site)}
                      >
                        <Trash2 className="size-4" />
                        <span>{t("site.actions.deleteSite")}</span>
                      </button>
                    </div>
                  </PopoverContent>
                </Popover>

                <IconActionButton
                  label={
                    forceExpanded
                      ? t("site.accountsAutoExpanded")
                      : isExpanded
                        ? t("site.collapseAccounts")
                        : t("site.expandAccounts")
                  }
                  disabled={forceExpanded || site.accounts.length === 0}
                  onClick={() => toggleSiteExpanded(site.id, forceExpanded)}
                >
                  <ChevronDown
                    className={cn(
                      "size-4 transition-transform",
                      isExpanded && "rotate-180",
                    )}
                  />
                </IconActionButton>
              </div>
            </div>

            <AnimatePresence initial={false}>
              {isExpanded ? (
                <motion.div
                  key="site-accounts"
                  initial={{ opacity: 0, height: 0 }}
                  animate={{ opacity: 1, height: 'auto' }}
                  exit={{ opacity: 0, height: 0 }}
                  transition={{ duration: 0.18, ease: [0.25, 0.46, 0.45, 0.94] }}
                  className="overflow-hidden"
                  style={{ willChange: 'height, opacity' }}
                >
                  <div className="mt-4 border-t border-border/60 pt-4">
                    {hasFilteredAccounts ? (
                      <div className="mb-3 text-xs text-muted-foreground">
                        {t("site.visibleAccountCount", {
                          visible: visibleAccounts.length,
                          total: site.accounts.length,
                        })}
                      </div>
                    ) : null}

                    {visibleAccounts.length === 0 ? (
                      <div className="rounded-2xl border border-dashed border-border/70 bg-muted/10 px-4 py-6 text-sm text-muted-foreground">
                        {t("site.noAccountsHint")}
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {visibleAccounts.map((account) => {
                          const accountFailed = accountHasHealthFailure(site, account);
                          const accountTone: HealthTone = accountFailed
                            ? "danger"
                            : account.enabled
                              ? "default"
                              : "muted";
                          const supportsCheckin = sitePlatformSupportsCheckin(
                            site.platform,
                          );
                          const canShowManualCheckin =
                            supportsCheckin &&
                            accountHasCheckinEnabled(account, site.platform);

                          return (
                            <article
                              key={account.id}
                              ref={(node) => setAccountElementRef(account.id, node)}
                              className={cn(
                                "rounded-2xl border px-4 py-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.04)] transition-colors",
                                cardToneClass(accountTone),
                                highlightedAccountId === account.id &&
                                  "ring-2 ring-primary/35 ring-offset-2 ring-offset-background",
                              )}
                            >
                              <div className="space-y-3">
                                <div className="flex items-start gap-3">
                                  <div className="min-w-0 flex-1 space-y-2">
                                    <div className="flex flex-wrap items-center gap-2">
                                      <div className="text-sm font-semibold">
                                        {account.name}
                                      </div>
                                      <Badge variant="outline">
                                        {CREDENTIAL_LABELS[
                                          account.credential_type
                                        ] ?? t("site.credential.usernamePassword")}
                                      </Badge>
                                      <Badge
                                        variant="outline"
                                        className={
                                          account.enabled
                                            ? "text-emerald-600"
                                            : "text-muted-foreground"
                                        }
                                      >
                                        {account.enabled ? t("site.accountStatus.enabled") : t("site.accountStatus.disabled")}
                                      </Badge>
                                    </div>

                                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                                      <CompactMetric
                                        label={t("site.metrics.groups")}
                                        value={account.user_groups.length}
                                      />
                                      <CompactMetric
                                        label={t("site.metrics.models")}
                                        value={account.models.length}
                                      />
                                      <CompactMetric
                                        label={t("site.metrics.balance")}
                                        value={formatBalance(account.balance)}
                                      />
                                      <CompactMetric
                                        label={t("site.metrics.todayIncome")}
                                        value={formatBalance(account.today_income)}
                                      />
                                    </div>

                                    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                                      <span>
                                        {account.auto_sync ? t("site.syncMode.auto") : t("site.syncMode.manual")}
                                      </span>
                                      <span>
                                        {account.auto_checkin
                                          ? account.random_checkin
                                            ? t("site.checkinMode.random")
                                            : t("site.checkinMode.auto")
                                          : t("site.checkinMode.manual")}
                                      </span>
                                      <span>
                                        {account.proxy_mode === "inherit"
                                          ? tProxy('site.inherit')
                                          : account.proxy_mode === "pool"
                                            ? tProxy('mode.pool')
                                            : account.proxy_mode === "system"
                                              ? tProxy('mode.system')
                                              : tProxy('mode.direct')}
                                      </span>
                                    </div>
                                  </div>

                                  <div className="flex shrink-0 items-center gap-2 self-start">
                                    <Tooltip>
                                      <TooltipTrigger asChild>
                                        <span>
                                          <Switch
                                            checked={account.enabled}
                                            disabled={enableSiteAccount.isPending}
                                            onCheckedChange={() =>
                                              handleToggleAccount(account)
                                            }
                                          />
                                        </span>
                                      </TooltipTrigger>
                                      <TooltipContent>
                                        {account.enabled ? t("site.actions.disableAccount") : t("site.actions.enableAccount")}
                                      </TooltipContent>
                                    </Tooltip>

                                    <IconActionButton
                                      label={t("site.actions.syncAccount")}
                                      disabled={syncingAccountIds.has(account.id)}
                                      onClick={() => handleSyncAccount(account)}
                                    >
                                      <RefreshCw
                                        className={cn(
                                          "size-4",
                                          syncingAccountIds.has(account.id) &&
                                            "animate-spin",
                                        )}
                                      />
                                    </IconActionButton>

                                    <Popover>
                                      <PopoverTrigger asChild>
                                        <Button
                                          type="button"
                                          size="icon-sm"
                                          variant="outline"
                                          className="rounded-xl"
                                          aria-label={t('site.moreAccountActions')}
                                          title={t('site.moreAccountActions')}
                                        >
                                          <MoreHorizontal className="size-4" />
                                        </Button>
                                      </PopoverTrigger>
                                      <PopoverContent
                                        align="end"
                                        className="w-44 rounded-2xl border border-border/60 bg-card p-2"
                                      >
                                        <div className="grid gap-1">
                                          <button
                                            type="button"
                                            className={MENU_BUTTON_CLASS}
                                            onClick={() =>
                                              jumpToSiteChannelAccount(site.id, account.id)
                                            }
                                          >
                                            <Waypoints className="size-4" />
                                            <span>{t("site.viewSiteChannels")}</span>
                                          </button>
                                          <button
                                            type="button"
                                            className={cn(
                                              MENU_BUTTON_CLASS,
                                              "disabled:cursor-not-allowed disabled:opacity-50",
                                            )}
                                            onClick={() =>
                                              handleCheckinAccount(account)
                                            }
                                            disabled={checkinAccountIds.has(account.id)}
                                            hidden={!canShowManualCheckin}
                                          >
                                            <CalendarCheck2 className="size-4" />
                                            <span>{t("site.actions.checkinNow")}</span>
                                          </button>
                                          <button
                                            type="button"
                                            className={MENU_BUTTON_CLASS}
                                            onClick={() =>
                                              openEditAccountDialog(site, account)
                                            }
                                          >
                                            <Pencil className="size-4" />
                                            <span>{t("site.editAccount")}</span>
                                          </button>
                                          <button
                                            type="button"
                                            className={cn(
                                              MENU_BUTTON_CLASS,
                                              "text-destructive",
                                            )}
                                            onClick={() =>
                                              handleDeleteAccount(account)
                                            }
                                          >
                                            <Trash2 className="size-4" />
                                            <span>{t("site.actions.deleteAccount")}</span>
                                          </button>
                                        </div>
                                      </PopoverContent>
                                    </Popover>
                                  </div>
                                </div>

                                <div className="space-y-1">
                                    <ExecutionSummary
                                      kind="sync"
                                      status={normalizedStatus(
                                        account.last_sync_status,
                                      )}
                                      at={account.last_sync_at}
                                      message={
                                        translateSiteMessage(locale, account.last_sync_message, t) || t("site.execution.awaitingFirstSync")
                                      }
                                    />
                                    {supportsCheckin ? (
                                      accountHasCheckinEnabled(
                                        account,
                                        site.platform,
                                      ) ? (
                                        <ExecutionSummary
                                          kind="checkin"
                                          status={normalizedStatus(
                                            account.last_checkin_status,
                                          )}
                                          at={account.last_checkin_at}
                                          message={
                                            account.last_checkin_message ||
                                            t("site.execution.awaitingFirstCheckin")
                                          }
                                        />
                                      ) : (
                                        <StaticSummary text={t("site.execution.checkinDisabled")} />
                                      )
                                    ) : (
                                      <StaticSummary
                                        tone="warning"
                                        text={t("site.execution.checkinUnsupported")}
                                      />
                                    )}
                                    {account.auto_checkin &&
                                    account.random_checkin ? (
                                      <div className="pl-4 text-xs text-muted-foreground">
                                        {t("site.nextAutoCheckin", {
                                          at:
                                            formatDateTime(
                                              account.next_auto_checkin_at,
                                            ) ?? t("site.checkinPendingSchedule"),
                                          hours:
                                            account.checkin_interval_hours,
                                          minutes:
                                            account.checkin_random_window_minutes,
                                        })}
                                      </div>
                                    ) : null}
                                </div>
                              </div>
                            </article>
                          );
                        })}
                      </div>
                    )}
                  </div>
                </motion.div>
              ) : null}
            </AnimatePresence>
          </div>
        </div>
      </section>
    );
  };

  const [showAutomation, setShowAutomation] = useState(false);

  return (
    <div className="rounded-t-3xl">
      <PageWrapper
        className="space-y-3 pb-4 sm:space-y-4"
      >
        <CheckinPanel
          sites={sites}
          inventory={inventory}
          statusDayKey={statusDayKey}
          visibleSiteCount={visibleSites.length}
          visibleAccountCount={visibleAccountCount}
          searchTerm={searchTerm.trim()}
          hasActiveFilters={hasActiveFilters}
          onClearFilters={clearFilters}
          activeFilterStatuses={checkinFilterStatuses}
          onFilterChange={handleCheckinFilterChange}
        />

        <div className="flex flex-wrap items-center justify-between gap-3">
          <Button
            variant="default"
            size="sm"
            className="rounded-xl"
            onClick={openCreateSiteDialog}
          >
            <Plus className="size-4" />
            {t("site.addSite")}
          </Button>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              className="rounded-xl"
              onClick={() => setShowAutomation((v) => !v)}
            >
              <Settings className="size-4" />
              {showAutomation
                ? t("site.hideAutomation")
                : t("site.showAutomation")}
            </Button>
          </div>
        </div>

        {showAutomation && <SettingSiteAutomation />}

        {batchMode ? (
          <section className="rounded-3xl border border-primary/30 bg-primary/5 p-4">
            <div className="flex flex-wrap items-center gap-3">
              {(() => {
                const visibleIds = visibleSites.map((item) => item.site.id);
                const allVisibleSelected =
                  visibleIds.length > 0 &&
                  visibleIds.every((id) => selectedSiteIds.includes(id));
                return (
                  <button
                    type="button"
                    onClick={() => {
                      if (allVisibleSelected) {
                        setSelectedSiteIds((prev) =>
                          prev.filter((id) => !visibleIds.includes(id))
                        );
                      } else {
                        setSelectedSiteIds((prev) =>
                          Array.from(new Set([...prev, ...visibleIds]))
                        );
                      }
                    }}
                    disabled={visibleIds.length === 0}
                    title={
                      allVisibleSelected
                        ? t("site.deselectAll")
                        : t("site.selectAllVisible")
                    }
                    className="inline-flex items-center gap-2 text-sm font-medium text-foreground transition-colors hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {allVisibleSelected ? (
                      <CheckSquare className="size-5 text-primary" />
                    ) : (
                      <Square className="size-5" />
                    )}
                    {t("site.selectAll")}
                  </button>
                );
              })()}
              <span className="text-sm font-medium">
                {t("site.selectedCount", { count: selectedSiteIds.length })}
              </span>
              <Button
                variant="outline"
                size="sm"
                className="rounded-xl"
                onClick={() => handleBatchAction("enable")}
                disabled={batchAction.isPending || selectedSiteIds.length === 0}
              >
                {t("site.batch.enable")}
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="rounded-xl"
                onClick={() => handleBatchAction("disable")}
                disabled={batchAction.isPending || selectedSiteIds.length === 0}
              >
                {t("site.batch.disable")}
              </Button>
              <Button
                variant="destructive"
                size="sm"
                className="rounded-xl"
                onClick={() => handleBatchAction("delete")}
                disabled={batchAction.isPending || selectedSiteIds.length === 0}
              >
                {t("site.batch.delete")}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="rounded-xl"
                onClick={() => {
                  setSelectedSiteIds([]);
                  setBatchMode(false);
                }}
              >
                {t("site.batch.done")}
              </Button>
            </div>
          </section>
        ) : (
          <div className="flex justify-end">
            <Button
              variant="outline"
              size="sm"
              className="rounded-xl"
              onClick={() => setBatchMode(true)}
            >
              {t("site.batch.edit")}
            </Button>
          </div>
        )}

        {error ? (
          <section className="rounded-3xl border border-destructive/30 bg-destructive/5 p-6 text-sm text-destructive">
            {t("site.loadFailed", { message: siteErrorMessage(error) })}
          </section>
        ) : null}

        {isLoading ? (
          <section className="rounded-3xl border border-border bg-card p-6 text-sm text-muted-foreground">
            {t("site.loading")}
          </section>
        ) : null}

        {!isLoading && !error && (!sites || sites.length === 0) ? (
          <section className="rounded-3xl border border-dashed border-border bg-card p-10 text-center">
            <CircleAlert className="mx-auto size-8 text-muted-foreground" />
            <div className="mt-4 text-lg font-semibold">{t("site.empty.title")}</div>
            <p className="mt-2 text-sm text-muted-foreground">
              {t("site.empty.hint")}
            </p>
            <Button onClick={openCreateSiteDialog} className="mt-5 rounded-xl">
              <Plus className="size-4" />
              {t("site.empty.action")}
            </Button>
          </section>
        ) : null}

        {!isLoading &&
        !error &&
        sites &&
        sites.length > 0 &&
        visibleSites.length === 0 ? (
          <section className="rounded-3xl border border-dashed border-border bg-card p-10 text-center">
            <CircleAlert className="mx-auto size-8 text-muted-foreground" />
            <div className="mt-4 text-lg font-semibold">{t("site.noMatch.title")}</div>
            <p className="mt-2 text-sm text-muted-foreground">
              {t("site.noMatch.hint")}
            </p>
            <Button
              type="button"
              variant="outline"
              className="mt-5 rounded-xl"
              onClick={clearFilters}
            >
              <FilterX className="size-4" />
              {t("site.noMatch.clearFilters")}
            </Button>
          </section>
        ) : null}

        {visibleSites.length > 0 ? (
          <>
            {/* 移动端单列不需要高度估算；且它与桌面列表同时挂载，
                若在此注册测量会与桌面测量互相覆盖（见 getSiteCardMeasureRef 注释）。 */}
            <div className="space-y-4 md:hidden">
              {visibleSites.map((item) => (
                <div key={item.site.id}>{renderSiteCard(item)}</div>
              ))}
            </div>
            <div className="hidden items-start gap-4 md:grid md:grid-cols-2">
              <div className="space-y-4">
                {masonryColumns[0].map((item) => (
                  <div
                    key={item.site.id}
                    ref={getSiteCardMeasureRef(item.site.id)}
                  >
                    {renderSiteCard(item)}
                  </div>
                ))}
              </div>
              <div className="space-y-4">
                {masonryColumns[1].map((item) => (
                  <div
                    key={item.site.id}
                    ref={getSiteCardMeasureRef(item.site.id)}
                  >
                    {renderSiteCard(item)}
                  </div>
                ))}
              </div>
            </div>
          </>
        ) : null}
      </PageWrapper>

      <SiteEditDialog
        key={editingSite ? `edit-site-${editingSite.id}` : "create-site"}
        open={siteDialogOpen}
        onOpenChange={closeSiteDialog}
        site={editingSite}
        onCreated={(createdSite) => openCreateAccountDialog(createdSite)}
      />

      <AccountEditDialog
        key={
          editingAccount
            ? `edit-site-account-${editingAccount.id}`
            : accountSite
              ? `create-site-account-${accountSite.id}`
              : "site-account"
        }
        open={accountDialogOpen}
        onOpenChange={closeAccountDialog}
        site={accountSite}
        account={editingAccount}
      />

      <Dialog
        open={importDialogOpen}
        onOpenChange={(open) => {
          setImportDialogOpen(open);
          if (!open) setLastImportResult(null);
        }}
      >
        <DialogContent className="max-w-3xl rounded-3xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <FileJson className="size-5" />
              {t("site.import.title")}
            </DialogTitle>
            <DialogDescription>
              {t("site.import.description")}
            </DialogDescription>
          </DialogHeader>

          <div
            className="space-y-5"
            onDragEnter={handleImportDragEnter}
            onDragLeave={handleImportDragLeave}
            onDragOver={handleImportDragOver}
            onDrop={handleImportDrop}
          >
            <div className="grid gap-2 text-sm">
              <span className="font-medium">{t("site.import.sourceLabel")}</span>
              <Select
                value={importSource}
                onValueChange={(value) => {
                  setImportSource(value as ImportSource);
                  setLastImportResult(null);
                }}
              >
                <SelectTrigger className="rounded-xl">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all-api-hub">All API Hub</SelectItem>
                  <SelectItem value="metapi">Metapi</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-2 text-sm">
              <div className="text-sm font-medium">{t("site.import.uploadLabel")}</div>
              <div className="flex items-center gap-2">
                <Input
                  ref={importFileInputRef}
                  type="file"
                  accept=".json,application/json"
                  onChange={(event) => {
                    setSelectedImportFile(event.target.files?.[0] ?? null);
                  }}
                  className="hidden"
                />
                <button
                  type="button"
                  onClick={() => importFileInputRef.current?.click()}
                  className={cn(
                    "flex min-w-0 flex-1 items-center justify-center rounded-xl border border-dashed px-3 text-center text-sm transition-all hover:bg-muted/30",
                    isImportDragging
                      ? "min-h-28 border-primary bg-primary/10 text-primary"
                      : "min-h-10 border-border bg-muted/20",
                  )}
                >
                  <span
                    className={cn(
                      "min-w-0 truncate",
                      importFile ? "text-foreground" : "text-muted-foreground",
                    )}
                  >
                    {isImportDragging
                      ? t("site.import.dropHint")
                      : importFile?.name ?? t("site.import.pickHint")}
                  </span>
                </button>
                <IconActionButton
                  label={t("site.import.clearFile")}
                  onClick={() => {
                    setSelectedImportFile(null);
                  }}
                  disabled={!importFile}
                  className={!importFile ? "opacity-50" : undefined}
                >
                  <X className="size-4" />
                </IconActionButton>
              </div>
              <div className="text-xs text-muted-foreground">
                {importFile
                  ? t("site.import.selectedFile", { name: importFile.name })
                  : t("site.import.acceptHint", {
                      source:
                        importSource === "metapi" ? "Metapi" : "All API Hub",
                    })}
              </div>
            </div>

            <label className="grid gap-2 text-sm">
              <span className="font-medium">{t("site.import.pasteLabel")}</span>
              <textarea
                value={importPayloadText}
                onChange={(event) => {
                  setImportPayloadText(event.target.value);
                  setLastImportResult(null);
                }}
                placeholder={t("site.import.pastePlaceholder", {
                  // 样例 JSON 里的花括号会被 ICU 当占位符解析，故走参数传入。
                  sample:
                    importSource === "metapi"
                      ? '{"version":"2.1","accounts":{"sites":[...],"accounts":[...]}}'
                      : '{"accounts":{"accounts":[...]}}',
                })}
                className="min-h-40 rounded-2xl border border-input bg-background px-4 py-3 font-mono text-xs outline-none transition focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/20"
              />
              <span className="text-xs text-muted-foreground">
                {importSource === "metapi"
                  ? t("site.import.metapiNote")
                  : t("site.import.allApiHubNote")}
              </span>
            </label>

            {lastImportResult ? (
              <div className="space-y-4 rounded-2xl border border-border/60 bg-muted/10 p-4">
                <div className="grid gap-3 sm:grid-cols-3">
                  <SiteMetric
                    label={t("site.import.stat.createdSites")}
                    value={lastImportResult.created_sites}
                  />
                  <SiteMetric
                    label={t("site.import.stat.reusedSites")}
                    value={lastImportResult.reused_sites}
                  />
                  <SiteMetric
                    label={t("site.addAccount")}
                    value={lastImportResult.created_accounts}
                  />
                  <SiteMetric
                    label={t("site.import.stat.updatedAccounts")}
                    value={lastImportResult.updated_accounts}
                  />
                  <SiteMetric
                    label={t("site.import.stat.skippedAccounts")}
                    value={lastImportResult.skipped_accounts}
                  />
                  {typeof lastImportResult.scheduled_sync_accounts ===
                  "number" ? (
                    <SiteMetric
                      label={t("site.import.stat.scheduledSync")}
                      value={lastImportResult.scheduled_sync_accounts}
                    />
                  ) : null}
                  {typeof lastImportResult.imported_tokens === "number" ? (
                    <>
                      <SiteMetric
                        label={t("site.import.stat.importedKeys")}
                        value={lastImportResult.imported_tokens}
                      />
                      <SiteMetric
                        label={t("site.import.stat.importedGroups")}
                        value={lastImportResult.imported_groups ?? 0}
                      />
                      <SiteMetric
                        label={t("site.import.stat.importedModels")}
                        value={lastImportResult.imported_models ?? 0}
                      />
                      <SiteMetric
                        label={t("site.import.stat.disabledModels")}
                        value={lastImportResult.disabled_models ?? 0}
                      />
                    </>
                  ) : null}
                </div>

                {lastImportResult.warnings.length > 0 ? (
                  <div className="rounded-2xl border border-border/60 bg-background/70 p-4">
                    <div className="flex items-center gap-2 text-sm font-medium">
                      <TriangleAlert className="size-4 text-muted-foreground" />
                      <span>{t("site.import.warnings")}</span>
                    </div>
                    <div className="mt-3 space-y-2 text-sm text-muted-foreground">
                      {lastImportResult.warnings.map((warning) => (
                        <div
                          key={warning}
                          className="break-all rounded-xl border border-border/60 bg-muted/20 px-3 py-2"
                        >
                          {warning}
                        </div>
                      ))}
                    </div>
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              className="rounded-xl"
              onClick={() => setImportDialogOpen(false)}
            >
              {t("common.close")}
            </Button>
            <Button
              onClick={handleImportSites}
              disabled={importAllAPIHub.isPending || importMetAPI.isPending}
              className="rounded-xl"
            >
              <Upload
                className={cn(
                  "size-4",
                  importAllAPIHub.isPending || importMetAPI.isPending
                    ? "animate-pulse"
                    : "",
                )}
              />
              {importAllAPIHub.isPending || importMetAPI.isPending
                ? t("site.import.importing")
                : t("site.import.start")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={archivedDialogOpen} onOpenChange={setArchivedDialogOpen}>
        <DialogContent className="flex h-[min(85vh,42rem)] max-w-3xl flex-col overflow-hidden rounded-3xl border-border/70 p-0 sm:max-w-3xl">
          <DialogHeader className="shrink-0 border-b border-border/60 px-6 py-4">
            <DialogTitle>{t("site.archived.title")}</DialogTitle>
            <DialogDescription>
              {t("site.archived.description")}
            </DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
            {archivedLoading ? (
              <div className="py-10 text-center text-sm text-muted-foreground">
                {t("site.archived.loading")}
              </div>
            ) : archivedError ? (
              <div className="rounded-2xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
                {t("site.archived.loadFailed", { message: siteErrorMessage(archivedError) })}
              </div>
            ) : !archivedSites || archivedSites.length === 0 ? (
              <div className="py-10 text-center text-sm text-muted-foreground">
                {t("site.archived.empty")}
              </div>
            ) : (
              <div className="space-y-2">
                {archivedSites.map((site) => (
                  <div
                    key={site.id}
                    className="flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-card/60 p-3"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate font-medium">
                          {site.name}
                        </span>
                        <Badge variant="outline" className="rounded-full text-xs">
                          {site.platform}
                        </Badge>
                        <span className="truncate text-xs text-muted-foreground">
                          {site.base_url}
                        </span>
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        {t("site.archived.archivedAt", {
                          at: site.archived_at
                            ? new Date(site.archived_at).toLocaleString()
                            : "-",
                        })}
                        {" · "}
                        {t("site.archived.accountsKept", {
                          count: site.accounts.length,
                        })}
                      </div>
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      className="rounded-xl"
                      onClick={() => handleRestoreSite(site.id, site.name)}
                      disabled={restoreSite.isPending}
                    >
                      <ArchiveRestore className="size-4" />
                      {t("site.archived.restore")}
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
          <DialogFooter className="shrink-0 border-t border-border/60 px-6 py-4">
            <Button
              variant="outline"
              className="rounded-xl"
              onClick={() => setArchivedDialogOpen(false)}
            >
              {t("common.close")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!deleteConfirm}
        onOpenChange={(open) => {
          if (!open) setDeleteConfirm(null);
        }}
      >
        <DialogContent className="max-w-md rounded-3xl">
          <DialogHeader>
            <DialogTitle>
              {deleteConfirm?.type === "archive-site"
                ? t("site.confirm.archiveTitle")
                : t("site.confirm.deleteTitle")}
            </DialogTitle>
            <DialogDescription>
              {deleteConfirm?.type === "site"
                ? t("site.confirm.deleteSite", { name: deleteConfirm.name })
                : deleteConfirm?.type === "archive-site"
                  ? t("site.confirm.archiveSite", {
                      name: deleteConfirm.name,
                    })
                  : t("site.confirm.deleteAccount", {
                      name: deleteConfirm?.name ?? "",
                    })}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              className="rounded-xl"
              onClick={() => setDeleteConfirm(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              variant="destructive"
              className="rounded-xl"
              onClick={confirmDelete}
              disabled={
                deleteSite.isPending ||
                deleteSiteAccount.isPending ||
                archiveSite.isPending
              }
            >
              {deleteConfirm?.type === "archive-site"
                ? archiveSite.isPending
                  ? t("site.confirm.archiving")
                  : t("site.confirm.archiveTitle")
                : deleteSite.isPending || deleteSiteAccount.isPending
                  ? t("site.confirm.deleting")
                  : t("site.confirm.deleteTitle")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
