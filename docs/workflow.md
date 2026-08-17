# 工作流配置说明

## 1. 提交需求

组织或项目成员先提交一份可审计的需求，至少包含：

- 目标与背景；
- 范围和非目标；
- 期望产物；
- 验收标准；
- 数据与权限边界；
- 截止时间和阻塞升级方式。

需求进入项目工作区后，再生成任务 Context，不把完整聊天记录当作事实源。

## 2. 装配 Context

任务 Context 由四部分组成：

```text
当前任务
  + 个人 Context 切片
  + 项目／需求 Context 切片
  + Agent 能力配置
```

使用 [`configs/task-context/task-context.example.yaml`](../configs/task-context/task-context.example.yaml) 作为起点，并用 [`contracts/task-context/task-context.schema.yaml`](../contracts/task-context/task-context.schema.yaml) 校验字段。

最小权限要求：

- 路径使用仓库相对路径；
- 未列出的路径默认不可读写；
- `.env`、Session、Memory、`.obsidian` 和状态库默认拒绝；
- 任务 Context 必须有有效期和回写位置。

## 3. 选择协作方式

### 个人助手

适合范围明确、需要持续讨论或需要人边判断边修改的任务。个人助手读取最小 Context，操作真实产物，形成的经验先进入候选。

### Multica 产品方案小队

适合需要多个专业视角的复杂任务。以 Issue 或同等任务对象作为协作中心，由 Lead Agent 负责：

1. 明确目标和验收；
2. 将任务拆解为产品方案、技术架构、交互设计、验证／评审等子任务；
3. 为每个 Agent 分配最小 Context 和明确产物；
4. 汇总结果并带回原任务；
5. 在人工决策点暂停，等待确认。

参考配置见 [`configs/squads/product-solution-squad.example.yaml`](../configs/squads/product-solution-squad.example.yaml)。

## 4. 人的决策中心

人至少参与以下节点：

- 需求目标和非目标确认；
- 协作方式和公开数据边界选择；
- 关键方案取舍；
- 高风险工具调用或外部发布；
- 最终产物验收；
- Skill、Context 或小队 Workflow 的晋升授权。

Agent 可以提出建议和候选，但不能自行把未经确认的内容提升为长期事实或公开资源。

## 5. 写回与能力沉淀

一次任务结束后，分别处理：

| 类型 | 去向 |
|---|---|
| 项目产物 | 项目需求目录或输出目录 |
| 已确认决定 | `decisions/` 或项目决策记录 |
| 执行进展／阻塞／完成 | `events/` |
| 可复用经验 | `knowledge-candidates/`，等待审核 |
| 稳定团队能力 | 审核后进入 Skill、Context 或小队 Workflow |

## 6. 运行桥接

公开参考实现是 Go ACP Adapter：

```text
Multica Custom Runtime
  → ACP stdio
  → Go ACP Adapter
  → 本地 Hermes Gateway
```

同一个活动 Session 不应由多个适配路径同时驱动。Adapter 的配置、Token 和状态库保持在运行设备本地，不进入项目仓库。
