# 基于 Context 的跨场域 Agent 协作方法

一个以 Context 为共同坐标、让个人助手和 Agent 小队承接组织需求，并把确认后的经验沉淀为团队能力的公开参考方案。

这个仓库不是一个单独的 Agent 产品，而是一套可配置的工作方式：

> **跨场域痛点 → Context 底座 → 个人助手 / Multica 产品方案小队 → 人的决策中心 → 能力进化闭环**

核心方法来自 [《基于 Context 的跨场域 Agent 协作方法》公开脱敏版](docs/framework/基于Context的跨场域Agent协作方法_公开脱敏版.md)。本文将其中的大部分方法、架构和工作流直接展开，docs/、contracts/、configs/ 和 plugins/ 负责补充可复现的实现细节。

## 一、先理解这套方法

在实际工作中，理解项目、形成方案、验证判断、推动协作和沉淀经验，往往发生在不同工具和协作空间。真正的问题不是“有没有更多 Agent”，而是：

- 换一个 Agent 或对话后，是否还知道这件事是什么、做到哪里、下一步是什么？
- 不同工具之间是否围绕同一份可验证的上下文继续工作？
- Agent 的结论、权限和写回范围是否清楚？
- 一次任务形成的方法，能否经过确认后复用于下一次任务？

这套方案的工作对象始终是 **Context**。入口可以是个人助手、Multica Issue、组织群聊或其他协作界面，但入口变化不改变事实来源、任务边界和写回规则。

### 核心闭环

~~~mermaid
flowchart LR
    Pain["跨场域痛点<br/>重复交代、协作断裂、结论难复用"]
    Context["Context 底座<br/>共同事实、任务状态、权限与产物"]
    Modes["两种协作方式<br/>个人助手 / 产品方案小队"]
    Human["人的决策中心<br/>目标、取舍、授权、验收"]
    Evolution["能力进化闭环<br/>候选、验证、晋升、复用"]

    Pain --> Context
    Context --> Modes
    Modes --> Human
    Human --> Evolution
    Evolution --> Context
~~~

### 一页理解

| 层次 | 要解决的问题 | 关键原则 | 典型产物 |
| --- | --- | --- | --- |
| 跨场域痛点 | 为什么换工具、换 Agent 后工作会断裂？ | 工作对象不能依赖单一对话 | 需求、任务、问题定义 |
| Context 底座 | Agent 需要读取什么才能继续工作？ | 最小加载、事实有源 | 项目 Context、需求 Context、任务 Context |
| 个人助手 | 哪些任务适合人与 Agent 直接共创？ | 边讨论、边判断、边操作真实产物 | 草案、分析、代码、文档 |
| Agent 小队 | 复杂需求如何拆给多个专业角色？ | 产品方案架构师负责拆解、交接和汇总 | 用户／市场洞察、方案总纲、PRD、红队评审 |
| 人的决策中心 | 哪些事情不能由 Agent 自行决定？ | 关键取舍、权限、发布和能力晋升由人确认 | 决定、评审意见、发布授权 |
| 能力进化闭环 | 如何把一次任务变成下一次能力？ | 候选先行、审核后写回 | Skill、Context、Agent 小队 Workflow |

## 二、跨场域使用 Agent 的痛点

### 1. 重复交代

个人的偏好、口径、工作方法和项目背景散落在不同对话中。换一个 Agent，就要重新说明“我是谁、项目是什么、应该怎么做”。

### 2. 跨场域断裂

Codex、Claude Code、Multica、组织群聊等入口各有优势，但如果没有共同的 Context，同一件事在切换工具或切换对话后就会断线，Agent 只能重新猜测上下文。

### 3. 结论难复用

方案结论散在聊天线程里，难以成为项目事实；个人经验也无法自然沉淀成团队可以复用的 Skill、Context 或小队 Workflow。

### 4. 协作边界不清

如果没有明确的读取范围、写入范围、证据要求和人工决策点，Agent 可能读取过多、写回过早，或者把未经确认的建议误当成长期事实。

## 三、解决方案：一个 Context 底座

### 3.1 Context 是跨工具协作的共同坐标

Context 不是把整个知识库复制给每个 Agent，也不是完整聊天记录的替代品。它应该回答：

- 这是什么任务，目标和验收是什么？
- 哪些项目和需求事实与当前任务相关？
- 哪些信息可以相信，来源和新鲜度如何？
- Agent 可以读取什么、写入什么、调用什么工具？
- 结果应该落到哪里，哪些内容需要人确认？

借助工作区规则、任务 manifest 和结构化文件，可以让 Agent 按需读取：

- **个人 Context**：协作偏好、工作方法、适用范围和个人入口；
- **项目／需求 Context**：共同事实、目标、决策、任务、产物和验证状态；
- **Agent 能力配置**：角色、Instructions、Skills、Tools、MCP、Runtime、Model 和权限。

### 3.2 任务 Context 的组成

~~~text
任务 Context
  = 当前任务
  + 个人 Context 切片
  + 项目／需求 Context 切片
  + Agent 能力配置
~~~

四部分分别承担不同职责：

| 组成部分 | 记录什么 | 不应该承担什么 |
| --- | --- | --- |
| 当前任务 | 目标、范围、交付物、验收标准、截止时间、阻塞项 | 不替代项目长期事实 |
| 个人 Context 切片 | 相关偏好、方法、工作范围和个人约束 | 不把完整个人 Memory 传播给项目 |
| 项目／需求 Context 切片 | 共同事实、决策、任务、产物和验证状态 | 不把未经确认的聊天内容当成事实 |
| Agent 能力配置 | 专业角色、工具、模型、运行时和权限 | 不绕过任务授权扩大读取或写入范围 |

### 3.3 两层 Context：个人连续性与项目公共事实

当工作从个人工作台进入项目群或组织协作，协作中心应从“个人 Context”切换为“项目的公共事实”：

1. 每个人可以建立自己的项目／需求 Context 系统；
2. 方案、结论、待办和验证结果沉淀在项目 Context 中；
3. 项目 Context 可以被不同协作入口按需唤起；
4. 进入公共场域的是用户分享、共创并确认的项目内容，而不是成员的完整个人 Context；
5. 个人运行时 Memory 留在个人边界内，只有经过确认的公共结论才进入项目 Context。

**不同入口改变的是交互方式，不变的是工作对象——Context 来源。**

### 3.4 Context 结构示例

下面是一个面向项目协作的示意结构。目录名可以按组织现有约定调整，但应保持“项目事实、需求工作区、Agent 资产和生成产物”分层。

~~~text
project/
├── AGENTS.md                         # 协作规则、权威顺序与写入边界
├── context/                          # 项目级公共事实
│   ├── PROJECT_CONTEXT.md            # 项目定位与跨需求共识
│   ├── architecture.md               # 总体架构或能力分层
│   ├── constraints.md                # 约束与非目标
│   ├── principles.md                 # 稳定设计原则
│   ├── glossary.md                   # 术语表
│   ├── external-dependencies.md      # 外部依赖及验证状态
│   ├── exception-ledger.md           # 未解决的结构、归属和链接例外
│   └── writeback-inbox.md             # 跨需求待评审写回候选
├── 需求/
│   ├── README.md                     # 需求导航与人读总览
│   └── <需求名>/
│       ├── README.md                 # 需求入口与产物导航
│       ├── 项目需求与方案文档等      # 用户可读的业务产物
│       ├── 草稿/                     # 备选方案和未定稿材料
│       ├── 输出/                     # 本需求生成的交付物
│       └── context/                  # 需求级 Agent 侧写
│           ├── REQUIREMENT_CONTEXT.md
│           ├── artifacts.md
│           ├── tasks.md
│           ├── decision-ledger.md
│           └── writeback.md
└── agent/                            # 可选：模板、工具和运行资产
    ├── templates/
    └── tools/
~~~

个人 Context 内部可以继续按“入口 → 项目／需求定位 → 有来源的事实 → 写回候选”组织；本仓库不要求所有组织采用同一套个人目录编号。

## 四、两种 Agent 协作方式

统一 Context 底座建立后，任务可以在不同地方被唤起。两种方式不是互相排斥的产品，而是面向不同任务形态的协作入口。

| 维度 | 个人助手 | Multica 产品方案小队 |
| --- | --- | --- |
| 适用任务 | 范围清晰，或需要人边讨论边判断边修改 | 需要多个专业视角和结构化交接的复杂需求 |
| 协作中心 | 个人共创工作台 | Issue 或同等的可审计任务对象 |
| 组织方式 | Agent 直接读取最小 Context 并操作真实产物 | Lead Agent 拆解任务，专业 Agent 并行分析 |
| 人的参与 | 持续共创和即时判断 | 在目标、关键取舍、风险、发布和验收节点介入 |
| 主要结果 | 分析、草案、代码、文档和候选结论 | 用户／市场洞察、方案总纲、PRD、红队风险和待决策项 |
| 写回方式 | 结果先进入任务或候选 | Lead 汇总后回到原 Issue，再按类型写回 |

### 4.1 个人助手：直接共创工作台

个人助手适合范围相对明确，或者需要人在过程中持续参与判断的任务。它的基本流程是：

1. 定位项目和需求；
2. 读取最小必要 Context；
3. 根据任务授权调用 Skill、工具和真实产物；
4. 输出结果并标记证据、假设、风险和待确认项；
5. 将新方法或长期影响写入候选，而不是直接修改长期能力。

个人助手的价值不是代替人的判断，而是让人不必在每个新对话中重新搭建工作背景。

### 4.2 产品方案小队：复杂需求的小队交付

Multica 产品方案小队是本仓库的核心公开演示场景。它以任务或 Issue 为协作中心，由 Lead Agent 负责目标、拆解、交接、汇总和人工决策点；专业 Agent 只获得与职责相关的最小 Context。

公开示例使用 Multica 中“产品方案小队”的五个角色：

- **产品方案架构师**：队长，负责目标、方案总纲、优先级、路线图、交接和汇总；
- **用户研究员**：负责用户痛点、场景、人物、旅程、JTBD 和证据置信度；
- **竞品与市场分析师**：负责市场、细分、竞品、定位、规模和外部证据；
- **PRD 撰写师**：负责 PRD、用户故事、功能规格和验收标准；
- **红队评审官**：负责前置验尸、风险、假设和最低成本验证建议。

这不是一套独立的控制面。Multica 负责 Issue、角色路由、Prompt/Workflow、状态、评论和人工评估节点；本仓库只提供可脱敏、可迁移的结构化参考配置。小队不覆盖视觉设计、高保真交互、研发实现、运营和发布执行。

### 4.3 小队协作流程

~~~mermaid
flowchart TD
    Intake["需求 / Issue"]
    Lead["产品方案架构师<br/>目标、方案总纲与汇总"]
    Research["用户研究员<br/>用户问题与证据"]
    Market["竞品与市场分析师<br/>市场、竞品与定位"]
    PRD["PRD 撰写师<br/>用户故事与验收"]
    Review["红队评审官<br/>风险、假设与验证"]
    Human["用户评估<br/>方向、风险与最终定稿"]
    Writeback["回写产物、决定、事件与知识候选"]

    Intake --> Lead
    Lead --> Research
    Lead --> Market
    Research --> Lead
    Market --> Lead
    Lead --> PRD
    PRD --> Review
    Review --> Human
    Human --> Writeback
    Writeback --> Intake
~~~

小队要求每次交接至少包含：

- task_id；
- objective；
- acceptance_criteria；
- context_slice；
- expected_output；
- blocked_by。

当人对结果不满意时，可以继续在原 Issue 留评论，重新加载同一任务 Context 迭代；需要深入讨论时，也可以唤起对应专家并通过“@”带入当前 Issue 上下文。

### 用户流线

~~~text
用户提出需求
  → 个人助手装配最小 Context
  → Multica 创建 Issue
  → Lead 澄清目标、验收和范围
  → 用户研究与竞品／市场分析
  → 形成产品方案总纲
  → 用户评估方案方向
  → PRD 细化与红队评审
  → 用户评估风险、取舍和最终方案
  → 回写项目产物、决定、事件和能力候选
  → 下一条需求复用已确认的 Context 和 Workflow
~~~

在这条流线上，Multica 是小队的控制面，Context Skill 是事实和权限底座，人的决策中心通过小队 Prompt/Workflow 中的评估节点发挥作用，Go ACP Adapter 通过本机 Supervisor IPC 接入 Hermes Gateway；Supervisor 独立负责 Gateway 生命周期。

## 五、从需求到能力的八步工作流

这套工作方式把一次需求组织成可追踪、可复用的协作过程：

| 步骤 | 做什么 | 关键结果 |
| --- | --- | --- |
| 1. 建统一工作区 | 用一个项目工作区承载关键事实和协作产出 | Agent 有共同的事实入口 |
| 2. 初始化项目 Context | 写清项目背景、目标、约束和需求入口 | 后续任务可以按需续办 |
| 3. 发起任务 | 说明要解决的问题、范围、产物和验收 | 形成可审计的任务 Context |
| 4. 进入产品方案小队 | 复杂任务交给 Lead Agent 和专业角色 | 形成分工和交接关系 |
| 5. Issue 迭代 | 通过评论、证据和补充 Context 持续修正 | 结果留在原任务链 |
| 6. 唤起专家 | 需要深度讨论时加载当前 Issue 上下文 | 专家不需要重新猜测背景 |
| 7. 沉淀决策框架 | 将任务中的有效方法整理为 Workflow、Skill 或 Context 候选 | 个人经验开始具备复用形态 |
| 8. 承接下一需求 | 新需求直接复用已验证的配置和能力 | 协作系统形成连续性 |

### 两类核心产出

一次任务通常产生两类不同的资产：

1. **项目知识**：一份可复用的项目 Context，记录目标、决策、任务、产物和验证状态，后续继续承接相关工作；
2. **团队经验**：一套评估框架、一轮迭代后的 Skill，以及一份需求工作 Workflow，将人机协作方法固化为可复用能力。

项目知识解决“下一次如何继续这件事”，团队经验解决“下一次如何更好地做类似的事”。

## 六、人的决策中心与写回规则

### 6.1 人负责什么

架构的中心仍然是人。人负责提出任务、判断方向、选择协作方式、确认权限和评审结果；AI 在明确范围内承接、执行和协作。

人至少应参与以下节点：

- 确认需求目标、范围和非目标；
- 选择个人助手、小队或其他协作方式；
- 确认数据、工具和公开边界；
- 判断关键方案取舍、风险和不确定性；
- 审核最终产物；
- 授权将经验晋升为 Skill、Context 或 Agent 小队 Workflow。

Agent 可以提出建议、生成候选和执行已授权动作，但不能把未经确认的内容自动提升为长期事实或公开资源。

### 6.2 结果如何写回

任务完成后，不同类型的结果进入不同位置：

| 结果类型 | 推荐去向 | 是否需要人工确认 |
| --- | --- | --- |
| 项目产物 | 项目需求目录或输出目录 | 需要验收 |
| 已确认决定 | 项目决策记录 | 需要确认 |
| 执行进展、阻塞、完成 | 事件记录 | 需要核对状态 |
| 可复用经验 | knowledge candidate 或等价候选区 | 必须审核 |
| 稳定团队能力 | Skill、Context 或小队 Workflow | 必须授权晋升 |

写回不是“把所有聊天记录存下来”，而是把经过判断的结果按类型、来源、状态和权限落到可审计的位置。

## 七、能力进化闭环

能力进化不是让 Agent 自行修改自己，而是把真实任务中验证过的方法变成下一次可复用的配置：

~~~text
真实任务
  → 形成方法或规则
  → 进入候选
  → 人工审核与裁剪
  → 试运行与验证
  → 晋升为 Skill / Context / 小队 Workflow
  → 在下一次任务中复用
  → 继续产生新的候选
~~~

当前仓库已经预留 skills/self-evolution/ 目录和说明，但可执行的自进化 Skill 尚未实现。后续至少需要补齐：

- 候选记录 Schema；
- 审核与授权协议；
- 试运行和效果验证；
- 晋升、版本化和回滚；
- 面向 Skill、Context 和小队 Workflow 的测试。

## 八、仓库中的公开资源

| 资源 | 作用 | 推荐阅读 |
| --- | --- | --- |
| Context Skill | 管理项目 Context、需求侧写、权限边界、写回和交接 | [SKILL.md](SKILL.md) |
| 核心方法文档 | 保留原方案主线的公开脱敏版，暂不包含图片 | [docs/framework/](docs/framework/) |
| 架构总览 | 将展示叙事映射到技术资产 | [docs/architecture.md](docs/architecture.md) |
| 工作流说明 | 需求提交、Context 装配、小队执行、人工决策和写回 | [docs/workflow.md](docs/workflow.md) |
| Context Contract | 层级、访问矩阵、新鲜度和写回规则 | [contracts/context-contract/](contracts/context-contract/) |
| Task Context Schema | 任务 Context 的字段约束和示例 | [contracts/task-context/](contracts/task-context/) |
| 产品方案小队配置 | Multica 产品方案小队的脱敏参考配置 | [configs/squads/](configs/squads/) |
| Multica 控制面边界 | 小队控制面、Context、CLI、Adapter 与人的职责边界 | [docs/multica-control-plane.md](docs/multica-control-plane.md) |
| Go ACP Adapter | Multica ACP、Supervisor IPC 与本地 Hermes Gateway 的公开接入实现 | [plugins/multica-hermes-bridge/](plugins/multica-hermes-bridge/) |
| 产品方案小队示例 | 从需求进入小队、评审和写回的最小演示 | [examples/product-solution-squad/](examples/product-solution-squad/) |
| 自进化 Skill 预留 | 后续能力进化实现边界和缺口 | [skills/self-evolution/](skills/self-evolution/) |

### 推荐阅读顺序

1. 本 README：理解方法、架构和工作流；
2. [公开脱敏版核心文档](docs/framework/基于Context的跨场域Agent协作方法_公开脱敏版.md)：阅读原方案的完整叙事；
3. [架构总览](docs/architecture.md) 与 [工作流配置说明](docs/workflow.md)：了解资产之间的关系；
4. Context Contract、Task Context Schema 和产品方案小队配置：开始配置；
5. Go ACP Adapter 与产品方案小队示例：了解运行桥接和端到端任务形态。

## 九、快速开始

### 1. 安装 Context Skill

保留根目录安装方式，以兼容现有用户：

~~~bash
git clone https://github.com/WYLINX601/project-context-workflow.git
cp -R project-context-workflow ~/.codex/skills/
~~~

### 2. 创建任务 Context

复制 [任务 Context 示例](configs/task-context/task-context.example.yaml)，按照自己的项目修改目标、Context 切片、允许读写路径、验收标准和回写位置，并使用 [任务 Context Schema](contracts/task-context/task-context.schema.yaml) 校验。

最小权限要求：

- 路径使用仓库相对路径；
- 未列出的路径默认不可读写；
- .env、Session、Memory、.obsidian 和状态库默认拒绝；
- 任务 Context 必须声明有效期和回写位置。

### 3. 配置产品方案小队

在 Multica 中创建或选择产品方案小队，配置产品方案架构师为 Lead，并设置四个专业角色、Prompt/Workflow、阶段产出、交接字段、人工评估节点和失败回退。仓库中的 [产品方案小队示例配置](configs/squads/product-solution-squad.example.yaml) 是公开结构参考，不保证可以直接导入 Multica。

具体冷启动顺序见 [工作流配置说明](docs/workflow.md)；控制面职责见 [Multica 控制面边界](docs/multica-control-plane.md)。

### 4. 构建公开 Go ACP Adapter

~~~bash
cd plugins/multica-hermes-bridge
go test ./...
go vet ./...
go build -o multica-hermes-gateway ./cmd/multica-hermes-gateway
~~~

启动本机 Supervisor（生产环境可由 launchd 或 Windows Service 托管）：

~~~bash
./multica-hermes-gateway supervisor
~~~

桥接路径如下：

~~~text
Multica Custom Runtime
  → ACP stdio
  → Go ACP Adapter
  → Supervisor IPC
  → 已存在或按需启动的本地 Hermes Gateway
~~~

Adapter 只处理 ACP 协议、Session 映射、Profile 透传、权限请求和 Supervisor IPC；Supervisor 独立负责 Gateway 的接管、按需启动、租约、turn pin、健康检查和安全回收。两者都不托管个人 Profile、Memory、Session 或凭据。同一个活动 Session 不应由多个适配路径同时驱动。

Adapter 不创建或调度 Multica 小队，也不管理 Issue、评论和状态；非 Multica 内的 Agent 读取 Issue 时通过当前可用的 Multica CLI 完成。

## 十、公开边界与安全

本仓库采用“默认拒绝、逐项允许”的发布方式。

### 可以公开

- 通用方法论、架构说明和脱敏示例；
- Context Skill、契约、Schema 和模板；
- Go ACP Adapter 源码、测试和配置示例；
- 不含真实数据的 fixtures；
- 工具的公开名称、版本和集成边界。

### 不可以公开

- API Key、Token、Cookie、密码、SSH key 和 .env；
- 个人 Profile、SOUL、Memory、Session、pairing 和状态数据库；
- 真实组织名称、内部项目、任务、聊天和知识库内容；
- 设备绝对路径、主机别名和内部网络信息；
- 未确认版权或隐私授权的图片、截图和外部素材。

详细规则见 [docs/public-boundary.md](docs/public-boundary.md) 和 [SECURITY.md](SECURITY.md)。公开资源仍需经过人工审核；示例配置不代表任何组织的生产权限或部署安全性。

## 十一、当前状态与后续路线

### 已具备

- 可安装的 Project Context Workflow Skill；
- 公开脱敏版核心方法文档；
- Context Contract、Task Context Schema 和配置示例；
- Multica 产品方案小队的公开参考配置；
- v0.3 Go ACP Adapter、Supervisor 的源码、测试和构建入口；
- 产品方案小队的最小流程示例；
- 发布边界、安全说明和 CI 基础结构。

### 当前边界与后续补充

- 自进化 Skill 目前只保留目录和说明，后续补充候选、审核、晋升、回滚和测试闭环；
- 小队运行控制面仍在 Multica，本仓库不实现独立的控制面或配置编译器；
- 当前公开小队示例已按实际 Multica 产品方案小队完成角色和流程脱敏；
- 展示图片和其他素材暂不放入仓库，后续由项目维护者补充；
- 发布前自动检查和完整端到端演示暂不纳入本阶段。

本仓库当前是基础结构和公开参考实现。Python Bridge、个人运行配置、Profile Memory、Session、凭据和敏感图片不属于当前公开范围。

## 十二、许可证

MIT
