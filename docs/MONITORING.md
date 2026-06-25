# Lodestar 监控与告警策略

> 本文档描述 Lodestar 生产环境的监控体系，覆盖健康检查、关键指标、告警规则和日志管理。
> 适用于单实例自用部署（西安 2C2G 服务器）及未来多实例商业部署。

---

## 1. 健康检查端点

### 已有端点

**`GET /api/v1/bootstrap/status`**

- 返回 JSON，包含站点配置与运行状态
- 在 maintenance 模式下仍然可达（白名单硬编码，见 `internal/server/middleware/maintenance.go`）
- 前端用它判断站点 banner、初始化状态等

验证：

```bash
curl -sS http://localhost:8080/api/v1/bootstrap/status | head -c 500
```

### Docker 内置健康检查

Dockerfile 和 docker-compose.yml 均已配置：

```dockerfile
# Dockerfile
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/v1/bootstrap/status || exit 1
```

```yaml
# docker-compose.yml
healthcheck:
  test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/api/v1/bootstrap/status"]
  interval: 30s
  timeout: 3s
  start_period: 10s
  retries: 3
```

容器状态查看：

```bash
docker inspect --format='{{.State.Health.Status}}' lodestar
# healthy / starting / unhealthy
```

---

## 2. 关键监控指标

### 基础设施层

| 指标 | 采集方式 | 告警阈值 |
|------|---------|---------|
| 容器内存使用 | `docker stats` / cAdvisor | > 400 MB（限额 512 MB） |
| 容器 CPU 使用率 | `docker stats` / cAdvisor | 持续 > 80% |
| 磁盘使用率 | `df -h` | data 目录 > 80% |
| 容器重启次数 | `docker inspect` | > 3 次/小时 |
| 健康检查状态 | Docker healthcheck | unhealthy 连续 3 次 |

### 应用层

| 指标 | 数据来源 | 告警阈值 |
|------|---------|---------|
| 请求延迟（P95） | 反向代理日志 / 应用中间件 | > 5 秒 |
| 错误率（5xx） | 反向代理日志 / relay 日志 | > 5%（5 分钟窗口） |
| Relay 成功率 | relay_logs 表统计 | < 90%（10 分钟窗口） |
| 活跃 API key 数 | 数据库查询 | 仅观察，无固定阈值 |
| 数据库连接数 | PG `pg_stat_activity` | > 50（自用场景通常 < 5） |
| Redis 连接数 | `redis-cli info clients` | > 100 |

### Relay 专项

Relay 是 Lodestar 的核心业务，建议重点关注：

| 指标 | 计算方式 |
|------|---------|
| 成功率 | `COUNT(status=success) / COUNT(*) * 100` |
| 平均延迟 | `AVG(response_time)` 按渠道分组 |
| Token 用量 | `SUM(prompt_tokens + completion_tokens)` 按日 |
| 渠道错误分布 | `GROUP BY channel_id, error_code` |

可用 SQL（PostgreSQL）：

```sql
-- 最近 1 小时 relay 成功率
SELECT
  COUNT(*) FILTER (WHERE status = 'success') * 100.0 / NULLIF(COUNT(*), 0) AS success_rate,
  COUNT(*) AS total,
  AVG(response_time_ms) AS avg_latency_ms
FROM relay_logs
WHERE created_at > NOW() - INTERVAL '1 hour';

-- 最近 24 小时各渠道错误 Top 5
SELECT channel_id, error_code, COUNT(*) AS cnt
FROM relay_logs
WHERE status != 'success' AND created_at > NOW() - INTERVAL '24 hours'
GROUP BY channel_id, error_code
ORDER BY cnt DESC
LIMIT 5;
```

---

## 3. 监控工具推荐

### 方案 A：轻量免费（推荐当前自用阶段）

**Uptime Robot**（免费版支持 50 个监控）

- 类型：外部 HTTP 监控
- 配置：监控 `GET https://你的域名/api/v1/bootstrap/status`
- 间隔：5 分钟
- 通知：邮件 / Telegram / 微信（通过 Webhook）
- 优点：零部署、不占服务器资源、能检测"从外网访问不到"的问题

设置步骤：

1. 注册 [uptimerobot.com](https://uptimerobot.com)
2. 添加 Monitor → HTTP(s) → 填入健康检查 URL
3. 设置 Alert Contacts（邮件 / Telegram）
4. 可选：添加 Keyword Monitor，检查返回 JSON 中包含特定字段

### 方案 B：全栈自建（商业部署 / 多服务器时）

**Prometheus + Grafana**

部署在同服务器（2C2G 内存紧张时考虑放别的机器）：

```yaml
# docker-compose.monitoring.yml（追加到现有 compose 或独立部署）
services:
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./monitoring/prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus
    ports:
      - "9090:9090"
    restart: unless-stopped

  grafana:
    image: grafana/grafana:latest
    volumes:
      - grafana-data:/var/lib/grafana
    ports:
      - "3000:3000"
    environment:
      GF_SECURITY_ADMIN_PASSWORD: "${GRAFANA_ADMIN_PASSWORD}"
    restart: unless-stopped

  node-exporter:
    image: prom/node-exporter:latest
    pid: host
    volumes:
      - /proc:/host/proc:ro
      - /sys:/host/sys:ro
    command:
      - '--path.procfs=/host/proc'
      - '--path.sysfs=/host/sys'
    restart: unless-stopped

  cadvisor:
    image: gcr.io/cadvisor/cadvisor:latest
    volumes:
      - /:/rootfs:ro
      - /var/run:/var/run:ro
      - /sys:/sys:ro
      - /var/lib/docker/:/var/lib/docker:ro
    ports:
      - "8081:8080"
    restart: unless-stopped

volumes:
  prometheus-data:
  grafana-data:
```

Prometheus 配置（`monitoring/prometheus.yml`）：

```yaml
global:
  scrape_interval: 30s

scrape_configs:
  - job_name: 'node'
    static_configs:
      - targets: ['node-exporter:9100']

  - job_name: 'cadvisor'
    static_configs:
      - targets: ['cadvisor:8080']

  - job_name: 'lodestar'
    # 若 Lodestar 暴露 /metrics 端点（当前未实现，预留）
    metrics_path: /metrics
    static_configs:
      - targets: ['lodestar:8080']
```

### 方案 C：Server酱 / 钉钉 Webhook（最简告警）

写一个 cron 脚本，定时 curl 健康检查，失败时推消息：

```bash
#!/bin/bash
# /opt/lodestar/scripts/health-alert.sh
# crontab: */5 * * * * /opt/lodestar/scripts/health-alert.sh

URL="http://localhost:8080/api/v1/bootstrap/status"
MAX_RETRIES=3
FAIL_COUNT_FILE="/tmp/lodestar_health_fail_count"

count=$(cat "$FAIL_COUNT_FILE" 2>/dev/null || echo 0)

if ! curl -sf --max-time 5 "$URL" > /dev/null 2>&1; then
  count=$((count + 1))
  echo "$count" > "$FAIL_COUNT_FILE"

  if [ "$count" -ge "$MAX_RETRIES" ]; then
    # 推送告警（替换为你的 Server酱 / 钉钉 / 企业微信 Webhook）
    curl -s "https://sctapi.ftqq.com/YOUR_KEY.send" \
      -d "title=Lodestar 健康检查失败" \
      -d "desp=连续 ${count} 次健康检查失败，请检查容器状态"
    echo 0 > "$FAIL_COUNT_FILE"  # 重置，避免重复告警
  fi
else
  echo 0 > "$FAIL_COUNT_FILE"
fi
```

---

## 4. 告警规则

### 通用告警矩阵

| 级别 | 条件 | 动作 |
|------|------|------|
| **P0 紧急** | 健康检查连续失败 >= 3 次 | 立即通知（Telegram / 电话） |
| **P0 紧急** | 容器 OOM 被杀 | 立即通知 + 自动重启（`restart: unless-stopped` 已覆盖） |
| **P1 警告** | 内存使用 > 400 MB（限额 512 MB） | 通知 + 排查内存泄漏 |
| **P1 警告** | 错误率 > 5%（5 分钟窗口） | 通知 + 检查上游渠道 |
| **P1 警告** | 磁盘使用 > 80% | 通知 + 清理日志/旧数据 |
| **P2 关注** | Relay 成功率 < 90%（10 分钟窗口） | 记录 + 检查渠道健康 |
| **P2 关注** | P95 延迟 > 5 秒 | 记录 + 排查上游 |
| **P2 关注** | 容器重启 > 3 次/小时 | 通知 + 检查日志 |

### Prometheus Alertmanager 规则示例

```yaml
# monitoring/alerts.yml
groups:
  - name: lodestar
    rules:
      - alert: LodestarHealthCheckFailing
        expr: probe_success{job="lodestar"} == 0
        for: 3m
        labels:
          severity: critical
        annotations:
          summary: "Lodestar 健康检查持续失败"
          description: "健康检查已失败超过 3 分钟"

      - alert: LodestarHighMemory
        expr: container_memory_usage_bytes{name="lodestar"} / 1024 / 1024 > 400
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Lodestar 内存使用过高"
          description: "当前内存 {{ $value }}MB，限额 512MB"

      - alert: LodestarHighErrorRate
        expr: lodestar_relay_errors_rate_5m > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Lodestar relay 错误率过高"
          description: "5 分钟错误率 {{ $value | humanizePercentage }}"

      - alert: LodestarHighDiskUsage
        expr: (node_filesystem_avail_bytes{mountpoint="/opt/docker/lodestar/data"} / node_filesystem_size_bytes{mountpoint="/opt/docker/lodestar/data"}) < 0.2
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Lodestar data 目录磁盘空间不足"
          description: "剩余空间不足 20%"
```

---

## 5. 日志管理

### 当前日志架构

- **应用日志**：通过 `internal/utils/log/log.go`（基于 zap）输出到 stdout/stderr，Docker 捕获
- **Relay 日志**：写入数据库（可配置独立日志库 `LODESTAR_DATABASE_LOG_TYPE` / `_LOG_PATH`）

### 日志聚合建议

#### 方案 1：Docker 日志 + logrotate（推荐当前）

Docker 默认写 JSON 日志到 `/var/lib/docker/containers/`，配置 logrotate 限制大小：

```bash
# /etc/logrotate.d/docker-containers
/var/lib/docker/containers/*/*.log {
    daily
    rotate 7
    compress
    delaycompress
    missingok
    notifempty
    copytruncate
    maxsize 50M
}
```

也可在 docker-compose.yml 中限制日志大小：

```yaml
services:
  lodestar:
    logging:
      driver: json-file
      options:
        max-size: "20m"
        max-file: "5"
```

#### 方案 2：Loki + Promtail（配合 Grafana）

若已部署 Grafana，可加 Loki 做日志查询：

```yaml
# 追加到 docker-compose.monitoring.yml
  loki:
    image: grafana/loki:latest
    volumes:
      - loki-data:/loki
    ports:
      - "3100:3100"
    restart: unless-stopped

  promtail:
    image: grafana/promtail:latest
    volumes:
      - /var/log:/var/log:ro
      - /var/lib/docker/containers:/var/lib/docker/containers:ro
      - ./monitoring/promtail-config.yml:/etc/promtail/config.yml
    restart: unless-stopped
```

### 日志查看命令速查

```bash
# 实时查看容器日志
docker logs -f --tail 100 lodestar

# 查看最近 1 小时日志
docker logs --since 1h lodestar 2>&1 | tail -200

# 搜索错误
docker logs lodestar 2>&1 | grep -i "error\|panic\|fatal" | tail -50

# 查看容器资源使用
docker stats lodestar --no-stream

# 查看健康检查历史
docker inspect --format='{{json .State.Health}}' lodestar | python3 -m json.tool
```

---

## 6. 快速上手清单

自用阶段的最小监控组合：

- [x] 健康检查端点已就绪：`GET /api/v1/bootstrap/status`
- [x] Docker HEALTHCHECK 已配置（30s 间隔，3 次重试）
- [x] 容器内存限制已配置（`mem_limit: 512m`）
- [ ] 注册 Uptime Robot，添加外部 HTTP 监控
- [ ] 配置 logrotate 或 Docker 日志轮转
- [ ] 部署健康检查告警脚本（cron + Webhook 通知）
- [ ] （可选）部署 Prometheus + Grafana 全栈监控

---

## 7. 未来扩展

当 Lodestar 进入商业阶段时：

- [ ] 暴露 `/metrics` 端点（Prometheus 格式），导出 relay 成功率、延迟分位数等应用指标
- [ ] 接入 Grafana 告警 → Telegram / 钉钉 / PagerDuty
- [ ] 数据库慢查询日志 + PG `pg_stat_statements`
- [ ] Redis 监控（`redis-cli info` → Prometheus exporter）
- [ ] 分布式追踪（OpenTelemetry），按 request_id 串联 relay 全链路
