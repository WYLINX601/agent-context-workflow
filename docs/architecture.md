# 架构总览

本仓库用一条用户可理解的主线组织技术资产：

| 展示层 | 要回答的问题 | 当前公开资产 |
|---|---|---|
| 跨场域痛点 | 为什么换工具、换 Agent 后工作会断裂？ | [`framework/`](framework/) |
| Context 底座 | 哪些信息是事实，应该如何按需加载？ | `SKILL.md`、[`contracts/context-contract/`](../contracts/context-contract/) |
| 个人助手 | 哪些任务适合人与 Agent 直接共创？ | `project-context-workflow` Skill、任务 manifest |
| Agent 小队 | 复杂需求如何拆解给专业角色？ | [`configs/squads/`](../configs/squads/)、[`examples/`](../examples/) |
| 人的决策中心 | 哪些节点必须由人判断、确认和授权？ | [`docs/workflow.md`](workflow.md)、写回与审核规则 |
| 能力进化闭环 | 如何把验证后的方法沉淀为团队能力？ | [`skills/self-evolution/`](../skills/self-evolution/)、候选与写回契约 |
| 运行适配层 | Multica 如何接入本地 Hermes？ | [`plugins/multica-hermes-bridge/`](../plugins/multica-hermes-bridge/) |

## 核心对象

```text
组织需求
  → 需求／任务 Context
  → 解决方案与小队配置
  → Lead Agent 拆解与交接
  → 专业 Agent 执行
  → 人工评审与决策
  → 项目产物、决定、事件、知识候选
```

Context 是跨工具协作的共同坐标，不是把整库内容复制给每个 Agent。每次任务应只加载完成目标所需的最小切片，并通过 manifest 明确允许读取、允许写入、验收标准和回写位置。

## 公开实现边界

Multica 产品方案小队是本仓库的核心参考演示。小队控制面仍在 Multica；本仓库公开 Context、角色职责、Workflow 结构、Go ACP Adapter 边界和脱敏配置，不实现第二套小队调度系统。组件职责详见 Multica 控制面边界文档。

公开接入采用 Go ACP Adapter + 本机 Supervisor。Adapter 连接 Multica Custom Runtime 与 Supervisor IPC，由 Supervisor 负责接管或按需启动本地 Hermes Gateway；两者都不迁移个人运行时，不复制 Memory、Session 或凭据，也不替代人的审批。

使用者可以以公开示例为起点，根据自己的需求、角色、工具、数据边界和人工审核规则调整 Multica 小队。
