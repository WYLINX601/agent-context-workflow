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

### 2.1 Context 冷启动

每次让 Agent 承接一个新任务时，按以下顺序建立最小可用 Context：

1. 确认当前工作区和项目根目录；
2. 读取工作区规则、项目 HOME/CONTEXT 和当前需求；
3. 根据任务 manifest 读取被授权的项目事实；
4. 装配个人 Context 的相关切片，不加载完整个人 Memory；
5. 装配任务目标、范围、产物、验收、截止时间和回写位置；
6. 明确 Agent 的角色、工具、模型、运行时和读写权限，完成后再开始执行。

冷启动的验收标准不是“读取了多少文件”，而是 Agent 能够回答：当前任务是什么、哪些事实有来源、可以改什么、结果写到哪里、哪些节点必须交给人确认。

### 2.2 Multica 冷启动

Multica 负责小队的运行控制面。首次使用产品方案小队时，建议按以下顺序配置：

1. 在 Multica 中确认工作区和产品方案小队；
2. 设置产品方案架构师为 Lead，并配置用户研究员、竞品与市场分析师、PRD 撰写师、红队评审官四个专业角色；
3. 为每个角色绑定职责、可读取的 Context、输出格式和禁止越权事项；
4. 在小队 Prompt/Workflow 中配置阶段顺序、阶段产出、交接字段、人工评估节点和异常回退；
5. 将项目 Context、需求 Context 与当前 Issue 关联；
6. 创建第一条 Issue，由 Lead 澄清目标并显式委派专业角色；
7. 在方案方向、风险评审和最终定稿等阶段暂停，请用户评估后再继续；
8. 将确认后的产物、决定、事件和能力候选分别写回约定位置。

运行边界和组件职责见 [Multica 控制面边界](multica-control-plane.md)。

## 3. 选择协作方式

### 个人助手

适合范围明确、需要持续讨论或需要人边判断边修改的任务。个人助手读取最小 Context，操作真实产物，形成的经验先进入候选。

### Multica 产品方案小队

适合需要多个专业视角的复杂任务。以 Issue 或同等任务对象作为协作中心，由 Lead Agent 负责：

1. 明确目标和验收；
2. 将任务拆解为用户研究、竞品与市场分析、产品方案、PRD 和红队评审等子任务；
3. 为每个 Agent 分配最小 Context 和明确产物；
4. 汇总结果并带回原任务；
5. 在阶段产出或 Workflow 定义的人工评估点暂停，等待用户确认、修改或补充证据。

参考配置见 [`configs/squads/product-solution-squad.example.yaml`](../configs/squads/product-solution-squad.example.yaml)。

## 4. 人的决策中心

人的决策中心在本方案中是一个抽象的 Workflow 节点，不单独实现一套决策系统。小队 Prompt/Workflow 负责定义何时把阶段成果、关键取舍、风险或最终方案交给用户；用户可以确认、修改、退回或要求补充证据，Lead 再根据结果继续推进。

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

公开参考实现是 Go ACP Adapter + 本机 Supervisor：

```text
Multica Custom Runtime
  → ACP stdio
  → Go ACP Adapter
  → Supervisor IPC
  → 已存在或按需启动的本地 Hermes Gateway
```

同一个活动 Session 不应由多个适配路径同时驱动。Adapter 的配置、Token 和状态库保持在运行设备本地，不进入项目仓库。

Go ACP Adapter 不负责创建或调度 Multica 小队、不管理 Issue、评论和状态，也不直接启动、停止或重启 Hermes；Supervisor 独立负责本地 Gateway 生命周期。非 Multica 内的 Agent 读取 Issue，优先通过当前可用的 Multica CLI 完成；两者的职责边界见 [Multica 控制面边界](multica-control-plane.md)。
