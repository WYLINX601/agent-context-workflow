# multica-hermes-gateway

独立的 Multica Custom Runtime ACP Adapter。它把官方 Multica 的 Hermes-family ACP stdio 请求翻译为已经运行的 Hermes TUI Gateway WebSocket 请求。

```text
Multica Custom Runtime
  └─ multica-hermes-gateway acp
       └─ ws://127.0.0.1:9119/api/ws
            └─ hermes serve
```

## 边界

适配器不会启动、停止或重启 Hermes，不创建临时 `HERMES_HOME`，不读取或复制 Hermes Memory、Skills、SOUL、Session transcript，也不调用 Multica Cloud API。Profile 由 Adapter 进程启动配置决定，并透传给本地 Hermes Gateway；多个 Adapter 进程可以共用同一个 Hermes 端口、分别接入不同 Profile。

本公开仓库只包含 ACP Adapter；看板轮询控制面属于另一种集成路径，不在本公开包中。不同适配路径不能同时驱动同一个 active Hermes session。

## 构建

需要 Go 1.24 或更高版本：

```bash
cd plugins/multica-hermes-bridge
go mod tidy
go test ./...
go vet ./...
go build -o multica-hermes-gateway ./cmd/multica-hermes-gateway
```

## 使用

先由用户独立启动持久 Hermes：

```bash
hermes serve
```

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

可通过 `MHG_CONFIG`、`MHG_GATEWAY_URL`、`MHG_STATUS_URL`、`MHG_LOG_LEVEL`、`MHG_ALLOW_REMOTE`、`MHG_PROFILE` 覆盖。Profile 也可以作为 ACP 启动参数传入：`multica-hermes-gateway acp --profile product-solution`。优先级为 `--profile` > `MHG_PROFILE` > `session.profile` > Hermes 默认；不要使用未命名的 `profile=product-solution` 环境变量。Gateway token 仅支持本机环境或 token file（`MHG_GATEWAY_TOKEN`、`MHG_GATEWAY_TOKEN_FILE`），不会出现在 `config show` 或日志中。

在 Multica 智能体设置中，推荐添加环境变量：

```text
KEY: MHG_PROFILE
值: product-solution
```

每个智能体进程只绑定一个 Profile；需要多个 Profile 时，分别配置多个智能体并设置不同的 `MHG_PROFILE`。

默认状态库：

```text
~/.local/state/multica-hermes-gateway/state.db
```

数据库只保存 MHG stable session ID、Hermes stored session ID、Profile、gateway identity、cwd 和时间戳，不保存 prompt 或 transcript。

## v0.2 兼容边界

- 只接受 ACP text prompt block；image/audio/attachment 返回 `MHG3002 UNSUPPORTED_CONTENT`。
- `prompt.submit` 的 RPC 返回不代表 turn 完成；必须等 `message.complete`、`session.status` idle 和 250ms quiet window。
- Adapter crash 或 submit timeout 后不自动 replay；恢复时若 Hermes 仍 running，返回 `MHG2004 AMBIGUOUS_PREVIOUS_TURN`。
- 默认只允许 loopback Gateway。Desktop 与适配器不能同时驱动同一个 active session。
- Profile 会写入会话映射；使用另一个 Profile 恢复旧的 MHG session 会返回 `MHG2005 SESSION_PROFILE_MISMATCH`，避免跨身份误恢复。
- `permissions.mode=deny` 会拒绝所有 approval；clarify/sudo/secret 均 fail closed，不代填密码或密钥。
