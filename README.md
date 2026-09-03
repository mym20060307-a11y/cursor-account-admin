# Cursor Account Admin

本地 Web 管理面板：集中管理多个 Cursor 账号，查看用量 / Token / 成本，并一键切换本机 Cursor 登录态。

> **仅供个人本地使用。** 账号 Token、密码保存在本机 JSON 中；请勿把 `accounts.json` 提交到公开仓库，也请自行遵守 Cursor 服务条款。

## 功能特性

- **一键登录 / 切号** — 用已保存 Session Token（失效时依次尝试本机同邮箱会话、已存密码的浏览器登录）刷新用量，写入本机 Cursor，并**自动重新打开客户端**
- **同步本机 Cursor** — 顶部按钮把本机当前登录账号写入 / 更新到本平台并拉取用量
- **账户管理** — 添加、编辑、删除；支持分组；支持粘贴文本批量导入
- **用量与成本** — 调用 Cursor 官方用量接口，展示请求用量、Token（输入 / 输出 / 缓存）、成本（$）
- **用量消耗页** — 全账号 Token / 请求 / 成本合计 + 分账号排行（**翻页**，默认每页 10 条）
- **用户账户页** — 账号数 / Free / 高用量 / 异常统计；账户列表翻页；Token 下方显示对应成本
- **本地自动同步** — 默认每 5 秒扫描本机 Cursor 登录态并 upsert；约每 30 秒刷新用量
- **本地存储** — 默认写入程序目录下的 `accounts.json`，无需数据库
- **单文件部署** — `go build` 得到可执行文件，前端 HTML 内嵌

## 界面一览

| 页面 | 内容 |
|------|------|
| **用量消耗** | Token 合计仪表盘、请求数、成本；分账号用量排行（可翻页） |
| **用户账户** | 概况卡片、分组筛选、账户列表（计划 / 状态 / 用量 / Token+成本 / 更新时间） |

## 快速开始

### 环境要求

- Go **1.22+**（本仓库 `go.mod` 为 1.25）
- 本机已安装 Cursor（切号 / 本地同步功能需要）
- Windows / macOS / Linux 均可编译运行

### 编译与运行

```bash
# 进入项目目录
cd cursor-account-admin

# 编译（Windows）
go build -o cursor-account-admin.exe .

# 编译（macOS / Linux）
go build -o cursor-account-admin .

# 运行（默认端口 9999，数据文件 accounts.json）
./cursor-account-admin.exe

# 自定义端口与数据文件
./cursor-account-admin.exe -port 9090 -data mydata.json

# 关闭本地 Cursor 账号自动同步
./cursor-account-admin.exe -sync-interval 0
```

Windows 也可使用脚本：

```bat
start.bat   # 后台启动并打开浏览器
stop.bat    # 结束 cursor-account-admin.exe
```

浏览器打开：**http://localhost:9999**

### 命令行参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `-port` | `9999` | HTTP 监听端口 |
| `-data` | `accounts.json` | 账户数据文件路径（相对路径相对**进程工作目录**） |
| `-sync-interval` | `5s` | 本机 Cursor 账号同步间隔；`0` 关闭 |

## 数据存在哪里

默认文件：

```text
<程序工作目录>/accounts.json
```

若用 `start.bat` 或在本仓库根目录启动，即为：

```text
c:\Users\...\cursor-account-admin\accounts.json
```

该文件含邮箱、密码（若填写）、Session Token、用量缓存等敏感信息。仓库 `.gitignore` 已忽略 `accounts.json`，**发布到 GitHub 前请确认不会误提交**。

## 使用说明

### 一键登录（推荐切号方式）

1. 在「用户账户」中打开账号详情，点 **登录**
2. 优先使用已保存 Session Token 校验并刷新用量
3. Token 失效则依次尝试：本机 Cursor 同邮箱会话 → 已存密码的浏览器登录（chromedp）
4. 成功后写入本机 Cursor 的 `state.vscdb`（`cursorAuth/*`）
5. 若 Cursor 正在运行会先关闭，写完后**自动重新打开**，客户端即为该账号

说明：

- 本机 Cursor **同时只能保持一个登录态**；点哪个号的「登录」，客户端就切到哪个号
- 顶部 **同步本机 Cursor**：方向相反，只把本机当前登录同步进平台
- 自动重启依赖能解析到 Cursor 安装路径（运行中进程 → 常见安装目录 → PATH）；失败时会提示手动打开

### 本地账号自动同步

服务启动后默认每 5 秒：

1. 从本机 `state.vscdb` / `storage.json` 读取邮箱与 Token
2. 平台中无同邮箱 → 自动新增
3. 已有同邮箱 → 仅在 Token 变化时更新
4. 约每 30 秒用 Session Token 拉取用量（无需打开浏览器）

### 手动获取 Session Token

1. 浏览器打开 [cursor.com/settings](https://www.cursor.com/settings) 并登录
2. `F12` → **Application** → **Cookies** → `https://www.cursor.com`
3. 复制 `WorkosCursorSessionToken`
4. 编辑账户，粘贴到 Session Token，再点「登录」写入本机（如需切号）

Token 格式一般为 URL 编码的 `userId::jwt`，形如：`user_01xxx%3A%3AeyJhbG...`

### 添加 / 批量导入

- **添加账户**：邮箱必填；密码、Session Token、分组可选
- **批量导入**：每行 `邮箱<Tab>密码`，例如：

```text
user1@example.com	your-password-1
user2@example.com	your-password-2
```

导入后建议为账号补上 Session Token（或先在客户端登录该号再点「同步本机 Cursor」）以便查用量。

### 刷新用量

- 单个：详情里点 **刷新**
- 全部：顶部 **刷新全部用量**
- 页面列表：每 **5 秒** 自动拉取最新本地数据（无变化不重绘）
- 后台：本地同步约每 **30 秒** 刷新已保存 Token 的用量

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/` | 仪表盘页面 |
| `GET` | `/api/accounts` | 账户列表 |
| `POST` | `/api/accounts` | 添加账户 |
| `POST` | `/api/accounts/import` | 批量导入 |
| `PUT` | `/api/accounts/{id}` | 更新账户 |
| `DELETE` | `/api/accounts/{id}` | 删除账户 |
| `POST` | `/api/accounts/{id}/refresh` | 刷新单个用量 |
| `POST` | `/api/accounts/{id}/local-login` | 一键登录并写入本机 Cursor |
| `POST` | `/api/accounts/{id}/browser-login` | 仅浏览器登录拿 Token（不写入本机） |
| `POST` | `/api/login-all` | 同步本机 Cursor 当前账号到平台 |
| `POST` | `/api/refresh-all` | 刷新全部用量 |
| `GET` | `/api/groups` | 分组列表 |

示例：

```bash
# 添加账户
curl -X POST http://localhost:9999/api/accounts \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"secret","session_token":"user_01xxx%3A%3AeyJhbG..."}'

# 批量导入
curl -X POST http://localhost:9999/api/accounts/import \
  -H "Content-Type: application/json" \
  -d '{"accounts":[{"email":"a@example.com","password":"p1"},{"email":"b@example.com","password":"p2"}]}'

# 刷新全部
curl -X POST http://localhost:9999/api/refresh-all
```

## 项目结构

```text
cursor-account-admin/
├── main.go                 # 入口：参数、启动 HTTP、嵌入前端
├── go.mod / go.sum
├── start.bat / stop.bat    # Windows 启停脚本
├── accounts.json           # 运行时数据（已 gitignore，勿提交）
├── web/
│   └── index.html          # 前端（编译时 embed）
├── internal/
│   ├── model/              # 账户与用量模型
│   ├── store/              # JSON 文件存储
│   ├── cursor/             # 用量 API、浏览器登录、本机 state.vscdb 读/写
│   ├── localsync/          # 本机登录态定时同步
│   └── handler/            # HTTP 路由与处理器
└── README.md
```

## 测试

```bash
go test ./...
go test ./internal/store/ -v
go test ./internal/cursor/ -v
go test ./internal/handler/ -v
go test ./... -cover
```

国内网络若拉模块较慢，可临时：

```bash
set GOPROXY=https://goproxy.cn,direct
go test ./...
```

## 技术说明

### 切号原理

本机 Cursor 登录态主要写在用户目录下的 `User/globalStorage/state.vscdb`（以及可能存在的 `storage.json`）中的 `cursorAuth/*` 键。写入前若 Cursor 正在运行会结束进程以释放数据库锁，写完后尝试重新启动客户端。

### 用量数据来源

使用已保存的 `WorkosCursorSessionToken` 请求 Cursor 接口（节选）：

| 接口 | 用途 |
|------|------|
| `GET https://cursor.com/api/usage-summary` | 计划、用量进度等 |
| `POST https://cursor.com/api/dashboard/get-aggregated-usage-events` | Token 合计、成本（美分）等 |
| `POST https://cursor.com/api/dashboard/get-filtered-usage-events` | 请求次数等 |

请求头携带 Cookie：`WorkosCursorSessionToken=...`。

### 依赖（主要）

- `modernc.org/sqlite` — 纯 Go 读写本机 `state.vscdb`
- `github.com/chromedp/chromedp` — 可选的浏览器自动登录（需账号密码）

## 安全与隐私

- 默认监听 `0.0.0.0:9999`，同一局域网内可访问；仅本机使用时可自行用防火墙限制，或改代码绑 `127.0.0.1`
- 不要把含真实 Token / 密码的 `accounts.json`、日志推送到 GitHub
- 本工具会读写本机 Cursor 配置；请在自有设备上使用

## 许可证

MIT License
