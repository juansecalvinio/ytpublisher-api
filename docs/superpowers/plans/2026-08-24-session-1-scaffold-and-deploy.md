# Session 1: Scaffold, Config, Health Check, Postgres, Initial Migration, AWS Deploy — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the YTPublisher API project skeleton — Go module, config loading, HTTP router with a health check, a Postgres connection pool against Supabase, the first migration (`api_clients` table), and a first deploy of the running binary to a single AWS EC2 instance, reachable publicly.

**Architecture:** A single Go binary (`cmd/api`) wired from three small internal packages (`internal/config`, `internal/api`, `internal/storage`), each independently unit-tested. No business logic yet — this session only proves the skeleton runs locally and in production.

**Tech Stack:** Go 1.22+, `chi` (HTTP router), `pgx/v5` (Postgres driver), `goose` (migrations), Supabase (managed Postgres), AWS EC2 (single instance, systemd-managed process).

**Spec:** `docs/superpowers/specs/2026-08-24-ytpublisher-api-v1-design.md`

## Global Constraints

- Go version: 1.22+
- Module path: `github.com/juansecalvinio/ytpublisher-api`
- Architecture: modular monolith, single binary, no microservices
- HTTP router: `chi` (`github.com/go-chi/chi/v5`)
- Postgres access: `pgx/v5` (`github.com/jackc/pgx/v5`), no ORM
- Migrations: `goose` (`github.com/pressly/goose/v3`), plain SQL files
- No job queue in v1 — everything in this session is synchronous
- Hosting target for this session: a single AWS EC2 instance (no ECS/App Runner)

---

## Prerequisites (not part of the tasks — confirm before starting)

- Go 1.22+ installed locally: `go version`
- A Supabase project already created, with its Postgres connection string on hand (Supabase dashboard → Project Settings → Database → Connection string → URI format). It looks like:
  `postgres://postgres:<PASSWORD>@<PROJECT-REF>.supabase.co:5432/postgres?sslmode=require`
- An AWS account with AWS CLI v2 installed and configured (`aws configure`) using credentials that can create EC2 instances, key pairs, and security groups. Confirm a default region is set: `aws configure get region` (must print something, e.g. `us-east-1`).
- An SSH client available locally (default on macOS/Linux).

Keep your Supabase connection string handy — you'll export it as `DATABASE_URL` in Tasks 3, 4, and 5, but it must never be committed to git.

---

### Task 1: Project scaffold + config loading

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config{Port string, DatabaseURL string}`, `config.Load() (Config, error)`, `config.ErrMissingDatabaseURL` (sentinel error)

- [ ] **Step 1: Initialize the Go module and .gitignore**

```bash
go mod init github.com/juansecalvinio/ytpublisher-api
```

Create `.gitignore`:

```
/bin/
.env
*.local
```

- [ ] **Step 2: Write the failing tests**

```go
// internal/config/config_test.go
package config

import (
	"errors"
	"testing"
)

func TestLoad_UsesDefaultPortWhenUnset(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
}

func TestLoad_ReadsCustomPort(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
}

func TestLoad_ReadsDatabaseURL(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://user:pass@host:5432/db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://user:pass@host:5432/db" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://user:pass@host:5432/db")
	}
}

func TestLoad_ErrorsWhenDatabaseURLMissing(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingDatabaseURL) {
		t.Errorf("err = %v, want ErrMissingDatabaseURL", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/config/...`
Expected: FAIL — compile error, `config.Load` and `config.ErrMissingDatabaseURL` undefined.

- [ ] **Step 4: Write the minimal implementation**

```go
// internal/config/config.go
package config

import (
	"errors"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
}

var ErrMissingDatabaseURL = errors.New("config: DATABASE_URL is required")

func Load() (Config, error) {
	cfg := Config{
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.DatabaseURL == "" {
		return Config{}, ErrMissingDatabaseURL
	}
	return cfg, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: all 4 tests PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod .gitignore internal/config
git commit -m "feat: add config loading from environment"
```

---

### Task 2: HTTP router with health check

**Files:**
- Create: `internal/api/router.go`
- Test: `internal/api/router_test.go`

**Interfaces:**
- Produces: `api.NewRouter() *chi.Mux`

- [ ] **Step 1: Add the chi dependency**

```bash
go get github.com/go-chi/chi/v5@latest
```

- [ ] **Step 2: Write the failing test**

```go
// internal/api/router_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	NewRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/api/...`
Expected: FAIL — compile error, `NewRouter` undefined.

- [ ] **Step 4: Write the minimal implementation**

```go
// internal/api/router.go
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter() *chi.Mux {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)
	return r
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api go.mod go.sum
git commit -m "feat: add health check endpoint"
```

---

### Task 3: Postgres connection pool

**Files:**
- Create: `internal/storage/db.go`
- Test: `internal/storage/db_test.go`

**Interfaces:**
- Produces: `storage.NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)`

- [ ] **Step 1: Add the pgx dependency**

```bash
go get github.com/jackc/pgx/v5/pgxpool@latest
```

- [ ] **Step 2: Write the failing tests**

```go
// internal/storage/db_test.go
package storage

import (
	"context"
	"os"
	"testing"
)

func TestNewPool_ConnectsAndPings(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test against Supabase")
	}

	pool, err := NewPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		t.Errorf("Ping() after NewPool() returned error: %v", err)
	}
}

func TestNewPool_ReturnsErrorForInvalidConfig(t *testing.T) {
	_, err := NewPool(context.Background(), "postgres://user:pass@localhost:5432/db?sslmode=notarealmode")
	if err == nil {
		t.Error("expected error for invalid sslmode, got nil")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/storage/...`
Expected: FAIL — compile error, `NewPool` undefined.

- [ ] **Step 4: Write the minimal implementation**

```go
// internal/storage/db.go
package storage

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/storage/... -v`
Expected: `TestNewPool_ReturnsErrorForInvalidConfig` PASSes. `TestNewPool_ConnectsAndPings` SKIPs unless `DATABASE_URL` is exported.

- [ ] **Step 6: Run the real integration test against Supabase**

```bash
export DATABASE_URL="<your supabase connection string>"
go test ./internal/storage/... -v -run TestNewPool_ConnectsAndPings
```

Expected: PASS (proves the Supabase project and connection string are correct before wiring `main.go`).

- [ ] **Step 7: Commit**

```bash
git add internal/storage go.mod go.sum
git commit -m "feat: add Postgres connection pool"
```

---

### Task 4: Wire main.go

**Files:**
- Create: `cmd/api/main.go`

**Interfaces:**
- Consumes: `config.Load() (config.Config, error)`, `storage.NewPool(ctx, databaseURL) (*pgxpool.Pool, error)`, `api.NewRouter() *chi.Mux`

- [ ] **Step 1: Write main.go**

```go
// cmd/api/main.go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/api"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := storage.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer pool.Close()
	log.Println("connected to database")

	router := api.NewRouter()

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./...`
Expected: no errors, no output

- [ ] **Step 3: Run it locally**

```bash
export PORT=8080
export DATABASE_URL="<your supabase connection string>"
go run ./cmd/api
```

Expected log output: `connected to database` then `listening on :8080`

- [ ] **Step 4: Verify the health check from another terminal**

```bash
curl -i http://localhost:8080/healthz
```

Expected: `HTTP/1.1 200 OK` with body `ok`. Stop the server (Ctrl+C) after confirming.

- [ ] **Step 5: Commit**

```bash
git add cmd/api
git commit -m "feat: wire config, storage, and router into main"
```

---

### Task 5: First migration — `api_clients` table

**Files:**
- Create: `migrations/00001_create_api_clients.sql`

**Interfaces:**
- Produces: `api_clients` table in the Supabase Postgres database (columns: `id`, `name`, `email`, `api_key_hash`, `stripe_customer_id`, `is_active`, `created_at`)

- [ ] **Step 1: Install the goose CLI**

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
goose --version
```

Expected: prints a goose version string.

- [ ] **Step 2: Write the migration**

```sql
-- migrations/00001_create_api_clients.sql

-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE api_clients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    email TEXT NOT NULL,
    api_key_hash TEXT NOT NULL UNIQUE,
    stripe_customer_id TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE api_clients;
```

- [ ] **Step 3: Apply the migration to Supabase**

```bash
export DATABASE_URL="<your supabase connection string>"
goose -dir migrations postgres "$DATABASE_URL" up
```

Expected output: `OK   00001_create_api_clients.sql`

- [ ] **Step 4: Verify the table and migration status**

```bash
goose -dir migrations postgres "$DATABASE_URL" status
```

Expected: shows `00001_create_api_clients.sql` with an "Applied" timestamp.

```bash
psql "$DATABASE_URL" -c "\d api_clients"
```

Expected: lists the 7 columns defined above.

- [ ] **Step 5: Commit**

```bash
git add migrations
git commit -m "feat: add initial migration for api_clients table"
```

---

### Task 6: Provision an AWS EC2 instance

**Files:** none (infrastructure only — no files change in this repo)

**Interfaces:**
- Produces: a running EC2 instance reachable over SSH at `$PUBLIC_IP`, using the key at `~/.ssh/ytpublisher-api.pem` — consumed by Task 7

- [ ] **Step 1: Confirm AWS CLI access and region**

```bash
aws sts get-caller-identity
aws configure get region
```

Expected: JSON with your `Account`/`Arn`, and a non-empty region string.

- [ ] **Step 2: Create an SSH key pair**

```bash
aws ec2 create-key-pair --key-name ytpublisher-api \
  --query 'KeyMaterial' --output text > ~/.ssh/ytpublisher-api.pem
chmod 400 ~/.ssh/ytpublisher-api.pem
```

- [ ] **Step 3: Create a security group allowing SSH (from your IP) and the app port (8080, public)**

```bash
VPC_ID=$(aws ec2 describe-vpcs --filters Name=isDefault,Values=true \
  --query 'Vpcs[0].VpcId' --output text)

SG_ID=$(aws ec2 create-security-group \
  --group-name ytpublisher-api-sg \
  --description "YTPublisher API session 1 deploy" \
  --vpc-id "$VPC_ID" \
  --query 'GroupId' --output text)

MY_IP=$(curl -s https://checkip.amazonaws.com)

aws ec2 authorize-security-group-ingress --group-id "$SG_ID" \
  --protocol tcp --port 22 --cidr "${MY_IP}/32"

aws ec2 authorize-security-group-ingress --group-id "$SG_ID" \
  --protocol tcp --port 8080 --cidr 0.0.0.0/0

echo "Security group: $SG_ID"
```

- [ ] **Step 4: Launch the instance**

```bash
AMI_ID=$(aws ec2 describe-images \
  --owners amazon \
  --filters "Name=name,Values=al2023-ami-*-x86_64" \
            "Name=state,Values=available" \
            "Name=architecture,Values=x86_64" \
  --query 'sort_by(Images, &CreationDate)[-1].ImageId' --output text)

INSTANCE_ID=$(aws ec2 run-instances \
  --image-id "$AMI_ID" \
  --instance-type t3.micro \
  --key-name ytpublisher-api \
  --security-group-ids "$SG_ID" \
  --query 'Instances[0].InstanceId' --output text)

aws ec2 wait instance-running --instance-ids "$INSTANCE_ID"

PUBLIC_IP=$(aws ec2 describe-instances --instance-ids "$INSTANCE_ID" \
  --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)

echo "Instance $INSTANCE_ID running at $PUBLIC_IP"
```

- [ ] **Step 5: Verify SSH connectivity**

```bash
ssh -i ~/.ssh/ytpublisher-api.pem -o StrictHostKeyChecking=accept-new \
  ec2-user@$PUBLIC_IP "echo connected"
```

Expected: prints `connected`. If it says "Connection refused", wait ~30s and retry — the instance may still be booting.

- [ ] **Step 6: Record the values for Task 7**

Note down `$PUBLIC_IP` (and optionally `$INSTANCE_ID`) — you'll pass the IP into the deploy script next. No git commit for this task; nothing in the repo changed.

---

### Task 7: Deploy the binary and verify it's publicly reachable

**Files:**
- Create: `deploy/ytpublisher-api.service`
- Create: `deploy/deploy.sh`

**Interfaces:**
- Consumes: `$PUBLIC_IP` and the SSH key from Task 6; the buildable binary from `cmd/api` (Task 4)
- Produces: `http://$PUBLIC_IP:8080/healthz` reachable publicly, returning `ok`

- [ ] **Step 1: Write the systemd unit file**

```ini
# deploy/ytpublisher-api.service
[Unit]
Description=YTPublisher API
After=network.target

[Service]
ExecStart=/opt/ytpublisher-api/ytpublisher-api
Restart=on-failure
User=ec2-user
EnvironmentFile=/opt/ytpublisher-api/.env
WorkingDirectory=/opt/ytpublisher-api

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 2: Write the deploy script**

```bash
#!/usr/bin/env bash
# deploy/deploy.sh
set -euo pipefail

PUBLIC_IP="$1"
KEY_PATH="${KEY_PATH:-$HOME/.ssh/ytpublisher-api.pem}"

echo "Building linux/amd64 binary..."
GOOS=linux GOARCH=amd64 go build -o bin/ytpublisher-api ./cmd/api

echo "Preparing remote directory..."
ssh -i "$KEY_PATH" "ec2-user@$PUBLIC_IP" \
  "sudo mkdir -p /opt/ytpublisher-api && sudo chown ec2-user:ec2-user /opt/ytpublisher-api"

echo "Copying binary and service file..."
scp -i "$KEY_PATH" bin/ytpublisher-api "ec2-user@$PUBLIC_IP:/opt/ytpublisher-api/ytpublisher-api"
scp -i "$KEY_PATH" deploy/ytpublisher-api.service "ec2-user@$PUBLIC_IP:/tmp/ytpublisher-api.service"

echo "Installing systemd service..."
ssh -i "$KEY_PATH" "ec2-user@$PUBLIC_IP" \
  "sudo mv /tmp/ytpublisher-api.service /etc/systemd/system/ytpublisher-api.service && \
   sudo systemctl daemon-reload && \
   sudo systemctl enable ytpublisher-api && \
   sudo systemctl restart ytpublisher-api"

echo "Waiting for the service to come up..."
sleep 2
curl -sf "http://$PUBLIC_IP:8080/healthz" && echo " -> healthz OK"
```

```bash
chmod +x deploy/deploy.sh
```

- [ ] **Step 3: Create the remote secret env file (one-time, manual, not committed)**

```bash
ssh -i ~/.ssh/ytpublisher-api.pem ec2-user@$PUBLIC_IP \
  "sudo mkdir -p /opt/ytpublisher-api && \
   printf 'DATABASE_URL=%s\nPORT=8080\n' '<your supabase connection string>' | sudo tee /opt/ytpublisher-api/.env > /dev/null"
```

- [ ] **Step 4: Run the deploy script**

```bash
KEY_PATH=~/.ssh/ytpublisher-api.pem ./deploy/deploy.sh $PUBLIC_IP
```

Expected: ends with `-> healthz OK`

- [ ] **Step 5: Verify publicly from your machine**

```bash
curl -i http://$PUBLIC_IP:8080/healthz
```

Expected: `HTTP/1.1 200 OK` with body `ok`

- [ ] **Step 6: Commit**

```bash
git add deploy/ytpublisher-api.service deploy/deploy.sh
git commit -m "feat: add systemd deploy for EC2"
```

---

## Session 1 done when

- `go test ./...` passes locally (with `DATABASE_URL` exported for the Supabase integration test)
- `go run ./cmd/api` serves `/healthz` locally
- `api_clients` table exists in Supabase
- `curl http://$PUBLIC_IP:8080/healthz` returns `200 ok` from your own machine, proving the binary runs unattended on EC2
