# Sigma 编码 Agent 的运行时迭代记录

更新日期：2026-11-09（补入 11 月 5 日前的上下文管理迭代）

本文专门记录为了让 **Sigma 自己更可靠地完成代码任务**而做过的运行时、工具、模型协议和工作流实现。它不记录各语言 Skill 里的解题方法，也不把 Docker 准备、数据集整理、官方评分和归档脚本算成 Sigma 的编码能力。

文中用三类证据：

- **实现证据：**对应的 Sigma 代码或 Git 提交。
- **运行证据：**真实 Agent 轨迹、官方 F2P/P2P 评分或已复现的 provider 错误。
- **归因限制：**同一次实验还改变了模型、thinking、Skill、Memory 或引导时，结果只能说明整套条件下发生了什么，不能证明某一处代码单独造成了成功。

## Sigma 与 Pi 的边界

Sigma 是独立的 Go 编码 Agent。它有自己的会话循环、provider adapter、工具执行层、工作区 diff、文件修改保护、context 管理和 workflow policy；并没有把 Pi 的 TypeScript Agent loop 或实现代码搬进来。两者可比较的是行为和任务轨迹，不是内部代码相同。

部分设计是在与 Pi 对照后改进的，例如 Sigma 最初把 DeepSeek thinking 关掉，而 Pi 实际启用了 thinking；发现后我们改的是 Sigma 自己的 DeepSeek adapter 与配置。Pi 的运行结果只能作为差异线索，除非模型、Skill、Memory、提示时机、预算和环境都一致，否则不能据此说某个 Agent 整体更强。实现映射见 [Pi 设计对照](PI_REFERENCE.md)，逐次实验和评分见 [SWE-bench 结果](../evals/swebench-verified-mini/evaluation-results.md)；常见运行问题与恢复方法收录于[实验方法](../evals/swebench-verified-mini/experiment-method.md#operational-issue-patterns-and-recoveries)。

## 迭代时间线

### 0. 先解决长任务“上下文满了，前面的工作接不上”（2026-10-31 至 11-04）

这段迭代早于 SWE-bench 运行时加固，是 Sigma 编码 Agent 的基础能力建设。回看 Git 历史时要区分“自动腾出上下文空间”和“把任务状态可靠地交接给下一轮”：它们不是同一件事。

在可查到的最早 Sigma 代码快照（`4834c5e`，2026-10-31）里，已经有 `WindowContextManager`。当请求估算超过窗口时，它会保留最近的完整对话轮次，把更早消息压成简短的摘录，并在仍超限时缩短旧工具输出；原始 session transcript 不会因此被删除。这是**自动抽取式压缩**，目的主要是让请求重新装进模型窗口。它不理解任务哪些细节对后续最关键，也不保证模型能从简短摘录中恢复精确的当前进度。因此，从运行体验上看，旧消息被裁掉后若下一步、失败原因或已改文件没留在最近轮次里，Agent 仍可能像“上下文没了”一样重复探索或接不上工作。

2026-11-03 的 `cabdd3b` 增加了 `compact_context` 工具：Agent 可以在历史变长时主动请求一次模型生成的交接摘要，重点保留用户目标、决策、仓库事实、改动文件、测试结果、阻塞点和下一步。摘要作为 session 状态保留，完整历史仍在 session 记录中；生成摘要会额外消耗一次模型请求。它补上的是**语义交接**，但依赖 Agent 识别到该压缩，并不代表每次压缩都能完整保留正确状态。

2026-11-04 的 `7a9c342` 又改进自动压缩：区分主动压缩阈值和模型上下文硬上限；摘要和系统提示纳入预算计算；保留完整对话边界；当最近一轮过大时先逐步缩减工具输出，并尽量在硬上限内留下最近可用交互，而不是仅因超过主动阈值就报错。摘录摘要还开始优先保留原始请求、源码写入、测试、diff 和失败结果。这样降低了“只留下最近一段却丢掉关键验收状态”的概率，但它仍是规则式提取和截短，不等于可靠的任务状态跟踪。

这解释了你提到的“以前没有上下文压缩，所以一下子就没了”：更精确地说，早期已经有基础的窗口压缩，但缺少主动的语义交接，而且旧压缩方式可能把重要进度挤掉；后续才逐步补上摘要工具、预算兜底、状态保留和压缩计数。评测日志也显示压缩带来新的协议风险：#7590 在第 33 轮压缩后触发 HTTP 400，后来确认是 Sigma 把待响应的 tool result 排在进度消息之后。这个问题不是“压缩不该做”，而是压缩后的消息重组必须继续满足 provider 的 tool-call 顺序；11 月 7 日的 serializer 修复记录在下文。

**代码入口：**[最早的自动窗口压缩实现](../internal/agent/context.go#L1)、[主动语义摘要工具与 session 状态](../internal/agent/compact_context.go#L1)、[session snapshot 保存摘要边界](../internal/agent/session_store.go#L20)、[11 月 4 日预算与保留策略改动](https://github.com/sdaAlbert/sigma-agent/commit/7a9c342)。对应历史提交：[Sigma 初始编码 Agent 快照](https://github.com/sdaAlbert/sigma-agent/commit/4834c5e)、[增加 session 导航和上下文工具](https://github.com/sdaAlbert/sigma-agent/commit/cabdd3b)。

同一时期还开始补“到预算末尾怎样收尾”。`7a9c342` 在剩余轮次进入 finalization phase，要求停止宽泛探索，优先处理已有测试或 diff 明确暴露的问题，并为检查保留有限工具权限；也加入重复相同失败命令的暂停机制。这是早期偏硬的 workflow 控制：当它把“建议检查”变成强制动作时，可能让环境不支持测试、或测试不适用的任务卡在结束前。11 月 5 日 `909c58d` 将验收要求改为 advisory，之后才逐步收敛为保留状态、提供恢复提示、避免不必要地冻结运行。提交：[建立 workflow/runtime 基线](https://github.com/sdaAlbert/sigma-agent/commit/7a9c342)；[将 review gate 改为 advisory](https://github.com/sdaAlbert/sigma-agent/commit/909c58d)。

### 1. 从“能调用工具”转向保护真实仓库修改（2026-11-05）

首批编码评测暴露出，Agent 可以发出合法工具调用，却仍可能因为整文件覆盖、越界路径或过期读取把仓库改坏。提交 `897682f` 把一部分约束放到了 Sigma 工具边界：文件写入前核对读取版本；把整文件替换限制在 32 KiB；为 benchmark 增加 workspace 文件范围和明确的命令网络配置；并给运行与评分增加可追溯元数据。之后的 `dc1cd0e` 又加了大文件异常缩减保护：对超过 100 行且一次缩短超过一半的文件拒绝整块修改，并返回“重读受影响区域、做局部编辑、再看 diff 和定向测试”的恢复方向。

这类保护不是代码理解能力，也不是完整的操作系统沙箱。它们负责阻止明显危险的编辑，并把恢复动作说清楚；模型仍要自己找到正确模块和实现。

**运行证据：**

- #12276 的旧运行曾把失败 `git show` 的输出重定向到源码，覆盖并损坏了文件。后续启用编辑保护的重测官方 resolved，F2P 2/2、P2P 117/117。详见[任务记录](../evals/swebench-verified-mini/tasks/README.md#django__django-12276)。
- #36（Sphinx #8056）曾把约 340 行的 `sphinx/util/docfields.py` 截到 75 行，且没有最终测试。它说明旧工具链没有把整文件误写挡住；后续 runtime 增加的保护避免同类截断再次发生，但保护本身没有立刻让目标实现正确。详见[旧版 #36 归档](../../archive/benchmarks/swebench-verified-mini/runs/batch-20261106-priority-retests-36-27-15/sphinx-doc__sphinx-8056/result-summary.md)。
- #10673 的尝试中，整文件大幅缩短被拦截后，Agent 改用局部编辑并完成任务；官方 resolved 1/1。它证明保护可以和正常修复共存，但单题不构成普遍成功率提升的证据。详见[任务记录](../evals/swebench-verified-mini/tasks/README.md#sphinx-doc__sphinx-10673)。
- #12273 仍未通过官方目标测试，说明文件安全、diff 和验证约束不会自动教会模型正确的 Django 继承语义。

### 2. 把“必须完成检查”改成可判断的 workflow 状态（2026-11-05 至 11-07）

早期 runtime（从 `7a9c342`，2026-11-04 起）曾在 Agent 想结束时硬要求“最近一次写入之后必须有相关测试和 `git_diff`”，未完成就拦下最终回答，并把 Agent 拉回去继续。这能避免漏掉检查，但也会把无适用测试、环境测试跑不起来或其他合理完成状态误判成必须继续。`909c58d`（2026-11-05）将硬拦截改为 advisory 状态；runtime 仍记录最后一次写入、测试、测试结果和 diff 检查的先后关系，并在工具结果里提示哪些证据过期或缺失，但不再因缺一项就冻结 Agent。

同一阶段增加了重复失败命令的 no-progress 检查：达到配置次数且工作区没有变化时暂停并指出需要改变命令或采取修复动作。它针对的是一模一样的失败重试；对“命令略有变化但重复同一错误假设”的识别仍有限。

`5cc8881` 把动态的检查状态和恢复提示从每轮追加的 system prompt 移到独立的 `workflow_policy` / recovery message。这样运行时约束仍然存在，system prompt 与工具描述可以保持稳定，有利于支持前缀缓存，也更容易分别审查“规则是什么”和“本轮状态是什么”。它不是把全部限制写进 Skill：必须由 runtime 执行或记录的状态继续留在 Sigma 代码里。

**已知取舍：**硬门禁太严会中断可用工作；完全没有反馈又会让 Agent 忘记测试、diff 和恢复动作。当前做法是记录状态并给软提示，少数重复命令用可配置保护停下来。`verification_status` 是 Agent 最终流程状态，不替代官方 evaluator；例如最新 #8056 运行状态为 `incomplete`，官方候选评分仍 resolved。

### 3. 让长输出和长任务可检查、可续跑（2026-11-06 至 11-07）

长代码任务里，工具结果过小会截掉报错上下文，过大又会挤满模型上下文。Sigma 增加了较大的分块读取、带 offset 的 artifact 读取、完整工具输出的会话级保存、输出预览与会话预算；压缩上下文时同时记录前后估算 token 数。会话事件还保留工具调用、写入、测试和 diff 的顺序，方便续跑前判断是否真的有进展。

预算策略也改为看证据而不是机械加轮：128 轮后只有发现跟踪源码的相关修改、测试变好，或剩下一个具体且能验证的修复，才考虑同一 session 有界续跑；反复探索、覆盖已有修改或没有源码进展时停止并保存候选。这条续跑决策是 Codex 的实验流程指导，不是 Sigma 模型内部自动推理能力，不能算作 Sigma 自己学会收敛。

**效果边界：**增加轮数和保留 session 能降低“刚找到方向就被预算截断”的风险，但也可能放大循环和 token 消耗。#35 到 200 轮后没有新增源码改动，说明只加预算无效；#10673、#8475 等运行则体现了保留轨迹、同 session 续跑的操作价值。详见[运行问题与恢复方法](../evals/swebench-verified-mini/experiment-method.md#operational-issue-patterns-and-recoveries)和[实验方法](../evals/swebench-verified-mini/experiment-method.md)。

### 4. 修正 DeepSeek thinking 与消息协议适配（2026-11-05 至 11-07）

Sigma 最初的 OpenAI-compatible adapter 对 DeepSeek 显式发送 `thinking: disabled`，并没有保存或回传 `reasoning_content`。Pi #12406 的轨迹对比才发现 Pi 在 thinking 模式，而 Sigma 不是。随后 Sigma 加入 `deepseek_thinking_enabled` 和 reasoning effort 配置，流式解析保留 thinking block，并在后续请求里按 DeepSeek 协议回传。该改动是 provider adapter 能力，不是 Skill 文本。

打开 thinking 后又暴露了两个协议边界：

1. 有时模型会返回 thinking 但没有文本或 tool call。把这种 assistant 消息按 `content: null` 发回会触发 DeepSeek HTTP 400（`content or tool_calls must be set`）。Sigma 后续过滤了这类空 assistant history 项，并为连续缺失 finish reason 加了有限恢复重试；超过次数就明确结束为 incomplete，而不是无限重试。
2. #7590 的 400 不是 DeepSeek 崩溃，而是 compaction 后将进度文本排在尚未回复的 tool result 前面，违反 OpenAI tool-call 顺序。`2bbb9c2` 修正 serializer 顺序，并在发送前验证每个 tool call 都有先行且匹配的 tool response。

**运行证据与限制：**#11885 的旧 session 出现空 assistant 消息，适配修复后的新运行 resolved 1/1、F2P 1/1、P2P 43/43；#12406 在 Sigma thinking 打开后 41 轮 resolved，F2P 3/3、P2P 165/165。#7590 的序列化错误得到定位和修复，但后续候选仍因目标测试未通过/预算消耗而没有 resolved。#12406 的 Skill 快照、Memory 条件和 steer 时机也和 Pi 不完全一致，因此这些结果不能证明 thinking 单独带来了成功。详见[Pi 对照记录](../evals/swebench-verified-mini/tasks/README.md#django__django-12406)、[实验方法中的运行问题与恢复记录](../evals/swebench-verified-mini/experiment-method.md#operational-issue-patterns-and-recoveries)及[thinking 归因说明](../evals/swebench-verified-mini/experiment-method.md#deepseek-thinking-mode-and-result-attribution)。

### 5. 用实测缓存数据优化请求稳定性（2026-11-06 起）

为判断 DeepSeek 前缀缓存是否工作，adapter 开始解析 provider 返回的 cache-read/cache-miss 计数并写入运行事件和 `run.json`。稳定的 system prompt 与工具顺序、将 issue 和变化的状态放在稳定前缀之后，是为了减少前缀变化；compaction 仍可能改变前缀，因此应看实际命中数，不用总输入 token 猜测。

最新 #8056 使用 DeepSeek Flash thinking，记录到 1,028,260 输入 token，其中 636,160 是 cache-read。

### 6. 对照 Pi 后扩展 Sigma 自己的工具反馈（2026-11-07）

Sigma 对 Pi 行为的借鉴主要是明确能力差距，再用 Go 实现，不是复刻 Pi 的 SDK。比如加大可见文件/工具输出、支持 offset 继续读取、把超长结果保存为 session artifacts；workflow 状态改成每次工具调用后的短反馈；read-only、write、command 等工具仍由 Sigma 的工具 registry、工作区作用域和并发控制管理。具体实现映射见 [PI_REFERENCE.md](PI_REFERENCE.md)。

这些属于工具可用性和运行可靠性改进。Pi #12406 在 67 轮 resolved，Sigma 的后续 thinking/guided run 在 41 轮 resolved，但模型设置、Skill 版本、Memory、提示时机并未完全一致；不能把轮数差异写成 Sigma 已追平或超过 Pi。

## 哪些不属于 Sigma 运行时迭代

以下工作对实验很重要，但应单独统计，不应说成 Sigma 的代码解题能力提升：

- **Skill 内容：**Python/Sphinx/C++ 等仓库工作流和题型经验，属于按需加载的模型指导。Skill 有帮助的假设必须结合是否加载、轨迹是否遵循、官方评分判断。
- **Codex 实验操作：**选题、准备精确 base checkout、镜像预检、gold control、决定续跑、记录 F2P/P2P、归档与清理。这些不在 Sigma Agent loop 中。
- **配置变化：**模型、thinking 开关、Memory、token/turn 预算、网络设置会改变运行条件；不能默认算作 runtime 代码变更。

## 结论和下一步判断

Sigma 迭代最明确的进步是：降低大文件误改风险；让工具失败可恢复、让检查状态可见但不过度拦截；保存可续跑的长输出和任务状态；修复 thinking 开启后的 DeepSeek 消息兼容和工具调用顺序；测量缓存而不是猜测。它们减少了运行时制造的失败，并让问题更容易定位。

这些变化没有消除模型的项目理解限制。#12273、#8548 等问题说明，安全门禁、消息协议和更多轮数都无法替代正确定位调用路径、维护兼容行为和设计有效实现。下一步应继续用成对的 runtime build、相同 issue/model/Skill/config 和官方 evaluator 做对照；现有多批次结果可用于定位问题和提出假设，不宜算成 runtime 单变量实验。

## 主要代码和记录

- 具体实现入口：
  - 文件写入大小、读取版本校验、workspace 范围和异常缩短保护：[internal/workspace/tools.go](../internal/workspace/tools.go#L29)、[write/edit 工具实现](../internal/workspace/tools.go#L736)、[大文件缩短检查](../internal/workspace/tools.go#L1638)。
  - 测试/diff 状态、恢复反馈、重复失败命令暂停和阶段检查点：[internal/agent/workflow_policy.go](../internal/agent/workflow_policy.go#L9)；runtime 如何记录状态及统计运行：[internal/agent/runtime.go](../internal/agent/runtime.go#L341)。
  - 长工具输出落盘及按 offset 读取：[internal/agent/tool_output.go](../internal/agent/tool_output.go#L60)；上下文压缩和工具结果裁剪：[internal/agent/context.go](../internal/agent/context.go#L69)、[internal/agent/compact_context.go](../internal/agent/compact_context.go#L151)。
  - DeepSeek thinking 的解析、回传、空 assistant 过滤、tool-call 顺序校验和缓存计数：[internal/agent/openai_chat_completions.go](../internal/agent/openai_chat_completions.go#L115)、[请求消息组装与顺序检查](../internal/agent/openai_chat_completions.go#L130)、[adapter 测试](../internal/agent/openai_chat_completions_test.go)。
  - 将 usage、验证状态、压缩和缓存计数写入运行结果：[cmd/sigma-agent/main.go](../cmd/sigma-agent/main.go#L685)。
- [`897682f` — 工具与 workflow 安全基线](https://github.com/sdaAlbert/sigma-agent/commit/897682f)；[`909c58d` — 验收门禁改为 advisory](https://github.com/sdaAlbert/sigma-agent/commit/909c58d)；[`dc1cd0e` — benchmark 工作流改进](https://github.com/sdaAlbert/sigma-agent/commit/dc1cd0e)；[`5cc8881` — workflow 提示移出 system prompt](https://github.com/sdaAlbert/sigma-agent/commit/5cc8881)；[`2bbb9c2` — 校验 OpenAI tool response 顺序](https://github.com/sdaAlbert/sigma-agent/commit/2bbb9c2)。
- [Sigma 与 Pi 的实现映射](PI_REFERENCE.md)
- [DeepSeek thinking 实验归因](../evals/swebench-verified-mini/experiment-method.md#deepseek-thinking-mode-and-result-attribution)
- [50 题最新结果与逐题分析](../evals/swebench-verified-mini/evaluation-results.md)
- [运行问题与恢复方法](../evals/swebench-verified-mini/experiment-method.md#operational-issue-patterns-and-recoveries)
