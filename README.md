# TransitHub

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](https://golang.org/)
[![Vue](https://img.shields.io/badge/Vue-3.5+-4FC08D.svg)](https://vuejs.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-336791.svg)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D.svg)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED.svg)](https://www.docker.com/)

**A multi-upstream operations hub for self-hosted API services built with sub2api or new-api.**

English | [中文](README_CN.md)

</div>

## Important Notice

Please read the following carefully before using this project:

- **Provider policy risk**: TransitHub helps administrators connect to and operate upstream admin platforms. Make sure your use complies with the terms of service of every upstream platform you connect.
- **Compliant use only**: Use this project only in compliance with the laws and regulations of your country or region. Do not use it to bypass authorization, abuse upstream services, or operate accounts you do not control.
- **Self-hosting responsibility**: You are responsible for protecting admin credentials, database backups, network access, and deployment secrets in your own environment.
- **Disclaimer**: This project is for technical learning only. You are responsible for complying with applicable laws and upstream platform policies. The authors assume no liability for service interruptions, account restrictions, data loss, or any direct or indirect damages caused by using this project.

## Overview

TransitHub is a self-hosted admin and operations hub for managing multiple upstream sites and admin workspaces. It focuses on practical operator workflows: connecting upstream platforms, tracking balances and group multipliers, monitoring dashboard metrics, configuring notifications, and running scheduled group-rate campaigns that can automatically restore original multipliers after an activity ends.

The project is built as a Go backend with a Vue 3 admin frontend, backed by PostgreSQL and Redis.

## Features

- **Admin workspace management** - Switch between admin accounts/workspaces and isolate workspace-scoped data.
- **Upstream site management** - Add, sync, inspect, and manage upstream sites with cached metrics.
- **Dashboard metrics** - View live and historical operation data, including balances, costs, trends, group usage, and upstream drilldowns.
- **Group multiplier tracking** - Track group multiplier snapshots, changes, history, platform tags, and custom group types.
- **Group-rate campaigns** - Create immediate or scheduled pricing activities, update selected admin groups, retain activity records, and restore original multipliers at the configured end time.
- **Auto pricing support** - Configure mapped group auto-pricing rules based on upstream multiplier changes.
- **Notification channels** - Configure DingTalk, Feishu, and Telegram bots for balance warnings, multiplier changes, auto-pricing, and campaign notifications.
- **System settings** - Manage refresh intervals, notification strategy, and runtime display settings.

## Tech Stack

| Component | Technology |
|-----------|------------|
| Backend | Go 1.25, net/http, pgx |
| Frontend | Vue 3.5, Vite, TypeScript, TailwindCSS, vue-i18n |
| Database | PostgreSQL 16+ |
| Cache / Session | Redis 7+ |
| Deployment | Docker, Docker Compose |

## Deployment

### Docker Compose

Production compose files live under `deploy/`.

```bash
git clone <your-repo-url> transit-hub
cd transit-hub

# Edit deploy/docker-compose.prod.yml first:
# - image tag
# - database password
# - ADMIN_EMAIL / ADMIN_PASSWORD
# - APP_VERSION if you want a custom version label

docker compose -f deploy/docker-compose.prod.yml up -d
```

Open:

```text
http://YOUR_SERVER_IP:10621
```

The production compose file includes:

- `app`: TransitHub application container.
- `postgres`: PostgreSQL database.
- `redis`: Redis for admin sessions, cache, and scheduled tasks.

Persistent data is stored under the repository root `data/` directory by default:

```text
data/postgres
data/redis
```

### Development Services

For local development dependencies only:

```bash
docker compose -f deploy/docker-compose.yml up -d
```

This starts PostgreSQL and Redis on local ports `5432` and `6379`.

### Build Docker Image

Because the Dockerfile is stored in `deploy/` but expects the repository root as build context, build with:

```bash
docker build -f deploy/Dockerfile -t transithub:local .
```

## Local Development

### Backend

```bash
cd backend
go test ./...
go run ./cmd/api
```

Important environment variables:

```env
PORT=10621
DATABASE_URL=postgres://postgres:postgres@localhost:5432/transithub?sslmode=disable
REDIS_URL=redis://127.0.0.1:6379/0
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=transithub
ALLOW_PUBLIC_REGISTER=true
APP_VERSION=dev
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

Build check:

```bash
npm run build
```

## Verification

Recommended checks before submitting changes:

```bash
cd backend
go test ./...
go vet ./...
go build ./...

cd ../frontend
npm run build

cd ..
docker compose -f deploy/docker-compose.yml config
docker compose -f deploy/docker-compose.prod.yml config
```

## Project Structure

```text
transit-hub/
├── backend/                  # Go backend service
│   ├── cmd/api/              # API entrypoint
│   ├── internal/config/      # Runtime configuration
│   ├── internal/database/    # PostgreSQL, Redis, migrations
│   ├── internal/httpserver/  # HTTP server assembly and middleware
│   └── internal/modules/     # Domain modules
│       ├── admin_accounts/
│       ├── auth/
│       ├── dashboard/
│       ├── group_rate_campaigns/
│       ├── group_rates/
│       ├── my_sites/
│       ├── settings/
│       ├── system/
│       └── upstream/
├── frontend/                 # Vue 3 admin frontend
│   └── src/modules/          # Feature modules
├── deploy/                   # Dockerfile and compose files
├── development-docs/         # Development notes and implementation plans
└── data/                     # Local production data directory, ignored by Git
```

## Project Notes

- `APP_VERSION` is only used for display.
- `AGENTS.md`, `CLAUDE.md`, `.sisyphus/`, local `.env` files, build output, and runtime data are intentionally ignored by Git.

## Star History

<a href="https://star-history.com/#qjp_ai/transit-hub&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=qjp_ai/transit-hub&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=qjp_ai/transit-hub&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=qjp_ai/transit-hub&type=Date" />
  </picture>
</a>

## License

This project is licensed under the GNU Lesser General Public License v3.0 (LGPL-3.0-only). See [LICENSE](LICENSE) for details.

---

<div align="center">

**If TransitHub helps your workflow, consider giving it a star.**

</div>
