# Context-Agent Collaboration Workflow

一个以 Context 为底座、用个人助手和 Agent 小队承接组织需求的公开参考方案。

核心展示主线是：

```text
跨场域痛点
  → Context 底座
  → 个人助手 / Multica 产品方案小队
  → 人的决策中心
  → 能力进化闭环
```

这个仓库当前提供第一批基础资源：

- `SKILL.md`：可直接安装的 `project-context-workflow` Context Skill；
- `docs/framework/`：核心方法的公开脱敏版，暂不包含图片；
- `contracts/`：Context 层级、访问、新鲜度、写回和任务 manifest 契约；
- `configs/squads/`：Multica 产品方案小队示例配置；
- `plugins/multica-hermes-bridge/`：公开的 Go ACP Adapter；
- `examples/`：从需求进入产品方案小队的最小示例；
- `skills/self-evolution/`：自进化 Skill 的预留位置，当前尚未实现。

## 快速开始

### 安装 Context Skill

保留根目录安装方式以兼容现有用户：

```bash
git clone https://github.com/WYLINX601/project-context-workflow.git
cp -R project-context-workflow ~/.codex/skills/
```

### 阅读方案

1. 阅读 [`docs/framework/`](docs/framework/) 了解核心方法；
2. 阅读 [`docs/architecture.md`](docs/architecture.md) 了解展示层与技术资产的映射；
3. 阅读 [`docs/workflow.md`](docs/workflow.md) 了解需求提交、Context 装配、小队执行和人工决策；
4. 复制并修改 [`configs/squads/product-solution-squad.example.yaml`](configs/squads/product-solution-squad.example.yaml)；
5. 按 [`plugins/multica-hermes-bridge/README.md`](plugins/multica-hermes-bridge/README.md) 构建 Go ACP Adapter。

### 构建公开桥接实现

```bash
cd plugins/multica-hermes-bridge
go test ./...
go vet ./...
go build -o multica-hermes-gateway ./cmd/multica-hermes-gateway
```

Adapter 只负责 Multica ACP 与本地 Hermes Gateway 的协议适配，不负责托管个人 Profile、Memory、Session 或凭据。

## 仓库结构

```text
.
├── SKILL.md                         # Context Skill，兼容根目录安装
├── docs/                            # 架构、工作流和公开边界
├── contracts/                       # 可复用契约与任务 manifest schema
├── configs/                         # 小队、任务和工具配置示例
├── plugins/multica-hermes-bridge/   # Go ACP Adapter
├── examples/                        # 脱敏演示
└── skills/self-evolution/           # 自进化 Skill 预留目录
```

## 公开边界

仓库只收录通用方法、脱敏文档、示例配置、公开桥接代码和测试。以下内容不得提交：

- API Key、Token、Cookie、密码、SSH key、`.env`；
- 个人 Profile、SOUL、Memory、Session、pairing 和运行状态；
- 个人项目原文、私有组织信息和设备绝对路径；
- 真实 Feishu／Multica 账号、任务和聊天记录。

公开资源仍需经过人工审核；示例配置不代表任何组织的生产权限或部署安全性。

## 当前状态

这是基础结构版本。Context Skill 和 Go ACP Adapter 已有可运行实现；产品方案小队配置、公开工作流文档和自进化 Skill 将继续迭代。Python 看板控制面 Bridge 不属于本仓库当前公开范围。

## License

MIT
