# Multica 控制面与公开仓库边界

本仓库采用一个明确的分工：**Multica 是 Agent 小队的控制面，公开仓库是 Context、配置、方法和接入边界的公开参考包。**

公开仓库不重新实现 Multica 的 Issue、小队调度或 Agent 状态系统，也不把个人运行时复制到 GitHub。

## 1. 组件分工

| 组件 | 负责什么 | 不负责什么 |
| --- | --- | --- |
| Multica | Workspace、Issue、小队、Agent 调度、状态、评论、Prompt／Workflow 执行 | 不替代项目 Context 的事实治理 |
| Context Skill | 项目／需求／任务 Context 配置、按需加载、权限边界和写回规则 | 不负责创建或调度 Multica 小队 |
| Multica CLI | 为非 Multica Agent 提供 Issue、项目和工作区的命令行访问入口 | 不替代 Multica 的控制面 |
| Go ACP Adapter | ACP stdio 与已经运行的本地 Hermes Gateway 之间的协议适配 | 不负责 Issue 轮询、小队调度或产品方案汇总 |
| Hermes Gateway | 在本地承接 Profile、Session 和 Agent Runtime | 不成为项目公共事实源 |
| 人 | 评估方向、关键取舍、阶段成果、最终交付和能力候选 | 不需要亲自承担每个专业 Agent 的执行 |

## 2. 数据流

~~~mermaid
flowchart LR
    User["用户<br/>提出需求与阶段评估"]
    Context["Context Skill<br/>项目/需求/任务 Context"]
    Multica["Multica 控制面<br/>Issue / Squad / Workflow"]
    Lead["产品方案架构师<br/>Lead Agent"]
    Experts["用户研究 / 市场竞品 / PRD / 红队"]
    CLI["Multica CLI<br/>非 Multica Agent 读取 Issue"]
    Adapter["Go ACP Adapter<br/>ACP ↔ Hermes Gateway"]
    Hermes["本地 Hermes Gateway<br/>Profile / Session"]
    Writeback["项目产物 / 决定 / 事件 / 能力候选"]

    User --> Context
    Context --> Multica
    User --> Multica
    Multica --> Lead
    Lead --> Experts
    Experts --> Lead
    CLI --> Multica
    Multica --> Adapter
    Adapter --> Hermes
    Lead --> User
    User --> Writeback
    Lead --> Writeback
~~~

核心关系是：

1. 用户在 Multica 中提出或确认需求；
2. Context Skill 为任务提供最小、可追溯的项目和需求背景；
3. Multica 把 Issue 路由给产品方案小队的队长；
4. 队长根据小队 Prompt／Workflow 委派专业 Agent；
5. 阶段性成果按照小队定义交给用户评估；
6. 确认后的产物、决定和方法候选按 Context 写回规则沉淀。

## 3. Multica 控制面职责

Multica 负责小队运行时的过程管理：

- 创建和维护 Issue；
- 将 Issue 路由到产品方案小队队长；
- 由队长按需委派成员，而不是默认唤起所有成员；
- 承载评论、状态、交接和结果；
- 执行小队成员的 Prompt、Skill 和 Workflow；
- 在核心阶段性成果出现时，按照小队规则请求用户评估；
- 将用户的评估继续带回原 Issue，推动下一轮迭代。

本仓库只提供一个脱敏的[产品方案小队配置示例](../configs/squads/product-solution-squad.example.yaml)，不承诺该 YAML 可以直接被 Multica 导入。它用于说明角色、阶段、交接、评审门和公开边界。

## 4. Context Skill 的职责

Context Skill 负责让 Agent 在进入任务时知道：

- 当前需求的目标、范围、产物和验收；
- 哪些项目／需求 Context 可以读取；
- 哪些路径允许写入；
- 哪些信息必须拒绝读取或传播；
- 哪些结果需要写回项目产物、决定、事件或能力候选。

Context 是小队协作的共同坐标，但不等于把完整个人 Context、Memory 或 Session 发送给小队。进入公共协作空间的应是用户确认并允许共享的项目内容。

## 5. 非 Multica Agent 的 Issue 访问

不在 Multica 内运行的 Agent，不需要成为 Multica 的原生队友，也不需要复制 Issue 数据到另一套事实源。它们通过当前 Multica CLI 提供的 Issue 读取能力获取：

- Issue 目标和当前状态；
- 评论和历史交接；
- 关联项目和资源；
- 当前任务所需的上下文入口。

具体命令和认证方式由使用者本地的 Multica CLI 版本决定；公开文档不写入账号、Token、工作区 ID 或设备路径。

## 6. Go ACP Adapter 的边界

Go ACP Adapter 是可选的运行接入层：

~~~text
Multica Custom Runtime
  → ACP stdio
  → Go ACP Adapter
  → 已运行的本地 Hermes Gateway
~~~

它只处理协议、Session 映射、Profile 透传、权限请求和本地 Gateway 通信。它不会：

- 创建或调度 Multica 小队；
- 轮询或管理 Issue；
- 读取个人 Memory、Session transcript 或凭据；
- 代替用户完成产品方向和阶段成果评估；
- 把 Multica 控制面复制成公开仓库中的第二套系统。

## 7. 人的评估节点

人的决策中心在这里是一个抽象流程节点，不是额外建设的服务。小队 Prompt／Workflow 在以下结果形成时请求用户评估：

- 方案总纲、范围、优先级和非目标；
- 红队风险、关键假设和放行条件；
- 最终方案包、未解决问题和下一步；
- 需要写回项目事实或形成能力候选的内容。

用户的评估可以是确认、修改、退回迭代或要求补充证据。没有用户确认时，Agent 只能保留为候选，不能把结果当成最终项目事实。

## 8. 公开仓库的运行前提

完整的小队运行需要使用者具备自己的：

- Multica Workspace；
- 产品方案小队和成员；
- 项目／需求 Context；
- Multica CLI 或 Multica 原生入口；
- 可选的本地 Hermes Gateway 和 Go ACP Adapter。

因此，本仓库的公开目标是“可理解、可配置、可接入、可脱敏复用”，而不是在没有 Multica 的情况下独立运行完整的小队控制面。
