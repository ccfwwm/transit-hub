# TransitHub

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](https://golang.org/)
[![Vue](https://img.shields.io/badge/Vue-3.5+-4FC08D.svg)](https://vuejs.org/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16+-336791.svg)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7+-DC382D.svg)](https://redis.io/)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED.svg)](https://www.docker.com/)

**面向 sub2api / new-api 自托管 API 服务的多上游运营管理中心。**

[English](README.md) | 中文

</div>

## 重要说明

使用本项目之前，请先阅读以下内容：

- **上游平台规则风险**：TransitHub 用于帮助管理员连接和操作上游后台平台。请确认你的使用方式符合所有上游平台的服务条款。
- **合规使用**：请仅在你所在国家或地区法律法规允许的范围内使用本项目。禁止用于绕过授权、滥用上游服务，或操作你无权管理的账号。
- **自部署责任**：你需要自行保护管理员凭据、数据库备份、网络访问权限和部署密钥。
- **免责声明**：本项目仅用于技术学习。你需要自行确保使用方式符合适用法律法规和上游平台规则。因使用本项目导致的服务中断、账号限制、数据丢失或其他直接/间接损失，作者不承担责任。

## 项目概览

TransitHub 是一个自部署的后台运营中心，用于管理多个上游站点和管理员工作区。它关注真实运营工作流：连接上游平台、追踪余额和分组倍率、查看仪表盘指标、配置通知，并运行可定时恢复原倍率的分组活动调价。

项目由 Go 后端和 Vue 3 管理前端组成，使用 PostgreSQL 和 Redis。

## 功能特性

- **管理员工作区管理** - 在多个管理员账号/工作区之间切换，并隔离工作区数据。
- **上游站点管理** - 添加、同步、查看和管理上游站点，并缓存关键指标。
- **仪表盘指标** - 查看实时和历史运营数据，包括余额、成本、趋势、分组用量和上游下钻明细。
- **分组倍率追踪** - 记录分组倍率快照、变动、历史、平台标签和自定义分组类型。
- **活动调价** - 创建立即或定时的调价活动，更新选中的 admin 分组，并在活动结束后恢复原倍率。
- **自动调价支持** - 基于上游倍率变化，为映射分组配置自动调价规则。
- **通知渠道** - 配置钉钉、飞书和 Telegram 机器人，用于余额预警、倍率变化、自动调价和活动通知。
- **系统设置** - 管理刷新间隔、通知策略和运行时展示配置。

## 技术栈

| 组件 | 技术 |
|------|------|
| 后端 | Go 1.25, net/http, pgx |
| 前端 | Vue 3.5, Vite, TypeScript, TailwindCSS, vue-i18n |
| 数据库 | PostgreSQL 16+ |
| 缓存 / 会话 | Redis 7+ |
| 部署 | Docker, Docker Compose |

## 部署

### Docker Compose

生产部署文件位于 `deploy/` 目录。

```bash
git clone <your-repo-url> transit-hub
cd transit-hub

# 先编辑 deploy/docker-compose.prod.yml：
# - 镜像 tag
# - 数据库密码
# - ADMIN_EMAIL / ADMIN_PASSWORD
# - 如需自定义后台展示版本，可调整 APP_VERSION

docker compose -f deploy/docker-compose.prod.yml up -d
```

访问地址：

```text
http://YOUR_SERVER_IP:10621
```

生产 compose 包含：

- `app`：TransitHub 应用容器。
- `postgres`：PostgreSQL 数据库。
- `redis`：用于管理员会话、缓存和定时任务。

默认持久化数据存放在仓库根目录的 `data/`：

```text
data/postgres
data/redis
```

### 开发依赖服务

本地开发只启动 PostgreSQL 和 Redis：

```bash
docker compose -f deploy/docker-compose.yml up -d
```

这会在本地开放 `5432` 和 `6379` 端口。

### 构建 Docker 镜像

由于 Dockerfile 放在 `deploy/`，但构建上下文需要使用仓库根目录，请使用：

```bash
docker build -f deploy/Dockerfile -t transithub:local .
```

## 本地开发

### 后端

```bash
cd backend
go test ./...
go run ./cmd/api
```

常用环境变量：

```env
PORT=10621
DATABASE_URL=postgres://postgres:postgres@localhost:5432/transithub?sslmode=disable
REDIS_URL=redis://127.0.0.1:6379/0
ADMIN_EMAIL=admin@example.com
ADMIN_PASSWORD=transithub
ALLOW_PUBLIC_REGISTER=true
APP_VERSION=dev
```

### 前端

```bash
cd frontend
npm install
npm run dev
```

构建检查：

```bash
npm run build
```

## 验证命令

提交变更前建议运行：

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

## 项目结构

```text
transit-hub/
├── backend/                  # Go 后端服务
│   ├── cmd/api/              # API 入口
│   ├── internal/config/      # 运行配置
│   ├── internal/database/    # PostgreSQL、Redis、迁移
│   ├── internal/httpserver/  # HTTP 服务组装和中间件
│   └── internal/modules/     # 领域模块
│       ├── admin_accounts/
│       ├── auth/
│       ├── dashboard/
│       ├── group_rate_campaigns/
│       ├── group_rates/
│       ├── my_sites/
│       ├── settings/
│       ├── system/
│       └── upstream/
├── frontend/                 # Vue 3 管理前端
│   └── src/modules/          # 前端业务模块
├── deploy/                   # Dockerfile 和 Compose 文件
├── development-docs/         # 开发说明和实现规划
└── data/                     # 本地生产数据目录，Git 忽略
```

## 项目说明

- `APP_VERSION` 仅用于后台展示。
- `AGENTS.md`、`CLAUDE.md`、`.sisyphus/`、本地 `.env`、构建产物和运行时数据均会被 Git 忽略。

## Star History 星际历史

<a href="https://star-history.com/#qjp_ai/transit-hub&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=qjp_ai/transit-hub&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=qjp_ai/transit-hub&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=qjp_ai/transit-hub&type=Date" />
  </picture>
</a>

## License

本项目采用 GNU Lesser General Public License v3.0（LGPL-3.0-only）协议，详见 [LICENSE](LICENSE)。

---

<div align="center">

**如果 TransitHub 对你的工作有帮助，欢迎点一个 Star。**

</div>
