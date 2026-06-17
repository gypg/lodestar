import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Lazily read chinaMode / exchangeRate from the persisted setting store.
 * Avoids a hard import-cycle: consumers in components already use the store
 * directly; utils.ts only needs the values at call-time.
 */
let _settingStoreGetter: (() => { chinaMode: boolean; exchangeRate: number }) | null = null;

export function registerSettingStoreGetter(getter: () => { chinaMode: boolean; exchangeRate: number }) {
  _settingStoreGetter = getter;
}

function getChinaMode(): { chinaMode: boolean; exchangeRate: number } {
  if (_settingStoreGetter) return _settingStoreGetter();
  return { chinaMode: false, exchangeRate: 7.2 };
}

function formatNumber(num: number | undefined, compare: number[], units: string[]): { value: string, unit: string } {
  if (num === undefined) return { value: "0.00", unit: units[0] };
  else if (num >= compare[0]) return { value: (num / compare[0]).toFixed(2), unit: units[1] };
  else if (num >= compare[1]) return { value: (num / compare[1]).toFixed(2), unit: units[2] };
  else if (num >= compare[2]) return { value: (num / compare[2]).toFixed(2), unit: units[3] };
  else if (num >= compare[3]) return { value: (num / compare[3]).toFixed(2), unit: units[4] };
  else return { value: (num).toFixed(2), unit: units[5] };
}

/**
 * Format a count (token count, request count, etc.).
 * China mode: 万 (10k) / 亿 (100M) only — 千 is skipped.
 */
export function formatCount(num: number | undefined): { raw: number, formatted: { value: string, unit: string } } {
  const { chinaMode } = getChinaMode();
  if (chinaMode) {
    const v = num ?? 0;
    if (v >= 100_000_000) return { raw: v, formatted: { value: (v / 100_000_000).toFixed(2), unit: '亿' } };
    if (v >= 10_000)      return { raw: v, formatted: { value: (v / 10_000).toFixed(2), unit: '万' } };
    return { raw: v, formatted: { value: v.toLocaleString(), unit: '' } };
  }
  return {
    raw: num ?? 0,
    formatted: formatNumber(num, [1000000000, 1000000, 1000, 1], ['', 'B', 'M', 'K', '', '']),
  };
}

/**
 * Format a monetary amount (USD internally).
 * China mode: convert to RMB (× exchangeRate) and display as ¥, using 万/亿.
 */
export function formatMoney(num: number | undefined): { raw: number, formatted: { value: string, unit: string } } {
  const { chinaMode, exchangeRate } = getChinaMode();
  if (chinaMode) {
    const raw = (num ?? 0) * exchangeRate;
    if (raw >= 100_000_000) return { raw, formatted: { value: (raw / 100_000_000).toFixed(2), unit: '¥亿' } };
    if (raw >= 10_000)      return { raw, formatted: { value: (raw / 10_000).toFixed(2), unit: '¥万' } };
    return { raw, formatted: { value: raw.toFixed(2), unit: '¥' } };
  }
  return {
    raw: num ?? 0,
    formatted: formatNumber(num, [1000000000, 1000000, 1000, 1], ['$', 'B$', 'M$', 'K$', '$', '$']),
  };
}

export function formatTime(ms: number | undefined): { raw: number, formatted: { value: string, unit: string } } {
  return {
    raw: ms ?? 0,
    formatted: formatNumber(ms, [86400000, 3600000, 60000, 1000], ['', 'd', 'h', 'm', 's', 'ms']),
  };
}

// significantDecimalPlaces returns how many decimals to render for a numeric
// display string, trimming trailing zeros so counts like "5.00" show as "5"
// while genuine precision such as "1.5" or "1.23" is preserved.
export function significantDecimalPlaces(value: string | number | undefined): number {
  if (typeof value !== 'string') return 0;
  const fracPart = value.split('.')[1];
  if (!fracPart) return 0;
  return Math.min(2, fracPart.replace(/0+$/, '').length);
}