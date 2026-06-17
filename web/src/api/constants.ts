export const REFETCH_INTERVAL_DEFAULT = 30_000;
export const REFETCH_INTERVAL_SLOW = 3_600_000;
export const REFETCH_INTERVAL_FAST = 10_000;
/** Polling interval for rarely-changing configuration data (rules, channels). */
export const REFETCH_INTERVAL_CONFIG = 60_000;

export const QUERY_STALE_TIME = 60_000;
export const QUERY_MAX_RETRIES = 2;
export const QUERY_RETRY_BACKOFF_CAP = 30_000;
