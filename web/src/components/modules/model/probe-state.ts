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

/**
 * Go/JS 两侧可能出现的零值时刻写法。前缀匹配即可覆盖带毫秒与时区偏移的变体。
 *
 * ★ 关于下面两个前缀层各自的可测性（WO-034 对抗验收的实测结论，别删了这段注释）：
 *
 * - **0001 层是冗余的、且冗余是刻意的**：公元 1 年无论加什么时区偏移都解析成深负值
 *  （实测 `0001-01-01T00:00:00+08:00` → -62135625600000），所以末尾的 `parsed > 0`
 *   本来就兜得住整个 0001 家族。把这一层削弱成精确 `===` 比较，测试观测不到
 *  （实测该削弱型变异存活）。保留它是因为它表达意图、且不依赖 Date.parse 的行为；
 *   但**不要**声称有测试守着它——没有，这是纵深防御里不可证伪的那一层。
 *
 * - **纪元层不是冗余的，是承重的**：`Date.parse('1970-01-01T00:00:00')`（无 Z）按
 *   **本地时区**解析，结果 = -(本地 UTC 偏移)。本机 UTC+8 得 -28800000（负，兜得住），
 *   但 UTC-5 得 +18000000、UTC-8 得 +28800000（**正值，`parsed > 0` 兜不住**）——
 *   西半球浏览器会把纪元零值误判成"探测失败"。这一层在那些时区里是唯一防线。
 *   本机测不到（Windows 下 Node 不吃 `TZ=`），故此处以注释存证。
 */
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
