# Troubleshooting Guide

This guide covers common configuration and deployment issues based on real-world deployment experience.

## Quick Diagnosis

Run the built-in configuration validator before starting the server:

```bash
./lodestar validate
```

This will check:
- ✓ Encryption key is set and meets minimum length
- ✓ JWT secret is configured properly
- ✓ Data directory is writable
- ✓ Database connection is reachable
- ✓ Server port is available
- ✓ Trusted proxy configuration is valid

## Common Issues

### 1. Server Fails to Start: "invalid CIDR address"

**Symptom:**
```
Error: server.trusted_proxies[1] has leading/trailing whitespace (" 10.0.0.0/8")
```

**Cause:** Comma-separated list in environment variable has spaces after commas.

**Fix:**
```bash
# Wrong:
LODESTAR_SERVER_TRUSTED_PROXIES="127.0.0.1, 10.0.0.0/8"

# Correct:
LODESTAR_SERVER_TRUSTED_PROXIES="127.0.0.1,10.0.0.0/8"
```

Or use array notation in `config.json`:
```json
{
  "server": {
    "trusted_proxies": ["127.0.0.1", "10.0.0.0/8"]
  }
}
```

---

### 2. Permission Denied: Cannot Write to Data Directory

**Symptom:**
```
failed to create default config: permission denied
```

**Cause:** The `data/` directory was created by root, but the container runs as UID 1000.

**Fix:**

```bash
# Host machine:
sudo chown -R 1000:1000 ./data/

# Or in Docker Compose, use the provided deployment script:
./scripts/deploy.sh
```

The deployment script automatically creates the directory with correct ownership.

---

### 3. Port Already in Use

**Symptom:**
```
Error: port 8080 is not available: bind: address already in use
```

**Cause:** Another service (e.g., `sub2api`) is using port 8080.

**Fix:**

Check what's using the port:
```bash
# Linux:
netstat -tlnp | grep 8080
lsof -i:8080

# Windows:
netstat -ano | findstr :8080
```

Either stop the conflicting service or change Lodestar's port:
```bash
LODESTAR_SERVER_PORT=8081 ./lodestar start
```

---

### 4. Encryption Key Not Set

**Symptom:**
```
✗ Encryption key configured: security.encryption_key is not set
```

**Cause:** `LODESTAR_SECURITY_ENCRYPTION_KEY` environment variable is missing.

**Fix:**

Generate a strong key and set it:
```bash
# Generate a 32-character random key:
openssl rand -hex 16

# Set it permanently:
export LODESTAR_SECURITY_ENCRYPTION_KEY="your-generated-key-here"

# Or in Docker Compose:
# Add to .env file:
LODESTAR_SECURITY_ENCRYPTION_KEY=your-generated-key-here
```

**⚠️ Important:** Once set, **do not change this key**. All encrypted data (API keys, payment secrets) will become unreadable if the key changes.

---

### 5. Database Connection Failed

**Symptom:**
```
✗ Database connection: cannot connect to postgres database
```

**Cause:** Database connection string is incorrect or database is unreachable.

**Fix:**

For **PostgreSQL**, check the connection string format:
```bash
# Correct format (conninfo, not URL):
LODESTAR_DATABASE_PATH="host=localhost port=5432 dbname=lodestar user=lodestar password=yourpassword sslmode=disable"

# Common mistakes:
# ❌ Using URL format: postgresql://user:pass@host:5432/db
# ❌ Missing sslmode: will default to "require" and fail locally
```

For **SQLite**, ensure the parent directory exists and is writable:
```bash
mkdir -p ./data
chmod 755 ./data
```

---

### 6. JWT Tokens Not Persisting Across Restarts

**Symptom:**
```
WARN: auth.jwt_secret is empty, generated an ephemeral secret for this process
```

**Cause:** `LODESTAR_AUTH_JWT_SECRET` is not set, so a random secret is generated each time.

**Fix:**

Set a persistent secret:
```bash
# Generate a random secret:
openssl rand -base64 32

# Set it:
export LODESTAR_AUTH_JWT_SECRET="your-generated-secret-here"
```

Or add it to `config.json`:
```json
{
  "auth": {
    "jwt_secret": "your-generated-secret-here"
  }
}
```

---

### 7. High Memory Usage / Container OOM

**Symptom:** Docker container restarts frequently, `docker stats` shows high memory usage.

**Cause:** 
- Large relay request/response bodies cached in memory
- Many concurrent relay connections
- Memory not being released back to OS (Go runtime behavior)

**Fix:**

1. **Limit body sizes** in environment or config:
   ```bash
   LODESTAR_RELAY_MAX_JSON_BODY_BYTES=33554432      # 32 MB
   LODESTAR_RELAY_MAX_MULTIPART_BODY_BYTES=33554432 # 32 MB
   ```

2. **Enable external Redis cache** (reduces in-memory caching):
   ```bash
   LODESTAR_REDIS_HOST=localhost
   LODESTAR_REDIS_PORT=6379
   ```

3. **Set container memory limit** in `docker-compose.yml`:
   ```yaml
   services:
     lodestar:
       deploy:
         resources:
           limits:
             memory: 512M
   ```

---

### 8. GitHub Actions CI Failing: "go: unknown revision"

**Symptom:**
```
go: github.com/gypg/lodestar@v0.0.0-...: unknown revision
```

**Cause:** Dependencies in `go.mod` reference a commit that doesn't exist in the remote repository.

**Fix:**

```bash
go mod tidy
git add go.mod go.sum
git commit -m "chore: update dependencies"
git push
```

---

### 9. GHCR Image Pull Timeout

**Symptom:**
```
Error response from daemon: Get "https://ghcr.io/v2/": net/http: TLS handshake timeout
```

**Cause:** GHCR (GitHub Container Registry) is temporarily unreachable or rate-limited.

**Fix:**

Use the sharded pull script (retries across multiple mirrors):
```bash
./scripts/ghcr-pull-sharded.sh
```

Or manually retry with exponential backoff:
```bash
for i in {1..5}; do
  docker pull ghcr.io/gypg/lodestar:latest && break
  echo "Retry $i failed, waiting..."
  sleep $((2**i))
done
```

---

## Environment Variable Reference

All configuration can be set via environment variables with the `LODESTAR_` prefix:

| Environment Variable | Config Path | Default | Description |
|---------------------|-------------|---------|-------------|
| `LODESTAR_SERVER_HOST` | `server.host` | `0.0.0.0` | Listen address |
| `LODESTAR_SERVER_PORT` | `server.port` | `8080` | Listen port |
| `LODESTAR_SERVER_TRUSTED_PROXIES` | `server.trusted_proxies` | `["127.0.0.1", "10.0.0.0/8", ...]` | Comma-separated CIDRs |
| `LODESTAR_DATABASE_TYPE` | `database.type` | `sqlite` | `sqlite`, `postgres`, or `mysql` |
| `LODESTAR_DATABASE_PATH` | `database.path` | `./data/data.db` | SQLite path or conninfo |
| `LODESTAR_SECURITY_ENCRYPTION_KEY` | `security.encryption_key` | _(none)_ | **Required**, 32+ chars |
| `LODESTAR_AUTH_JWT_SECRET` | `auth.jwt_secret` | _(ephemeral)_ | Recommended to set |
| `LODESTAR_LOG_LEVEL` | `log.level` | `info` | `debug`, `info`, `warn`, `error` |
| `LODESTAR_REDIS_HOST` | `redis.host` | _(none)_ | Optional, enables Redis cache |

**Note:** Use `_` (underscore) to replace `.` (dot) in config paths for environment variables.

---

## Getting More Help

1. **Run the validator**: `./lodestar validate`
2. **Check logs**: `docker-compose logs -f lodestar`
3. **Enable debug logging**: `LODESTAR_LOG_LEVEL=debug`
4. **Check issues**: https://github.com/gypg/lodestar/issues

If you're reporting a bug, include:
- Output of `./lodestar validate`
- Relevant log excerpts (with secrets redacted)
- Environment (Docker version, OS, deployment method)
