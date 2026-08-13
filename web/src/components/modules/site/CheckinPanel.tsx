"use client";

import { useCallback, useMemo, useState, type ReactNode } from "react";
import { useTranslations } from "next-intl";
import {
  AlertTriangle,
  CalendarCheck2,
  ChevronDown,
  ExternalLink,
  FilterX,
  Layers3,
  TrendingUp,
  Wallet,
} from "lucide-react";
import { type Site } from "@/api/endpoints/site";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  buildCheckinSummary,
  type CheckinActiveFilterStatus,
  type CheckinFilterStatus,
} from "./checkin-status";

function FilterLabel({ status }: { status: CheckinFilterStatus }) {
  const t = useTranslations("site.checkinPanel.filter");

  switch (status) {
    case "all":
      return <>{t("all")}</>;
    case "success":
      return <>{t("success")}</>;
    case "failed":
      return <>{t("failed")}</>;
    case "idle":
      return <>{t("idle")}</>;
    case "disabled":
      return <>{t("disabled")}</>;
  }
}

// Enum order only — labels are resolved at the render site with literal keys, so
// the i18n gate can see them (a table of copy or of key strings is invisible to it).
const FILTER_ORDER: CheckinFilterStatus[] = [
  "all",
  "success",
  "failed",
  "idle",
  "disabled",
];

function filterTone(status: CheckinFilterStatus, active: boolean) {
  if (active) {
    switch (status) {
      case "success":
        return "border-emerald-500/30 bg-emerald-500 text-white";
      case "failed":
        return "border-destructive/30 bg-destructive text-white";
      case "idle":
        return "border-border bg-foreground text-background";
      case "disabled":
        return "border-slate-500/30 bg-slate-700 text-white dark:bg-slate-200 dark:text-slate-900";
      case "all":
      default:
        return "border-primary/30 bg-primary text-primary-foreground";
    }
  }

  switch (status) {
    case "success":
      return "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "failed":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "idle":
      return "border-border bg-muted/40 text-muted-foreground";
    case "disabled":
      return "border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-300";
    case "all":
    default:
      return "border-border bg-background text-foreground";
  }
}

function formatCurrency(value: number) {
  const safe = Number.isFinite(value) ? value : 0;
  return `$${safe.toFixed(2)}`;
}

function OverviewMetric({
  icon,
  label,
  value,
  tone,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  tone?: "default" | "warning";
}) {
  return (
    <div className="flex items-center gap-2.5 rounded-2xl bg-muted/20 px-3 py-2.5 sm:gap-3 sm:px-4 sm:py-3">
      <span
        className={cn(
          "flex size-8 shrink-0 items-center justify-center rounded-xl bg-background shadow-sm sm:size-9",
          tone === "warning"
            ? "text-amber-600 dark:text-amber-400"
            : "text-muted-foreground",
        )}
      >
        {icon}
      </span>
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-base font-semibold truncate">{value}</div>
      </div>
    </div>
  );
}

export function CheckinPanel({
  sites,
  inventory,
  statusDayKey,
  visibleSiteCount,
  visibleAccountCount,
  searchTerm,
  hasActiveFilters,
  onClearFilters,
  activeFilterStatuses,
  onFilterChange,
}: {
  sites: Site[] | undefined;
  inventory: {
    totalBalance: number;
    totalBalanceUsed: number;
    enabledAccounts: number;
    totalAccounts: number;
  };
  statusDayKey: string;
  visibleSiteCount: number;
  visibleAccountCount: number;
  searchTerm: string;
  hasActiveFilters: boolean;
  onClearFilters: () => void;
  activeFilterStatuses: CheckinActiveFilterStatus[];
  onFilterChange: (status: CheckinFilterStatus) => void;
}) {
  const t = useTranslations("site.checkinPanel");
  const summaryNow = useMemo(() => {
    const [year = "", month = "", day = ""] = statusDayKey.split("-");
    const parsed = new Date(Number(year), Number(month), Number(day));
    return Number.isNaN(parsed.getTime()) ? new Date() : parsed;
  }, [statusDayKey]);

  const summary = useMemo(
    () => buildCheckinSummary(sites, summaryNow),
    [sites, summaryNow],
  );
  const hasContextBadges = Boolean(searchTerm);

  const manualCheckinUrls = useMemo(
    () =>
      (sites ?? [])
        .filter((s) => s.external_checkin_url?.trim())
        .map((s) => s.external_checkin_url!.trim()),
    [sites],
  );

  const [overviewExpanded, setOverviewExpanded] = useState(true);

  const openAllManualCheckin = useCallback(() => {
    for (const url of manualCheckinUrls) {
      window.open(url, "_blank", "noopener,noreferrer");
    }
  }, [manualCheckinUrls]);

  return (
    <section className="overflow-hidden rounded-[28px] border border-border/70 bg-card shadow-[0_18px_60px_-40px_rgba(15,23,42,0.45)]">
      <div className="border-b border-border/60 bg-gradient-to-br from-background via-card to-muted/10 px-5 py-5">
        <button
          type="button"
          onClick={() => setOverviewExpanded((prev) => !prev)}
          className="flex w-full flex-col gap-3 lg:flex-row lg:items-center lg:justify-between"
        >
          <div className="flex flex-wrap items-center gap-2 text-base font-semibold">
            <CalendarCheck2 className="size-5 text-primary" />
            <span>{t("overview")}</span>
            <ChevronDown
              className={cn(
                "size-4 text-muted-foreground transition-transform duration-200",
                overviewExpanded && "rotate-180",
              )}
            />
          </div>

          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>{t("currentResults")}</span>
            <span className="font-medium text-foreground">
              {t("visibleCounts", {
                sites: visibleSiteCount,
                accounts: visibleAccountCount,
              })}
            </span>
          </div>
        </button>

        {overviewExpanded && (
          <div className="mt-4 grid grid-cols-2 gap-2.5 sm:gap-3 xl:grid-cols-4">
            <OverviewMetric
              icon={<Wallet className="size-4" />}
              label={t("currentBalance")}
              value={formatCurrency(inventory.totalBalance)}
            />
            <OverviewMetric
              icon={<TrendingUp className="size-4" />}
              label={t("totalSpent")}
              value={formatCurrency(inventory.totalBalanceUsed)}
            />
            <OverviewMetric
              icon={<Layers3 className="size-4" />}
              label={t("enabledAccounts")}
              value={`${inventory.enabledAccounts} / ${inventory.totalAccounts}`}
            />
            <OverviewMetric
              icon={<AlertTriangle className="size-4" />}
              label={t("todayIssues")}
              value={`${summary.failed}`}
              tone={summary.failed > 0 ? "warning" : "default"}
            />
          </div>
        )}

        {hasActiveFilters && hasContextBadges ? (
          <div className="mt-4 flex flex-wrap gap-2">
            {searchTerm ? (
              <Badge variant="outline">{t("searchBadge", { term: searchTerm })}</Badge>
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="px-5 py-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap gap-2">
            {FILTER_ORDER.map((status) => {
              const count = status === "all" ? summary.total : summary[status];
              const active =
                status === "all"
                  ? activeFilterStatuses.length === 0
                  : activeFilterStatuses.includes(status);
              return (
                <button
                  key={status}
                  type="button"
                  onClick={() => onFilterChange(status)}
                  className={cn(
                    "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
                    filterTone(status, active),
                  )}
                >
                  <span>{count}</span>
                  <span>
                    <FilterLabel status={status} />
                  </span>
                </button>
              );
            })}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {hasActiveFilters ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="rounded-xl text-xs"
                onClick={onClearFilters}
              >
                <FilterX className="size-4" />
                {t("clearFilters")}
              </Button>
            ) : null}
            {manualCheckinUrls.length > 0 ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="rounded-xl text-xs"
                onClick={openAllManualCheckin}
              >
                <ExternalLink className="size-4" />
                {t("openManualCheckin", { count: manualCheckinUrls.length })}
              </Button>
            ) : null}
          </div>
        </div>
      </div>
    </section>
  );
}
