# N-14 Database Backup Strategy

> Last updated: 2026-06-25

## 1. SQLite Backup (Default)

Lodestar uses SQLite by default, stored at `data/data.db`.

### Manual Backup

```bash
# Create timestamped backup
cp data/data.db data/backup/data-$(date +%Y%m%d_%H%M%S).db

# Verify copy integrity
sqlite3 data/backup/data-$(date +%Y%m%d_%H%M%S).db "PRAGMA integrity_check;"
```

### Hot Backup (Without Stopping Service)

SQLite supports online backup via the `.backup` command, which avoids locking issues:

```bash
mkdir -p data/backup
sqlite3 data/data.db ".backup 'data/backup/data-$(date +%Y%m%d_%H%M%S).db'"
```

## 2. PostgreSQL Backup (Optional)

If Lodestar is configured with PostgreSQL via `SQL_DSN`, use `pg_dump`:

```bash
# Full SQL dump
pg_dump -h "$PG_HOST" -U "$PG_USER" -d "$PG_DB" \
  --format=custom \
  --compress=9 \
  -f "data/backup/lodestar-$(date +%Y%m%d_%H%M%S).dump"
```

### Connection Parameters

Set via environment or `.env`:

```bash
PG_HOST=localhost
PG_USER=lodestar
PG_DB=lodestar
# Password via ~/.pgpass or PGPASSWORD env var
```

## 3. Automated Backup Script

Save as `scripts/backup.sh` and make executable (`chmod +x`):

```bash
#!/usr/bin/env bash
# Lodestar Automated Backup — retains 7 daily copies
set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-data/backup}"
DB_PATH="${DB_PATH:-data/data.db}"
KEEP_DAYS=7
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

mkdir -p "$BACKUP_DIR"

# --- SQLite ---
if [ -f "$DB_PATH" ]; then
  DEST="$BACKUP_DIR/data-${TIMESTAMP}.db"
  sqlite3 "$DB_PATH" ".backup '${DEST}'"
  echo "[backup] SQLite -> ${DEST}  ($(du -h "$DEST" | cut -f1))"
else
  echo "[backup] WARNING: $DB_PATH not found, skipping SQLite backup"
fi

# --- PostgreSQL (optional, runs only if SQL_DSN is set) ---
if [ -n "${SQL_DSN:-}" ] && [ -n "${PG_HOST:-}" ]; then
  PG_DEST="$BACKUP_DIR/lodestar-${TIMESTAMP}.dump"
  pg_dump -h "$PG_HOST" -U "${PG_USER:-lodestar}" -d "${PG_DB:-lodestar}" \
    --format=custom --compress=9 -f "$PG_DEST"
  echo "[backup] PostgreSQL -> ${PG_DEST}  ($(du -h "$PG_DEST" | cut -f1))"
fi

# --- Retention: delete backups older than KEEP_DAYS ---
find "$BACKUP_DIR" -name "data-*.db" -mtime +${KEEP_DAYS} -delete 2>/dev/null || true
find "$BACKUP_DIR" -name "lodestar-*.dump" -mtime +${KEEP_DAYS} -delete 2>/dev/null || true
echo "[backup] Cleaned backups older than ${KEEP_DAYS} days"
```

### Cron Setup (Daily at 03:00)

```bash
# Edit crontab
crontab -e

# Add this line (adjust path to your Lodestar root):
0 3 * * *  cd /path/to/lodestar && bash scripts/backup.sh >> data/backup/backup.log 2>&1
```

### Systemd Timer Alternative

For systemd-based systems, create `/etc/systemd/system/lodestar-backup.timer`:

```ini
[Unit]
Description=Lodestar DB Backup Schedule

[Timer]
OnCalendar=*-*-* 03:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

And `/etc/systemd/system/lodestar-backup.service`:

```ini
[Unit]
Description=Lodestar DB Backup

[Service]
Type=oneshot
WorkingDirectory=/path/to/lodestar
ExecStart=/bin/bash scripts/backup.sh
```

Enable: `systemctl enable --now lodestar-backup.timer`

## 4. Recovery Steps

### SQLite Restore

```bash
# 1. Stop Lodestar service
pkill lodestar   # or systemctl stop lodestar

# 2. Replace the database file
cp data/backup/data-20260625_030000.db data/data.db

# 3. Verify integrity
sqlite3 data/data.db "PRAGMA integrity_check;"
# Expected output: ok

# 4. Restart Lodestar
./lodestar
```

### PostgreSQL Restore

```bash
# 1. Stop Lodestar service

# 2. Restore from dump
pg_restore -h "$PG_HOST" -U "$PG_USER" -d "$PG_DB" \
  --clean --if-exists \
  data/backup/lodestar-20260625_030000.dump

# 3. Restart Lodestar
```

## 5. Verify Backup Integrity

### SQLite

```bash
# Integrity check
sqlite3 data/backup/data-YYYYMMDD_HHMMSS.db "PRAGMA integrity_check;"
# Must output: ok

# Row count comparison (should match production)
sqlite3 data/data.db "SELECT COUNT(*) FROM users;"
sqlite3 data/backup/data-YYYYMMDD_HHMMSS.db "SELECT COUNT(*) FROM users;"

# Quick schema diff
sqlite3 data/data.db ".schema" > /tmp/schema_prod.sql
sqlite3 data/backup/data-YYYYMMDD_HHMMSS.db ".schema" > /tmp/schema_bak.sql
diff /tmp/schema_prod.sql /tmp/schema_bak.sql
# Should produce no output
```

### PostgreSQL

```bash
# List tables to confirm completeness
pg_restore -l data/backup/lodestar-YYYYMMDD_HHMMSS.dump | head -20

# Test restore to a temporary database
createdb lodestar_verify
pg_restore -d lodestar_verify data/backup/lodestar-YYYYMMDD_HHMMSS.dump
psql -d lodestar_verify -c "SELECT COUNT(*) FROM users;"
dropdb lodestar_verify
```

## 6. Backup Storage Recommendations

| Layer | Location | Retention | Notes |
|-------|----------|-----------|-------|
| Local | `data/backup/` | 7 days | Automated via cron |
| Remote | Off-site / S3 / NAS | 30 days | Manual or rclone sync |
| Cold | Offline archive | 90 days | Monthly snapshot |

### Remote Sync Example (rclone)

```bash
# One-time setup
rclone config create backup s3 provider=Other endpoint=https://your-s3-endpoint

# Add to cron after local backup
0 4 * * *  rclone sync /path/to/lodestar/data/backup/ backup:lodestar-backups/ --log-file=data/backup/rclone.log
```

## Checklist

- [ ] `scripts/backup.sh` created and executable
- [ ] Cron or systemd timer configured
- [ ] First manual backup completed and verified
- [ ] Recovery tested at least once on a staging copy
- [ ] Remote backup destination configured (recommended)
