const FULL_SHA_PATTERN = /^[0-9a-f]{40}$/i;
const DEV_SHA_PATTERN = /^dev-([0-9a-f]{7,40})$/i;
const SHORT_SHA_LENGTH = 7;

/**
 * 运维中心的"版本"字段直接来自 conf.Version（构建时用 -ldflags 注入）。
 *
 * docker.yml 曾把 APP_VERSION 设成 `dev-<40位sha>`，于是 UI 上显示的是一长串
 * 十六进制，看不出这是哪个版本。构建侧已改成 `git describe --tags`，这里是
 * 兜底：老镜像仍在跑时把它压成 `dev (abc1234)`。
 *
 * 已经是语义化版本（v2.1.4、v2.1.4-3-gabc1234）时原样返回，不做任何加工。
 */
export function formatVersion(raw?: string | null): string {
    const value = (raw ?? '').trim();
    if (!value) return '-';

    const devSha = DEV_SHA_PATTERN.exec(value);
    if (devSha) return `dev (${devSha[1].slice(0, SHORT_SHA_LENGTH)})`;

    if (FULL_SHA_PATTERN.test(value)) return value.slice(0, SHORT_SHA_LENGTH);
    return value;
}

/**
 * 提交号收敛到短 sha。40 位全量在 UI 里只是噪音，且会撑破右对齐布局。
 * 非 sha 形态（如 "unknown"）原样返回。
 */
export function formatCommit(raw?: string | null): string {
    const value = (raw ?? '').trim();
    if (!value) return '-';
    return FULL_SHA_PATTERN.test(value) ? value.slice(0, SHORT_SHA_LENGTH) : value;
}
