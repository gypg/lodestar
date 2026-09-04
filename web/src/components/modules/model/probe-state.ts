/**
 * 「这个模型是否被探测判定为不可用」的唯一判据。
 *
 * 为什么不直接写 `!!model.probe_failed_at`：那是原缺陷的形态。后端曾把该字段声明为
 * 非指针 `time.Time` 配 `omitempty`（Go 的 omitempty 对结构体无效），于是每个健康
 * 模型都收到 `"probe_failed_at":"0001-01-01T00:00:00Z"`；truthiness 判定把这个非空
 * 字符串读成"失败"，广场默认视图因此把**全部**模型隐藏，横幅还报出
 * "94 个模型连续探测失败"——而当时探测开关是关的、状态表零行，从未探测过。
 *
 * 后端已改为 `*time.Time`（healthy → 字段缺席），这里是第二道：任何形式的零值时刻
 * 都不算失败。两端都堵的理由同 float-config 那次——单端修复会在下一次序列化形态
 * 变动时静默失效，而这个失效方向是"把可用模型全部藏起来"。
 */

/** Go/JS 两侧可能出现的零值时刻写法。前缀匹配即可覆盖带毫秒与时区偏移的变体。 */
const ZERO_TIME_PREFIX = '0001-01-01';

export function isProbeFailed(model: { probe_failed_at?: string }): boolean {
    const raw = model.probe_failed_at;
    if (!raw) {
        return false;
    }
    const trimmed = raw.trim();
    if (trimmed === '') {
        return false;
    }
    // Go 零值时刻（0001-01-01T00:00:00Z）以及它的时区偏移变体
    if (trimmed.startsWith(ZERO_TIME_PREFIX)) {
        return false;
    }
    // Unix 纪元零值（若序列化方式变成时间戳字符串或 ISO 纪元）
    if (trimmed === '0' || trimmed.startsWith('1970-01-01T00:00:00')) {
        return false;
    }
    const parsed = Date.parse(trimmed);
    if (Number.isNaN(parsed)) {
        // 解析不了的值不当作"失败"：宁可少标一个徽章，也不要把可用模型藏起来。
        return false;
    }
    return parsed > 0;
}
