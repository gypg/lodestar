# Lodestar 灾难恢复手册（DISASTER-RECOVERY）

> Last updated: 2026-06-25
>
> 本文档定义 Lodestar 服务的灾难恢复目标、操作流程和常见故障处理方案。
> 与 [BACKUP.md](BACKUP.md)（备份策略）和 [DEPLOY.md](DEPLOY.md)（部署指南）配合使用。

---

## 1. RTO / RPO 目标

| 指标 | 目标值 | 说明 |
|------|--------|------|
| **RTO**（恢复时间目标） | < 1 小时 | 从发现故障到服务恢复正常运行的最长时间 |
| **RPO**（恢复点目标） | < 24 小时 | 可接受的最大数据丢失窗口（与每日 03:00 自动备份对齐） |

**实际恢复时间预估**：

| 场景 | 预计耗时 |
|------|----------|
| 容器崩溃/重启 | < 2 分钟 |
| 数据库恢复（SQLite cp） | < 5 分钟 |
| 数据库恢复（PG pg_restore） | < 10 分钟 |
| 全栈重建（容器 + 数据库 + 密钥） | 20-40 分钟 |
| 服务器迁移（新机器从零部署） | 30-60 分钟 |

---

## 2. 备份恢复流程

> 详细备份策略见 [BACKUP.md](BACKUP.md)。

### 2.1 确认备份可用

恢复前，先确认最近的备份文件完好：

```bash
# 列出备份文件（按时间排序，最新的在最后）
ls -lt data/backup/

# 验证 SQLite 备份完整性
sqlite3 data/backup/data-YYYYMMDD_HHMMSS.db "PRAGMA integrity_check;"
# 预期输出：ok

# 验证 PostgreSQL 备份
pg_restore -l data/backup/lodestar-YYYYMMDD_HHMMSS.dump | head -20
```

### 2.2 选择恢复点

- 优先选择故障前最近的、完整性校验通过的备份
- 若最新备份不可用，逐级回退到更早的备份
- 若使用远程备份（rclone/S3），先拉取到本地再恢复

---

## 3. Docker 容器重建步骤

### 3.1 容器崩溃（最常见）

```bash
# 检查容器状态
docker ps -a --filter name=lodestar

# 查看崩溃日志（最近 50 行）
docker logs --tail 50 lodestar

# 重启容器
docker compose -f /opt/docker/lodestar/docker-compose.yml up -d
```

### 3.2 完整容器重建

当容器无法恢复时，从零重建：

```bash
cd /opt/docker/lodestar

# 1. 停止并移除旧容器
docker compose down

# 2. 确认 .env 存在（密钥不可丢失，见第 5 节）
cat .env | head -5

# 3. 拉取镜像（可指定版本回滚）
docker compose pull
# 回滚到特定版本：
#   编辑 docker-compose.yml，将 image tag 改为 sha-<commit>
#   例如：image: ghcr.io/gypg/lodestar:sha-f51451d0

# 4. 启动容器
docker compose up -d

# 5. 验证健康
docker ps --filter name=lodestar
curl -s http://localhost:8081/api/v1/bootstrap/status
# 预期：{"status":"ok"} 或类似成功响应
```

### 3.3 镜像拉取慢（国内服务器）

```bash
# 使用加速源
docker pull ghcr.chenby.cn/gypg/lodestar:latest
# 拉取后需重新 tag
docker tag ghcr.chenby.cn/gypg/lodestar:latest ghcr.io/gypg/lodestar:latest
```

---

## 4. 数据库恢复步骤

### 4.1 SQLite 恢复

```bash
cd /opt/docker/lodestar

# 1. 停止服务（防止写入冲突）
docker compose stop lodestar

# 2. 备份当前损坏的数据库（留作诊断）
mv data/data.db data/data.db.corrupted-$(date +%Y%m%d_%H%M%S)

# 3. 从备份恢复
cp data/backup/data-YYYYMMDD_HHMMSS.db data/data.db

# 4. 验证完整性
sqlite3 data/data.db "PRAGMA integrity_check;"
# 预期输出：ok

# 5. 检查数据量是否合理
sqlite3 data/data.db "SELECT COUNT(*) FROM users;"
sqlite3 data/data.db "SELECT COUNT(*) FROM channels;"

# 6. 重启服务
docker compose start lodestar

# 7. 验证服务正常
curl -s http://localhost:8081/api/v1/bootstrap/status
```

### 4.2 PostgreSQL 恢复

```bash
# 1. 停止 Lodestar 服务
docker compose stop lodestar

# 2. 删除现有数据并重建（危险操作，确认后再执行）
psql -h 172.16.0.87 -U admin -c "DROP DATABASE IF EXISTS lodestar;"
psql -h 172.16.0.87 -U admin -c "CREATE DATABASE lodestar OWNER admin;"

# 3. 从 dump 恢复
pg_restore -h 172.16.0.87 -U admin -d lodestar \
  --clean --if-exists \
  data/backup/lodestar-YYYYMMDD_HHMMSS.dump

# 4. 验证
psql -h 172.16.0.87 -U admin -d lodestar -c "SELECT COUNT(*) FROM users;"
psql -h 172.16.0.87 -U admin -d lodestar -c "\dt"

# 5. 重启服务
docker compose start lodestar

# 6. 验证服务正常
curl -s http://localhost:8081/api/v1/bootstrap/status
```

### 4.3 PostgreSQL 完全不可用

如果 PostgreSQL 服务器本身故障，临时回退到 SQLite：

```bash
# 1. 编辑 .env，切换数据库类型
# LODESTAR_DATABASE_TYPE=postgres  -->  LODESTAR_DATABASE_TYPE=sqlite
# 注释掉 LODESTAR_DATABASE_PATH 的 PG 连接串

# 2. 重启（Lodestar 会自动用 SQLite 初始化新库）
docker compose up -d

# 3. 登录 UI，手动重新配置渠道和用户（当前数据规模极小，成本约 0）

# 4. PG 恢复后再切回
```

---

## 5. 密钥恢复

### 关键密钥清单

| 密钥 | 环境变量 | 丢失后果 |
|------|----------|----------|
| JWT 签名密钥 | `LODESTAR_AUTH_JWT_SECRET` | 全员掉登录（但可重新生成，不影响数据） |
| 加密密钥 | `LODESTAR_SECURITY_ENCRYPTION_KEY` | **所有渠道 API key 无法解密，数据报废** |
| PG 密码 | PG 连接串中的 `password` | 无法连接数据库 |

### !! ENCRYPTION_KEY 不可丢失 !!

> `LODESTAR_SECURITY_ENCRYPTION_KEY` 一旦有加密数据后**不可更改**。
> 丢失或篡改将导致所有已加密的渠道 API key 永久无法解密。
> 这是唯一的不可恢复项——数据库可以重建，密钥丢了就是丢了。

### 密钥备份位置

密钥存储在 `/opt/docker/lodestar/.env`，应同时备份到以下位置：

1. **本地离线备份**：U盘/移动硬盘上的 `.env` 副本
2. **密码管理器**：KeePass / Bitwarden 中存储三个关键值
3. **异地备份**：另一台机器上的加密副本

### 密钥恢复步骤

```bash
# 1. 从备份恢复 .env 文件
cp /path/to/backup/.env /opt/docker/lodestar/.env

# 2. 确认文件权限（密钥文件不应公开可读）
chmod 600 /opt/docker/lodestar/.env

# 3. 重启服务
cd /opt/docker/lodestar
docker compose up -d
```

### 密钥丢失后的应急处理

| 密钥 | 处理方式 |
|------|----------|
| `LODESTAR_AUTH_JWT_SECRET` | 生成新的：`openssl rand -hex 32`，填入 .env，重启。代价：全员重新登录 |
| `LODESTAR_SECURITY_ENCRYPTION_KEY` | **如果丢失且已有加密数据，无法恢复。** 只能删除所有渠道、生成新 key、重新配置渠道 API key |
| PG 密码 | 修改 PG 角色密码后更新 .env，重启 |

---

## 6. 常见故障场景及处理

### 6.1 容器崩溃

**症状**：`docker ps` 显示 lodestar 容器 `Exited` 或 `Restarting`

**诊断**：
```bash
# 查看退出码和日志
docker inspect lodestar --format='{{.State.ExitCode}}'
docker logs --tail 100 lodestar
```

**处理**：
```bash
# 通常重启即可
docker compose -f /opt/docker/lodestar/docker-compose.yml restart lodestar

# 如果反复崩溃，尝试拉取最新镜像
docker compose pull && docker compose up -d

# 如果最新镜像也有问题，回滚到上一个已知正常版本
# 编辑 docker-compose.yml 中的 image tag 为 sha-<previous>
docker compose up -d
```

### 6.2 磁盘满

**症状**：容器无法启动、日志写入失败、`df -h` 显示 100%

**诊断**：
```bash
df -h
du -sh /opt/docker/lodestar/data/*
du -sh /var/lib/docker/
docker system df
```

**处理**：
```bash
# 1. 清理旧日志（最常见的大文件来源）
# relay 日志通常在数据库中，清理方式：
#   - 进入 UI 设置，关闭/调整日志保留策略
#   - 或手动清理 PG/SQLite 中的 logs 表

# 2. 清理 Docker 悬空资源
docker system prune -f

# 3. 清理旧备份
find /opt/docker/lodestar/data/backup -name "*.db" -mtime +7 -delete
find /opt/docker/lodestar/data/backup -name "*.dump" -mtime +7 -delete

# 4. 清理旧 Docker 镜像
docker image prune -f

# 5. 如果 PG 日志库独立（LODESTAR_DATABASE_LOG_*），可直接 drop + recreate
psql -h 172.16.0.87 -U admin -c "DROP DATABASE IF EXISTS lodestar_log;"
psql -h 172.16.0.87 -U admin -c "CREATE DATABASE lodestar_log OWNER admin;"
```

### 6.3 网络中断

**症状**：无法访问 `lodestar.ggznb.xyz`，但容器仍在运行

**诊断**：
```bash
# 1. 确认容器运行正常
docker ps --filter name=lodestar
curl -s http://localhost:8081/api/v1/bootstrap/status

# 2. 检查端口监听
ss -tlnp | grep 8081

# 3. 检查防火墙/安全组
iptables -L -n | grep 8081

# 4. 检查 Cloudflare 隧道状态（如使用 CF Tunnel）
# 在 Cloudflare Zero Trust Dashboard 检查 tunnel 状态
```

**处理**：

| 层级 | 检查项 | 修复方式 |
|------|--------|----------|
| 容器 | 端口映射正确 | `docker compose down && docker compose up -d` |
| 主机 | 防火墙放行 8081 | `ufw allow 8081` 或 `iptables` 规则 |
| 隧道 | Cloudflare Tunnel 断开 | 重启 tunnel 进程或检查 token |
| DNS | 域名解析异常 | 检查 CF DNS 记录指向 |

### 6.4 PostgreSQL 连接失败

**症状**：日志报错 `connection refused` 或 `FATAL: too many connections`

**处理**：
```bash
# 1. 确认 PG 进程运行
systemctl status postgresql
# 或
ps aux | grep postgres

# 2. 确认网络可达
pg_isready -h 172.16.0.87 -p 5432

# 3. 如果连接数耗尽
psql -h 172.16.0.87 -U admin -c "SELECT count(*) FROM pg_stat_activity;"
# 清理空闲连接
psql -h 172.16.0.87 -U admin -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE state = 'idle' AND query_start < now() - interval '10 minutes';"

# 4. 临时方案：切换到 SQLite（见 4.3 节）
```

### 6.5 Redis 不可用

**症状**：缓存命中率下降，但服务不中断（Redis 是可选组件）

**处理**：
```bash
# 1. 确认 Redis 状态
redis-cli -h 172.16.0.87 -n 2 ping
# 预期：PONG

# 2. 如果 Redis 不可用，Lodestar 会自动回落到内存缓存
# 无需干预，但多实例场景下需恢复 Redis

# 3. 重启 Redis
systemctl restart redis
```

### 6.6 服务器硬件故障 / 迁移

**完全重建步骤**（预计 30-60 分钟）：

```bash
# 1. 新服务器准备
#    - 安装 Docker + Docker Compose
#    - 恢复 .env 密钥文件（从离线备份）

# 2. 部署目录
mkdir -p /opt/docker/lodestar/data/backup
cd /opt/docker/lodestar

# 3. 恢复配置文件
cp /path/to/backup/.env /opt/docker/lodestar/.env
cp /path/to/backup/docker-compose.yml /opt/docker/lodestar/docker-compose.yml

# 4. 恢复数据库
#    SQLite：直接 cp 备份文件到 data/data.db
cp /path/to/backup/data-YYYYMMDD_HHMMSS.db data/data.db
#    PostgreSQL：需要先恢复 PG 服务，再 pg_restore

# 5. 拉取镜像并启动
docker compose pull
docker compose up -d

# 6. 验证
curl -s http://localhost:8081/api/v1/bootstrap/status
docker logs --tail 20 lodestar

# 7. 更新 DNS / Cloudflare Tunnel 指向新服务器 IP
```

---

## 7. 恢复验证清单

每次灾难恢复完成后，逐项确认：

- [ ] 容器运行正常：`docker ps --filter name=lodestar`
- [ ] 健康检查通过：`curl http://localhost:8081/api/v1/bootstrap/status`
- [ ] 管理员可登录
- [ ] API 端点可访问：`curl -H "Authorization: Bearer <token>" http://localhost:8081/api/v1/models`
- [ ] 渠道连通性正常（任选一个渠道测试）
- [ ] 数据完整性：用户数、渠道数与恢复前一致
- [ ] Cloudflare Tunnel / DNS 指向正确
- [ ] 日志正常写入
- [ ] 下次备份已计划执行

---

## 8. 联系信息

> 请填写以下信息，便于紧急情况下快速联络。

| 角色 | 姓名 | 电话 | 邮箱 | 备注 |
|------|------|------|------|------|
| 运维负责人 | __________ | __________ | __________ | 主要联系人 |
| 开发负责人 | __________ | __________ | __________ | 代码 / 配置问题 |
| 服务器管理员 | __________ | __________ | __________ | 服务器 / 网络问题 |
| 域名/DNS 管理 | __________ | __________ | __________ | Cloudflare / DNS |

### 关键服务地址

| 服务 | 地址 | 备注 |
|------|------|------|
| Lodestar 前台 | https://lodestar.ggznb.xyz | Cloudflare Tunnel |
| 服务器 SSH | 5508.axzt.top:61225 | 用户 aiagent |
| 服务器部署目录 | /opt/docker/lodestar/ | .env + docker-compose.yml + data/ |
| PostgreSQL | 172.16.0.87:5432 | 库 lodestar |
| Redis | 172.16.0.87:6379/db=2 | 可选，回落内存缓存 |
| Docker 镜像 | ghcr.io/gypg/lodestar | CI 构建 |
| GitHub 仓库 | github.com/gypg/lodestar | 源码 |

---

## Checklist

- [ ] 本文档已审阅并确认准确
- [ ] `.env` 文件已备份到至少 2 个异地位置
- [ ] `ENCRYPTION_KEY` 已单独备份到密码管理器
- [ ] 自动备份 cron 已配置且运行正常（见 BACKUP.md）
- [ ] 至少执行过一次恢复演练
- [ ] 联系信息已填写
