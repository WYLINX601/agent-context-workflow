# multica-hermes-gateway

独立的 Multica Custom Runtime ACP Adapter。它把官方 Multica 的 Hermes-family ACP stdio 请求翻译为由 Supervisor 管理的 Hermes TUI Gateway WebSocket 请求；Gateway 已运行时接管连接，未运行时按任务按需启动。

```text
Multica Custom Runtime
  └─ multica-hermes-gateway acp
       └─ Supervisor IPC
            └─ managed/adopted Hermes Gateway
                 └─ ws://127.0.0.1:9119/api/ws
```

## 边界

ACP Adapter 不直接启动、停止或重启 Hermes；完整生命周期由独立的 Supervisor 进程负责。Supervisor 是 `runtime.db` 的唯一写入者，Adapter 只维护自己的 ACP Session mapping。两者都不创建临时 `HERMES_HOME`，不读取或复制 Hermes Memory、Skills、SOUL、Session transcript，也不调用 Multica Cloud API。

Runtime ownership 分为三类：外部已启动的 `adopted-shared` 只连接和监控、永不停止或主动重启；Supervisor 启动的 `managed-shared` 按 Gateway endpoint 复用并默认常驻；`managed-isolated` 为单 Profile 独占进程，零租约且无活动 turn 后才自动回收。活动 turn 通过 `PinTurn` 保活，进程重启后 generation 增加，旧 Adapter 租约会被拒绝，Prompt 不会自动重放。

现有 Workbench Python 控制面 Bridge 位于 `90_system/hermes/automation/multica_hermes_bridge.py`，负责另一种“看板轮询 → Hermes Gateway”路径；它与本模块并存，不能同时驱动同一个 active Hermes session。

## 构建

需要 Go 1.24 或更高版本：

```bash
cd 90_system/hermes/multica-hermes-gateway
go mod tidy
go test ./...
go vet ./...
go build -o multica-hermes-gateway ./cmd/multica-hermes-gateway
```

## 使用

先启动本机 Supervisor（生产环境应由 launchd／Windows Service 托管）：

```bash
multica-hermes-gateway supervisor
```

如果配置的 Gateway 已经由 Hermes Desktop 或其他客户端启动，Supervisor 会将它识别为 `adopted-shared`，不会接管或停止它。未启动时，首次 `session/new` 或 `session/resume` 才会由 Supervisor 按需启动 Hermes。

再检查适配器：

```bash
./multica-hermes-gateway doctor
./multica-hermes-gateway config show
```

Multica Custom Runtime 配置为：

```text
Name: Persistent Hermes
Protocol family: Hermes
Command: /absolute/path/to/multica-hermes-gateway
```

Multica 当前会在命令后追加 `acp`，因此实际启动为：

```bash
multica-hermes-gateway acp
```

如果 Desktop daemon 找不到 PATH 中的程序，应使用 Multica 官方的 per-machine executable path override；不要把 Gateway token 放入 Custom Runtime fixed args。

## 本地配置

默认读取：

```text
~/.config/multica-hermes-gateway/config.yaml
```

可通过 `MHG_CONFIG`、`MHG_GATEWAY_URL`、`MHG_STATUS_URL`、`MHG_LOG_LEVEL`、`MHG_ALLOW_REMOTE`、`MHG_PROFILE` 覆盖。Profile 也可以作为 ACP 启动参数传入：`multica-hermes-gateway acp --profile kahn`。优先级为 `--profile` > `MHG_PROFILE` > `session.profile` > Hermes 默认；不要使用未命名的 `profile=kahn` 环境变量。Gateway token 仅支持本机环境或 token file（`MHG_GATEWAY_TOKEN`、`MHG_GATEWAY_TOKEN_FILE`）。受管 Runtime 由 Supervisor 生成 0600 token file，并只通过 IPC 返回不含密钥内容的 `token_ref`；token 内容不会进入 IPC、`runtime.db`、`config show` 或日志。

在 Multica 智能体设置中，推荐添加环境变量：

```text
KEY: MHG_PROFILE
值: kahn
```

每个智能体进程只绑定一个 Profile；需要多个 Profile 时，分别配置多个智能体并设置不同的 `MHG_PROFILE`。

Supervisor 相关 YAML 示例：

```yaml
concurrency:
  exclusive_turn_scope: profile # profile 或 endpoint

supervisor:
  enabled: true
  endpoint: ~/.local/state/multica-hermes-gateway/supervisor.sock
  runtime_db: ~/.local/state/multica-hermes-gateway/runtime.db
  hermes_executable: hermes
  scope: shared # shared 或 isolated
  heartbeat_interval: 10s
  lease_ttl: 30s
  health_interval: 5s
  start_timeout: 20s
  shutdown_grace: 5s
  isolated_idle_timeout: 10m
```

`concurrency.exclusive_turn_scope=profile` 只串行化同一 Gateway endpoint + Profile 的 turn；`endpoint` 会把同一 endpoint 上的所有 Profile 串行化。旧的 `exclusive_turn_per_gateway` 仅作为兼容字段，新配置应使用 `exclusive_turn_scope`。

Supervisor IPC 的第一版信任边界是“同一 OS 用户”：Unix socket 为 `0600`，Windows named pipe 使用 owner-only ACL。它不是跨用户控制面；若部署为跨用户 Windows/Unix 服务，必须另外收紧 service/pipe/socket ACL。

`shared` 的 key 是规范化 Gateway endpoint，不是 Profile，因此多个 Profile 会复用同一个 Gateway；当配置了非空 Profile 时，Supervisor 必须从 Hermes `/api/profiles` 确认 `gateway_mode=multiplex` 且该 Profile 位于目标 Gateway 的 `served_profiles`，否则 fail closed；如果本机 Hermes 未开启多 Profile multiplexing，需要为这些智能体配置 `scope: isolated`。

配置非空 Profile 时，Adapter 会在 `session/new` 或 `session/resume` 前先读取 Hermes 的 `/api/profiles` 并做精确匹配。Profile 不存在时返回 `MHG3004 PROFILE_NOT_FOUND`，Profile 列表无法读取或响应格式无法验证时返回 `MHG3005 PROFILE_LOOKUP_FAILED`；这两种情况都不会调用 `session.create`／`session.resume`，也不会由 Adapter 自动创建 Profile 或回退到其他 Profile。`doctor` 也会执行同一项检查。

默认状态库：

```text
~/.local/state/multica-hermes-gateway/state.db
```

数据库只保存 MHG stable session ID、Hermes stored session ID、Profile、gateway identity、cwd 和时间戳，不保存 prompt 或 transcript。

Supervisor 的 `runtime.db` 只保存 Runtime/Lease/turn pin/启动 operation 的生命周期元数据，包括 PID、进程启动标识、generation、命令指纹、endpoint、launch nonce 和不含密钥内容的 `token_ref`；它由 Supervisor 单实例独占写入，不保存 Gateway token。启动先持久化 `STARTING` journal；Supervisor 重启后只恢复能通过进程身份校验的 Hermes，旧 generation 会被拒绝。

## v0.3 兼容边界

- 只接受 ACP text prompt block；image/audio/attachment 返回 `MHG3002 UNSUPPORTED_CONTENT`。
- `prompt.submit` 的 RPC 返回不代表 turn 完成；必须等 `message.complete`、`session.status` idle 和 250ms quiet window。
- Adapter crash 或 submit timeout 后不自动 replay；恢复时若 Hermes 仍 running，返回 `MHG2004 AMBIGUOUS_PREVIOUS_TURN`。
- 默认只允许 loopback Gateway。Desktop 与适配器不能同时驱动同一个 active session。
- Profile 会写入会话映射；使用另一个 Profile 恢复旧的 MHG session 会返回 `MHG2005 SESSION_PROFILE_MISMATCH`，避免跨身份误恢复。
- 非空 Profile 必须先在 Hermes `/api/profiles` 中存在；不存在时 fail closed，不创建 Hermes 会话、不创建 Profile，也不回退到启动 Profile。
- `permissions.mode=deny` 会拒绝所有 approval；clarify/sudo/secret 均 fail closed，不代填密码或密钥。
