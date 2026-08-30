# Agent Context Workflow

让不同的 Agent、工具与协作场景，围绕同一份可验证的项目 Context 持续工作。

Agent Context Workflow 是一套 Context-driven collaboration 参考架构。它不创建新的 Agent 平台，也不绑定某个模型或 Runtime；它定义的是任务如何获得可信事实、最小权限和可审阅的写回路径。

> **Project → Demand → Task → Runtime**

## Why

跨工具协作常有三个断点：换对话后需要重复交代背景；多个 Agent 没有共同事实；讨论结果留在聊天里，无法被下一项工作复用。

本项目把 Context 作为共同坐标。入口可以是个人助手、Agent Squad、Issue 或其他协作界面；入口改变的是互动方式，不是事实来源、权限或写回规则。

## The model

| Layer | Location | Purpose |
|---|---|---|
| Project Context | `context/` | 跨需求的目标、原则、约束、架构和术语 |
| Demand Context | `需求/<demand>/context/` | 当前需求的范围、决定、任务、产物索引和写回候选 |
| Task Context | Task manifest | 本次任务的目标、验收、最小读写授权、写回和有效期 |
| Runtime / Personal | Memory、Session、Profile、SOUL、运行状态 | 连续性与执行环境；默认不是项目事实 |

Agent 只加载完成当前任务所需的 Context。个人或运行时信息不会默认传播进项目；未经审核的输出也不会自动成为项目事实。

## How work flows

1. 人提出需求，并确认范围、产物、验收与数据边界。
2. 建立 Project Context 和 Demand Context，记录可共享的已确认事实。
3. 创建 Task Context，显式限定读取、写入、写回和失效时间。
4. 选择协作入口：范围清晰时用个人助手；需要多专业交接时使用 Agent Squad。
5. Agent 产出结果、证据、风险与待决策项。
6. 人审核关键取舍和高风险动作。
7. 已确认的需求事实写回 Demand Context；跨需求事实才进入 Project Context。
8. 经验证的方法成为候选 Skill、Workflow 或 Context，再由人决定是否复用。

人的角色始终处于决策中心：方向、权限扩大、高风险操作、最终验收和能力晋升都需要人工判断。

## Quick start

```bash
git clone https://github.com/WYLINX601/project-context-workflow.git
cd project-context-workflow
python3 -m pip install -r requirements-dev.txt
python3 scripts/validate_repository.py
```

然后：

1. 从 [`templates/project/`](templates/project/) 建立项目入口和 `context/`。
2. 从 [`templates/demand/`](templates/demand/) 为一个需求建立工作区与 `context/` sidecar。
3. 复制 [`contracts/task-context/task-context.example.yaml`](contracts/task-context/task-context.example.yaml)，按 [`schema`](contracts/task-context/task-context.schema.yaml) 填写任务。
4. 参考 [`examples/minimal-project/`](examples/minimal-project/) 查看从 Context、Task 到 output 和 writeback 的完整闭环。

开始执行前，Agent 应能回答：目标是什么？哪些事实可信？能读写什么？结果写到哪里？哪些节点必须交由人确认？

## Collaboration modes

**Personal Agent** 适合范围明确、需要持续讨论或直接操作真实产物的任务。它在任务授权内读取最小 Context，并把结果写回需求工作区。

**Agent Squad** 适合需要多个专业视角的任务。Lead Agent 管理目标、拆解、Context 切片、交接、汇总和人工评审点；专业 Agent 只得到与职责相关的任务 Context。仓库提供了一个 [Product Solution Squad](examples/product-solution-squad/) 参考配置。

Multica 与 Hermes 的 Go ACP Adapter 是可替换的参考运行集成，不是使用本模型的前置依赖。见 [`plugins/multica-hermes-bridge/`](plugins/multica-hermes-bridge/)。

## Repository guide

| Path | Purpose |
|---|---|
| [`SKILL.md`](SKILL.md) | Agent 的最小加载、权限、异常与写回操作规则 |
| [`contracts/context/`](contracts/context/) | 唯一的 Context、authority、access、freshness 与 writeback 规范 |
| [`contracts/task-context/`](contracts/task-context/) | Task manifest schema 与示例 |
| [`templates/`](templates/) | Project 和 Demand 的可复制模板 |
| [`examples/minimal-project/`](examples/minimal-project/) | 可验证的最小端到端示例 |
| [`docs/`](docs/) | 架构、工作流和公开边界 |
| [`scripts/`](scripts/) | 本地与 CI 校验 |

## Safety and publication

公共内容采用默认拒绝、逐项允许。凭据、Token、Cookie、`.env`、个人 Memory、Session、Profile、运行状态、设备路径与真实组织数据不得进入项目公开物。校验脚本会阻断常见残留，但人工审核仍是发布前的必要步骤。

详见 [`docs/public-boundary.md`](docs/public-boundary.md) 与 [`SECURITY.md`](SECURITY.md)。

## License

MIT License. See [`LICENSE`](LICENSE).
