# SWE-bench Verified Mini（50 题）Sigma Agent 评测计划

更新日期：2026-11-06

## 公开参考成绩（SWE-bench Verified 大规模评测）

| 模型 / 设置 | SWE-bench Verified | 公开成绩的口径 |
|---|---:|---|
| Claude 3.7 Sonnet「High」 | **70.3%** | Anthropic 的 high-compute 结果：并行生成多个补丁、用可见测试筛选，再用评分模型挑选；在可运行的 489 题上评测。单次基础结果是 63.7%。[Anthropic 评测说明](https://www.anthropic.com/news/claude-3-7-sonnet) |
| OpenAI o4-mini High | **68.1%** | OpenAI 内部工具脚手架，在固定的 477 题上，每题跑 4 次计算 pass@1。[OpenAI system card](https://cdn.openai.com/pdf/2221c875-02dc-4789-800b-e7758f3722c1/o3-and-o4-mini-system-card.pdf) |
| Claude Opus 4.1 | **74.5%** | Anthropic 公布的 Verified 成绩。[Anthropic 公告](https://www.anthropic.com/news/claude-opus-4-1) |
| MiniMax M3 | **80.5%** | MiniMax 公布；Claude Code 脚手架，覆盖默认 system prompt，跑 4 次取平均。[MiniMax 方法说明](https://www.minimax.io/blog/minimax-m3) |
| DeepSeek V4 Flash | **79%** | 公开报告的成绩；属于厂商报告值，评测设置不一定和其他行一致。[成绩汇总](https://evals.report/benchmarks/swe-bench-verified?tab=scores) |

上表是 SWE-bench Verified 大规模评测的公开成绩（不同报告实际运行题数和方法见各行口径），不是同一 Mini 50 题集的成绩，也不是同条件的 Sigma 对照。Mini 50 上的公开运行结果见下表，二者不可混算或直接比较。

截至 2026-11-05，HAL 的 SWE-bench Verified Mini leaderboard 收录了两种 Agent scaffold 的 50 题结果。以下列出代表性结果，作为本计划的公开参照：

| Scaffold | 模型 | Resolved | 公开运行数 |
|---|---|---:|---:|
| SWE-Agent | o4-mini Low | 54.0% | 1 |
| SWE-Agent | Claude 3.7 Sonnet High | 54.0% | 1 |
| SWE-Agent | Claude Opus 4.1 High | 54.0% | 1 |
| SWE-Agent | o4-mini High | 50.0% | 1 |
| SWE-Agent | GPT-5 Medium | 46.0% | 1 |
| HAL Generalist Agent | Claude Opus 4.1 High | 46.0% | 1 |
| SWE-Agent | DeepSeek V3 | 24.0% | 1 |
| HAL Generalist Agent | DeepSeek V3 | 10.0% | 1 |

来源：[HAL SWE-bench Verified Mini leaderboard](https://hal.cs.princeton.edu/swebench_verified_mini)。该页面显示共 33 次评测、2 种 scaffold、18 个模型；页面还提示其成本和 token 统计存在重复计数问题，因此这里不引用成本数据。表中分数使用不同模型和 scaffold，也未必与本计划固定的 dataset revision、Sigma 提示、工具、预算及运行环境完全一致；它们是背景参照，不能当作 Sigma 的同条件对照或因果比较。

**时效性说明：**这不是当前 coding agent 的 SOTA 表。HAL 页面已暂停更新 Mini 榜，称其近期重点转向 Agent 可靠性研究，因此此处成绩主要是历史参照。近期公开报告更多采用完整 SWE-bench Verified、SWE-bench Pro、Terminal-Bench 或厂商内部任务集；SWE-bench 官方 Verified leaderboard 仍有 500 题结果，但 OpenAI 在 2026-02 表示 Verified 的设计与数据污染问题已使其难以提供有意义的前沿能力信号，并建议报告 SWE-bench Pro。见 [OpenAI：Why we no longer evaluate SWE-bench Verified](https://openai.com/index/why-we-no-longer-evaluate-swe-bench-verified/) 与 [SWE-bench 官方 leaderboard](https://www.swebench.com/)。因此 Mini 50 仍适合本项目做低成本、可复现的回归和迭代检查，但不应作为当前大厂 coding agent 水平的排名依据。

数据集抽样方式以[数据集作者仓库](https://github.com/mariushobbhahn/SWEBench-verified-mini)为准：作者说明最终 50 题通过聚类与线性规划优化，以尽量贴近完整 Verified 的表现、测试通过率和难度分布，并减少 Docker 环境体积。HAL 页面摘要把 Mini 称为“random subset”；两者表述不一致，因此本计划沿用数据集作者对该固定 Mini 数据集的说明，不把它描述为简单随机抽样。

## 目标与边界

用固定的 SWE-bench Verified Mini 50 题子集评估 Sigma Agent 的端到端 issue 修复能力。先进行单题试跑，确认模型、仓库准备、Docker 测试和官方评分链路；通过后再逐题顺序执行。后续评测先统一使用 DeepSeek `deepseek-flash`，固定 Sigma 运行配置、Skills/记忆开关、提示词和每题预算；与此前 MiniMax M3/M3.1 结果分开记录和比较。旧批次按实际使用的模型记录，不回写历史结果。

### Pi Agent 的代码能力公开数据与设计：互补参照，不是同一方向的分数对比

Pi 的公开项目定位是“精简、可扩展、由用户按工作流定制”的 Agent harness：核心提供可用的默认能力，但把扩展留给 extensions、Skills 和 packages；Pi 明确选择不内置 plan mode 和 sub-agents。这些是设计取舍，不是性能分数，也不表示 Sigma 必须照搬所有取舍。[Pi 上游 README](https://github.com/earendil-works/pi)

在更贴近 issue 修复的公开测试中，Arena 的 HarnessTax 研究把 Pi、Claude Code、Codex CLI 与多个模型放在同一协议下，在 SWE-bench Lite 随机抽出的 30 题上每组跑 3 次，并用官方 evaluator 评分。报告中 Claude Fable 5 + Pi 的尝试成功率为 **96.7%**，Claude Code 为 **97.8%**，Codex CLI 为 **96.7%**；这项研究的结论是，在所测任务上 Pi 可以达到相近成功率，同时部分组合成本更低。该数字是**Fable 5 + Pi 的组合成绩**，不是 Pi 单独的分数；它只覆盖 30 道抽样题和一次研究配置，不是完整 SWE-bench Lite 或 Verified 榜单成绩，也不能拿来和 Sigma 的 Mini 50 分数直接比较。[HarnessTax 研究及方法](https://canary.arena.ai/blog/coding-agents-harness-tax)

Pi 也有更偏终端操作与长流程的公开评测。以下数字来自 **Cline 团队的独立运行**，不是 Pi 官方成绩：Terminal-Bench 2.0 全集 89 题、pass@1，Pi-Code `@mariozechner/pi-coding-agent` 0.73.1，经 OpenRouter 调模型，在 Modal 上运行。Pi 组合结果如下：

| 模型 + Pi scaffold | Terminal-Bench 2.0 | 未评分任务 |
|---|---:|---:|
| Kimi K2.7 Code，medium reasoning | 55/89 = 61.8% | 2 |
| GLM-5.2，medium reasoning | 51/89 = 57.3% | 1 |
| MiniMax M3，medium reasoning | 42/89 = 47.2% | 0 |
| MiMo V2.5 Pro，medium reasoning | 43/89 = 48.3% | 1 |

来源：[Cline 的运行说明](https://github.com/cline/benchmark-results/blob/main/terminal-bench/2026-06-open-weights/README.md)和[完整结果表](https://github.com/cline/benchmark-results/blob/main/terminal-bench/2026-06-open-weights/summary.md)。它固定的是当时的 Pi 版本、模型路由和运行配置；不代表当前 Pi 版本，也不能把某个百分比归因于 Pi 单独的能力。Terminal-Bench 2.0 的公开运行还是历史版本；2.1 后来修复了 2.0 的 89 题中 28 题的问题。[Terminal-Bench 2.1 更新说明](https://www.tbench.ai/news/terminal-bench-2-1)

这份 Mini 50 计划测的是**给出真实仓库和 GitHub issue 后，Agent 能否做出通过官方 evaluator 的补丁**；Pi 的 SWE-bench Lite 结果直接涉及 issue 修复，Terminal-Bench 则测**Agent 在 Linux 终端环境里执行一组更广泛、较长流程任务的能力**。两个方向互补，分数不能横向比较。当前 Sigma 项目的主要主张是 issue 修复，因此本计划继续把 SWE-bench Mini 作为主测；Pi 的两类公开结果提供背景参照，不作为 Sigma 的目标分数或 Pi/Sigma 胜负依据。若以后要证明 Sigma 的终端操作、环境使用等能力，可以另立 Terminal-Bench 评测，不混入本表。

Docker 镜像准备、网络故障、运行中断和清理一律遵循 [benchmark protocol](benchmark-protocol.md)。

`full-sigma-5` 的 #11815、#11848 和 #11880 首轮使用的是各目标 checkout
自己的 `.sigma-agent/MEMORY.md`，未加载仓库外的 `AGENTS.md` profile。现在增加了
`configs/swebench-verified-mini/task-overrides/django-11880-shared-profile.json`：它会把 Sigma 的
`AGENTS.md` 外置注入，并从 Sigma 仓库外的共享 Django 记忆源检索，目标 checkout
不需要写入这些配置。若用它续跑 #11880，应标记为 profile 更新后的续跑，不与原
条件混报；后续题目使用共享 profile 时也应单独标明评测条件。

这个子集来自 500 题人工验证的 SWE-bench Verified。Mini 数据集作者用聚类与优化选择 50 题，试图近似原数据集的难度、通过率和模型表现分布，并把所需 Docker 环境压缩到 Django/Sphinx 相关环境；它不是从 500 题中简单随机抽 50 题。50 题成绩适合作为小型、可复现的项目评测证据，不等同于官方 500 题成绩。

## 固定数据

- Hugging Face 数据集：`MariusHobbhahn/swe-bench-verified-mini`，split `test`，50 题。
- 数据集 revision：`b316c349947c29963fce3f4a65967c9807a4b673`。
- Dataset Server 读取时间：2026-11-03；下面清单固定这次拿到的 50 个 `instance_id` 与 base commit。
- Issue 输入仅含 `problem_statement`。不给 Agent `patch`、`test_patch`、`hints_text`、PR 链接、目标测试名或维护者方案。
- 评分使用同一实例的官方测试补丁及 SWE-bench evaluator；官方补丁/测试补丁仅在隔离的评分数据目录和 evaluator 进程中使用，绝不放入 Agent 工作区。

## 实验协议

| 阶段 | 操作 | 记录与判定 |
|---|---|---|
| 0. 固定配置 | 记录 Sigma Git commit、MiniMax 模型 ID、`config.json` 哈希、Skills/Memory 状态、128 turns / 256 tool calls 等预算 | 配置变化后另开实验批次，不混报分数 |
| 1. 准备任务 | 基于清单的 `base_commit` 创建干净 checkout；只输入 issue 原文；Agent 使用隔离工作区和 Docker 命令容器 | 启动前记录 HEAD、工作区干净状态及任务 ID |
| 2. Agent 修复 | 每题单次运行；不注入 PR diff、修复提示、测试补丁或人工中途指导；允许 Agent 自己查看代码、编辑和运行项目命令 | 保存完整轨迹、工具调用、模型错误、耗时、用量、patch 与退出状态 |
| 3. 官方评分 | 对 Agent patch 运行 SWE-bench 官方 evaluator；同时对该题 gold patch 跑 evaluator 作环境校验 | gold patch 若不能通过，标记 infra-invalid，不计入 Agent 分母；候选 patch 的 fail-to-pass 全通过且无新增 pass-to-pass 回归才记 resolved |
| 4. 清理与归档 | 只有 Agent、官方 evaluator 与结果归档全部完成后，才清理不再复用的任务专属资源；保留数据集、日志和可复用环境层 | 中断/未完成的题标记 `invalid/incomplete` 并保留环境；同一时间最多运行一个任务 |

## 单题试跑验收（第 1 题）

1. 数据集第一题：`django__django-11790`，仓库 `django/django`，Django 3.1，base commit `b1d6b35e146aea83b171c1b921178bbaae2795ed`。
2. 先验证任务镜像、容器内 Python/Django 版本、基线工作区和目标测试环境可用；Sigma 的 `run_command` 使用该任务的 Python 镜像，不使用 Go 镜像。
3. 给 Sigma 的唯一任务输入是数据集 `problem_statement`；无 Skill/记忆人工提示，不读目标 patch/test patch/hints。记录本题实际开关，作为后续 50 题统一配置依据。
4. 保存原始候选 patch；官方 evaluator 分别测候选 patch 和 gold patch。对比目标 fail-to-pass、原有 pass-to-pass，报告明确区分“测试通过”“官方 resolved”“环境无效”。
5. 本题完成后再决定是否调整 agent。任何调整后从新实验批次重新开始，不把调参后结果和原批次混合。

### 第 1 题实际操作步骤（2026-11-03）

以下按当时实际执行顺序记录；文件位置相对于 `go-runtime`。完整 Docker 与网络边界见 [benchmark protocol](benchmark-protocol.md)。

1. **固定题目与输入。** 从固定 revision `b316c349947c29963fce3f4a65967c9807a4b673` 的 Mini 数据集读取 `django__django-11790`，核对仓库、版本和 base commit。将题目元数据、原始 `problem_statement` 和数据集清单保存在 `archive/benchmarks/swebench-verified-mini/`；本题输入快照为 `task-01-issue-only.json`。Agent 不接收 gold patch、test patch、hints 或 PR 信息。
2. **准备候选工作区。** 在 `go-runtime/var/benchmark-data/swebench-verified-mini/task-01-seed/` 准备 Django checkout，并确认起始 commit 为 `b1d6b35e146aea83b171c1b921178bbaae2795ed`。运行前保存 Sigma 源码清单及哈希；当时 Sigma 位于 `780238173e1b98101c75f30d6e73c6179077f2f8`，工作区有未提交改动，因此把源码清单哈希 `5EFCDE8CD2BB9B8CBF28E5E5F6D5713F3313E70337E95FEF5F002DBFEB883876` 和配置哈希 `9766C05DA608BBDF68A3726624B1B808B0A8C3872FAB2A9D9735B6345B83D38B` 与结果一起记录。
3. **准备并检查 Docker 环境。** 使用该题官方任务镜像 `swebench/sweb.eval.x86_64.django_1776_django-11790:latest`，构建薄包装命令镜像；其 Dockerfile 删除镜像内的 `/testbed`，并把 Sigma 挂载的 `/workspace` 链接为 `/testbed`。核对 Python 和任务依赖，并确认 Agent 命令实际读写的是候选 checkout。首次 gold 评测因 Windows 生成的 SWE-bench 4.1.0 脚本含 CRLF 未能启动；在隔离 venv 中将脚本写入改为 LF 后重跑，之后才进入有效评测。
4. **运行 Sigma。** 使用当时保存的 pilot 配置和 MiniMax-M3；开启 Skills 和 Memory，关闭 web search、GitHub MCP，容器网络关闭。只传入上述 issue 正文，允许 Agent 在候选工作区检查、修改和运行项目命令。记录开关状态，不据此推断 Agent 一定实际读取了 Skill 或检索了 Memory。保留完整 transcript、trace、stdout/stderr、运行元数据和候选 patch；本次运行 ID 为 `343a5689-9f97-41c2-b630-2371177360d2`。
5. **冻结候选并准备评分输入。** 保存 Agent 原始 patch，不覆盖原件；另生成移除 Agent 自增测试改动的 source-only 诊断 patch。分别记录 patch 应用检查结果，确保评分使用的候选源码来自该次 Sigma checkout。
6. **先做 gold 正控，再评候选。** 在同一 SWE-bench evaluator、任务镜像和 base commit 上先运行官方 gold patch，确认环境与目标测试可用；再分别评原始 patch 和 source-only patch。保存每次 predictions、aggregate report、实例级 report、容器日志及命令输出，按 F2P、P2P 和 resolved 状态判读。
7. **归档后再处理环境。** 将 issue 输入、运行配置、源码/配置哈希、原始及诊断 patch、gold 与候选评分产物、运行日志放入 `archive/benchmarks/swebench-verified-mini/`，Sigma session 记录保存在 `var/runs/343a5689-9f97-41c2-b630-2371177360d2/`。只有 Agent 运行、官方评分和归档均完成后，才按协议判断任务专属镜像、容器与缓存是否可清理；中断时标记 `invalid/incomplete` 并保留环境。

## 50 题固定清单

| # | instance_id | 仓库 / 版本 | base commit | 状态 |
|---:|---|---|---|---|
| 1 | `django__django-11790` | `django/django` / 3.1 | `b1d6b35e146aea83b171c1b921178bbaae2795ed` | ok |
| 2 | `django__django-11815` | `django/django` / 3.1 | `e02f67ef2d03d48128e7a118bf75f0418e24e8ac` | ok |
| 3 | `django__django-11848` | `django/django` / 3.1 | `f0adf3b9b7a19cdee05368ff0c0c2d087f011180` | no（官方测试存在时钟 mock 与实现耦合的疑问：测试固定 `datetime.datetime.utcnow()`，未控制其他时钟 API；固定 2019 可用于边界测试，但不能单凭该 mock 断言读取系统时钟的实现有错） |
| 4 | `django__django-11880` | `django/django` / 3.1 | `06909fe084f87a65459a83bd69d7cdbe4fce9a7c` | ok |
| 5 | `django__django-11885` | `django/django` / 3.1 | `04ac9b45a34440fa447feb6ae934687aacbfc5f4` | 未评分（Agent incomplete；优先重测） |
| 6 | `django__django-11951` | `django/django` / 3.1 | `312049091288dbba2299de8d07ea3e3311ed7238` | ok |
| 7 | `django__django-11964` | `django/django` / 3.1 | `fc2b1cc926e34041953738e58fa6ad3053059b22` | ok |
| 8 | `django__django-11999` | `django/django` / 3.1 | `84633905273fc916e3d17883810d9969c03f73c2` | ok |
| 9 | `django__django-12039` | `django/django` / 3.1 | `58c1acb1d6054dfec29d0f30b1033bae6ef62aec` | ok |
| 10 | `django__django-12050` | `django/django` / 3.1 | `b93a0e34d9b9b99d41103782b7e7aeabf47517e3` | ok |
| 11 | `django__django-12143` | `django/django` / 3.1 | `5573a54d409bb98b5c5acdb308310bed02d392c2` | ok |
| 12 | `django__django-12155` | `django/django` / 3.1 | `e8fcdaad5c428878d0a5d6ba820d957013f75595` | ok |
| 13 | `django__django-12193` | `django/django` / 3.1 | `3fb7c12158a2402f0f80824f6778112071235803` | ok |
| 14 | `django__django-12209` | `django/django` / 3.1 | `5a68f024987e6d16c2626a31bf653a2edddea579` | ok |
| 15 | `django__django-12262` | `django/django` / 3.1 | `69331bb851c34f05bc77e9fc24020fe6908b9cd5` | ok |
| 16 | `django__django-12273` | `django/django` / 3.1 | `927c903f3cd25c817c21738328b53991c035b415` | no |
| 17 | `django__django-12276` | `django/django` / 3.1 | `53d8646f799de7f92ab9defe9dc56c6125448102` | ok |
| 18 | `django__django-12304` | `django/django` / 3.1 | `4c1b401e8250f9f520b3c7dc369554477ce8b15a` | ok |
| 19 | `django__django-12308` | `django/django` / 3.1 | `2e0f04507b17362239ba49830d26fec504d46978` | ok |
| 20 | `django__django-12325` | `django/django` / 3.1 | `29c126bb349526b5f1cd78facbe9f25906f18563` | no |
| 21 | `django__django-12406` | `django/django` / 3.1 | `335c9c94acf263901fb023404408880245b0c4b4` | no（Agent incomplete；优先重测） |
| 22 | `django__django-12708` | `django/django` / 3.1 | `447980e72ac01da1594dd3373a03ba40b7ee6f80` | ok（Agent incomplete；优先重测收尾） |
| 23 | `django__django-12713` | `django/django` / 3.1 | `5b884d45ac5b76234eca614d90c83b347294c332` | ok |
| 24 | `django__django-12774` | `django/django` / 3.1 | `67f9d076cfc1858b94f9ed6d1a5ce2327dcc8d0d` | ok |
| 25 | `django__django-9296` | `django/django` / 3.1 | `84322a29ce9b0940335f8ab3d60e55192bef1e50` | ok |
| 26 | `sphinx-doc__sphinx-10323` | `sphinx-doc/sphinx` / 5.0 | `31eba1a76dd485dc633cae48227b46879eda5df4` | ok |
| 27 | `sphinx-doc__sphinx-10435` | `sphinx-doc/sphinx` / 5.0 | `f1061c012e214f16fd8790dec3c283d787e3daa8` | no |
| 28 | `sphinx-doc__sphinx-10466` | `sphinx-doc/sphinx` / 5.0 | `cab2d93076d0cca7c53fac885f927dde3e2a5fec` | ok |
| 29 | `sphinx-doc__sphinx-10673` | `sphinx-doc/sphinx` / 5.2 | `f35d2a6cc726f97d0e859ca7a0e1729f7da8a6c8` | ok（Agent run incomplete；source-only 官方 resolved） |
| 30 | `sphinx-doc__sphinx-11510` | `sphinx-doc/sphinx` / 7.2 | `6cb783c0024a873722952a67ebb9f41771c8eb6d` | no（新一轮 Agent incomplete；source-only F2P 0/2、P2P 0/7） |
| 31 | `sphinx-doc__sphinx-7590` | `sphinx-doc/sphinx` / 3.1 | `2e506c5ab457cba743bb47eb5b8c8eb9dd51d23d` | incomplete（DeepSeek 400；同会话重试仍 400；诊断候选 unresolved，等待 runtime 修复后再评估） |
| 32 | `sphinx-doc__sphinx-7748` | `sphinx-doc/sphinx` / 3.1 | `9988d5ce267bf0df4791770b469431b1fb00dcdd` | no（重测 Agent incomplete；source-only F2P 0/2、P2P 12/12；暂不优先） |
| 33 | `sphinx-doc__sphinx-7757` | `sphinx-doc/sphinx` / 3.1 | `212fd67b9f0b4fae6a7c3501fdf1a9a5b2801329` | ok |
| 34 | `sphinx-doc__sphinx-7985` | `sphinx-doc/sphinx` / 3.2 | `f30284ef926ebaf04b176f21b421e2dffc679792` | no（原运行 unresolved；MiniMax 重测 incomplete，未评分） |
| 35 | `sphinx-doc__sphinx-8035` | `sphinx-doc/sphinx` / 3.2 | `5e6da19f0e44a0ae83944fb6ce18f18f781e1a6e` | no（Agent incomplete；200 轮封顶） |
| 36 | `sphinx-doc__sphinx-8056` | `sphinx-doc/sphinx` / 3.2 | `e188d56ed1248dead58f3f8018c0e9a3f99193f7` | no（200 轮重测仍 F2P 0/1；不再标优先，是否额外续跑待定） |
| 37 | `sphinx-doc__sphinx-8265` | `sphinx-doc/sphinx` / 3.3 | `b428cd2404675475a5c3dc2a2b0790ba57676202` | ok（Agent incomplete；source-only 官方评分 resolved） |
| 38 | `sphinx-doc__sphinx-8269` | `sphinx-doc/sphinx` / 3.3 | `1e2ccd8f0eca0870cf6f8fce6934e2da8eba9b72` | ok（Agent test_failed；联网补充评分 source-only resolved） |
| 39 | `sphinx-doc__sphinx-8475` | `sphinx-doc/sphinx` / 3.4 | `3ea1ec84cc610f7a9f4f6b354e264565254923ff` | ok |
| 40 | `sphinx-doc__sphinx-8548` | M3 旧运行 128 轮未收敛；2026-11-07 MiniMax M3 新尝试：128 轮 + 同 session 续跑，未完成 | 原 source-only 评分仍 unresolved 0/1；新尝试 incomplete，未评分 | 原 F2P 1/1 | 原 P2P 4/5（`test_instance_variable` 失败）；新尝试定向套件 133 passed、1 环境失败 | 原 128 轮；新续跑累计计数未落盘 | 旧运行约 7 分钟；本次详情见归档 | 原运行 3,113,431 输入 / 17,195 输出；新首段 3,341,304 输入 / 25,911 输出，续跑未结算 | 原问题是默认 autodoc 输出包含继承属性。本次 Skill 新增默认行为保留指导且已在第 1 轮加载；Agent 仍先探索大量轮次，之后添加生产代码/回归测试，最后反复启动宽范围测试。定向套件唯一失败 `test_autodoc` 被复现为基线 docutils meta 节点问题。用户要求停止；未做候选官方评分，原分数不变。归档：[2026-11-07 interrupted rerun](experiments/README.md#archive-access)。|
| 41 | `sphinx-doc__sphinx-8551` | `sphinx-doc/sphinx` / 3.4 | `57ed10c68057c96491acbd3e62254ccfaf9e3861` | ok（Agent incomplete；优先重测收尾） |
| 42 | `sphinx-doc__sphinx-8638` | `sphinx-doc/sphinx` / 4.0 | `4b452338f914d4f6b54704222d70ae8a746e3db5` | no（续跑 incomplete；按用户决定不迭代） |
| 43 | `sphinx-doc__sphinx-8721` | `sphinx-doc/sphinx` / 3.5 | `82ef497a8c88f0f6e50d84520e7276bfbf65025d` | ok（source-only resolved 1/1） |
| 44 | `sphinx-doc__sphinx-9229` | `sphinx-doc/sphinx` / 4.1 | `876fa81e0a038cda466925b85ccf6c5452e0f685` | incomplete（128 轮到限；无 tracked-source 改动；环境保留） |
| 45 | `sphinx-doc__sphinx-9230` | `sphinx-doc/sphinx` / 4.1 | `567ff22716ac258b9edd2c1711d766b440ac0b11` | ok（raw candidate resolved 1/1） |
| 46 | `sphinx-doc__sphinx-9281` | `sphinx-doc/sphinx` / 4.1 | `8ec06e9a1bd862cd713b9db748e039ccc7b3e15b` | MiniMax M3 source-only resolved 1/1；F2P 1/1，P2P 38/38；#9281 已完成 |
| 47 | `sphinx-doc__sphinx-9320` | `sphinx-doc/sphinx` / 4.1 | `e05cef574b8f23ab1b57f57e7da6dee509a4e230` | MiniMax M3 raw resolved 1/1；F2P 1/1，P2P 9/9；#9320 已完成 |
| 48 | `sphinx-doc__sphinx-9367` | `sphinx-doc/sphinx` / 4.1 | `6918e69600810a4664e53653d6ff0290c3c4a788` | ok（source-only 官方 resolved 1/1） |
| 49 | `sphinx-doc__sphinx-9461` | `sphinx-doc/sphinx` / 4.2 | `939c7bb7ff7c53a4d27df067cea637540f0e1dad` | no（Agent incomplete；source-only 官方 unresolved） |
| 50 | `sphinx-doc__sphinx-9698` | `sphinx-doc/sphinx` / 4.3 | `f050a7775dfc9000f55d023d36d925a8d02ccfa8` | ok（Agent finalization failed；source-only 官方 resolved） |

表中对 #11790、#11848、#12273、#12276 分别列出原批次与 2026-11-04 迭代续跑结果。续跑属于独立实验批次，不覆盖原结果，也不与原 50 题批次合并计算总分。

`ok` / `no` 表示官方结果 resolved / unresolved；括号中的 `Agent incomplete` 表示运行或最终验证未收敛。“优先重测”标出可优先补跑的 incomplete；#8548 与 #8638 按用户决定不再重测或迭代。未评分、待测仍单独标识。

## 结果汇总表

`ok` 表示官方评分 resolved，`no` 表示官方评分 unresolved；未评分或未运行项目保留对应状态，不等同于 `no`。

**F2P（fail-to-pass）** 是基准中在未修复的 base 版本上会失败、用于验证 issue 是否修好的测试；表中“F2P 通过 / 总数”表示候选补丁通过了多少个这类目标测试。**P2P（pass-to-pass）** 是 base 版本原本就通过的测试；候选补丁也应继续通过它们，用来检查是否引入回归。只有目标 F2P 全部通过且 P2P 没有回归，官方 evaluator 才会将该题判为 `resolved`。例如 `F2P 2/2，P2P 77/77` 表示两个目标测试都通过，77 个原有测试也都保持通过。

| # | instance_id | Sigma 执行状态 | 官方评分 | F2P 通过 / 总数 | P2P 回归 | 轮数 / 工具数 | 耗时 | Token / 费用 | 备注（含固定清单原状态说明） |
|---:|---|---|---|---:|---:|---:|---:|---:|---|
| 1 | `django__django-11790` | 完成 | unresolved | 0/2 | 77/77 通过 | 44 / 58 | 约 4.5 分钟 | 172,572 / 11,786 tokens | gold 1/1；raw 与 source-only 均未通过 F2P，属性值类型不符；固定清单原状态说明：Pilot：gold 1/1、Sigma unresolved；2026-11-04 迭代续跑：resolved（DeepSeek Flash，F2P 2/2、P2P 77/77） |
| 2 | `django__django-11815` | 完成 | resolved | 2/2 | 44/44 通过 | 72 / 83 | 约 11 分钟（按记录时间估算） | 1,634,776 / 19,227 tokens | code-only candidate 1/1；raw patch 因 `.sigma-agent/MEMORY.md` sidecar 冲突未评分；运行配置启用 Skills/Memory，但 trace 没有 `read_skill` / `search_memory` 调用。；固定清单原状态说明：已评分：resolved |
| 3 | `django__django-11848` | 完成（额度恢复后续跑） | unresolved | 0/2 | 43/43 通过 | 135 / 142（3 次运行） | 约 13 分钟（不含等待额度） | 3,571,625 / 54,527 tokens（累计） | 官方测试中两个 F2P 因 `datetime.datetime` mock 接口不匹配而报错；第一次 evaluator 调用的 CRLF 补丁格式错误，修正为 LF 后评分。续跑前修复了 Skill 误匹配。；固定清单原状态说明：原批次 unresolved；2026-11-04 迭代续跑仍 unresolved（DeepSeek Flash，F2P 0/2、P2P 43/43） |
| 4 | `django__django-11880` | 完成（保存的 issue-only session 续跑） | code-only resolved | 1/1 | 114/114 通过 | 15 / 13（续跑） | 约 1 分钟续跑 | 366,119 输入 / 7,337 输出 tokens | gold control 1/1；原始补丁因 session 生成的 `.sigma-agent/MEMORY.md` 与 evaluator 现存文件冲突，code-only 评分补丁移除此单个 sidecar 后通过。Agent 本地组测 816（9 skip）及 319（22 skip）通过。完整日志：`central archive/legacy\swb5\outputs\archives\2026-11-04-django__django-11880-resumed-m31`。；固定清单原状态说明：已评分：code-only resolved 1/1；raw patch 有 memory sidecar 冲突 |
| 5 | `django__django-11885` | M3 续跑到 128 轮；无目标源码改动 | 未评分（无候选补丁） | — | — | 128 / 130 | 19 分 26 秒 | 3,204,270 输入 / 24,249 输出 tokens | 本地 `delete` 测试通过；没有官方评测。启用 `/workspace` 边界后未再访问 `/tmp`；反复读取包含路径核实和上下文压缩导致的重复。完整记录：`central archive/legacy\swb5\outputs\runs\django-11885-20261104\attempts\m3-continued-after-recheck`。；固定清单原状态说明：M3 已尝试；128 轮无补丁，未评分 |
| 6 | `django__django-11951` | 完成（429 后同 session 续跑） | source-only resolved | 1/1 | 21/21 通过 | 61 / 71 | 约 4 分钟累计模型运行 | 1,328,297 输入 / 34,723 输出 tokens | 本地 bulk_create 组测 27 通过、5 跳过；raw patch 与官方 test_patch 在 imports 处冲突，保留 raw 并对只含源码的独立 patch 评分。归档：`central archive/legacy\swb5\outputs\archives\2026-11-04-django__django-11951`。；固定清单原状态说明：已评分：source-only resolved 1/1；raw patch 与官方 test_patch import 冲突 |
| 7 | `django__django-11964` | 完成（首轮越界后，同 session 加命令路径门禁续跑） | raw resolved 1/1 | 2/2 | 15/15 通过 | 57 / 63（续跑） | 约 14 分钟续跑 | 1,473,611 输入 / 79,611 输出 tokens（续跑） | 首轮仍记 `invalid/incomplete`；续跑继承其越界读取上下文，因此不是 pristine issue-only 结果。`command_filesystem_scope=workspace` 在 Docker 启动前拦截显式越界路径；本地目标组 120 测试、1 跳过通过；官方评测无错误。归档：`central archive/legacy\swb5\outputs\archives\2026-11-04-django__django-11964-boundary-guard-rerun`。；固定清单原状态说明：官方评分 resolved 1/1（边界门禁后同 session 续跑；继承首轮越界读取上下文，不算 pristine issue-only） |
| 8 | `django__django-11999` | 完成（MiniMax 流错误后同 session 续跑） | source-only resolved | 1/1 | 30/30 通过 | 125 / 144（两次运行合计） | 约 38 分钟 | 3,128,686 输入 / 209,711 输出 tokens | Agent 本地 `model_fields` 313 项通过、2 跳过；raw patch 因 Agent 测试文件与官方 test patch 同路径而未评分，source-only patch 官方评分无错误。归档：`central archive/legacy\swb5\outputs\archives\2026-11-04-django__django-11999`。；固定清单原状态说明：已评分：source-only resolved 1/1；raw patch 与官方 test patch 路径冲突 |
| 9 | `django__django-12039` | 完成（Token Plan 恢复后同 session 续跑；M3.1 中断后切换 M3） | source-only 与 raw 均 resolved | 1/1 | 12/12（F2P 1/1、P2P 11/11） | 185 / 216（含 1 次额度失败重试；M3 续跑 96 / 95） | M3 续跑约 6 分 19 秒；评分约 34 / 28 秒 | 4,734,474 输入 / 303,264 输出 tokens（混合模型累计） | M3 续跑本地定向测试 51 通过、1 跳过；官方 source-only 与 raw 补丁均通过，0 evaluator errors。该结果包含 M3.1 首段，不能记为纯 M3 从头运行。归档：`central archive/legacy\swb5\outputs\archives\2026-11-04-django__django-12039`。；固定清单原状态说明：已完成：历史 M3.1 阶段因 Token Plan 中断，同一 session 改用 M3 续跑；source-only 与 raw 均 resolved 1/1 |
| 10 | `django__django-12050` | 完成（中断后续原 session） | resolved | 1/1 | 10/10 通过 | 88 / 86（累计） | Agent 运行约数分钟，官方测试约 30 秒 | 2,095,876 输入 / 22,456 输出 tokens | DeepSeek Flash；Skills/Memory 配置开启，联网、MCP 关闭。74 个本地 focused tests 通过；官方 source-only patch resolved。审计：0 次 read_skill；1 次 memory 查询只返回测试环境信息。通用系统提示含“保留领域类型”原则，属于相关但非解法的引导。首次连接中断后，第一次续跑被 Windows NTFS symlink 的 Git 预检阻塞（56 次调用）；修复后继续同 session 完成。报告与日志：`archive/benchmarks/swebench-verified-mini/runs/django-12050-20261104/official-eval`。；固定清单原状态说明：已评分：DeepSeek Flash，source-only resolved 1/1 |
| 11 | `django__django-12143` | 完成 | resolved | 1/1 | 52/52 通过 | 19 / 25 | 约 1 分钟（含评分约 30 秒） | 266,136 输入 / 3,595 输出 tokens | issue-only 正文（官方 issue 自带 `re.escape()` 建议）；Skills/Memory 开启但 trace 无读取调用；本地定向 3/3 通过；官方测试补丁先还原测试文件后应用成功。报告：`archive/benchmarks/swebench-verified-mini/runs/django-12143-20261104/official-eval`。；固定清单原状态说明：DeepSeek Flash：官方 resolved 1/1 |
| 12 | `django__django-12155` | 完成 | resolved | 1/1 | 6/6 通过 | 24 / 27 | 约 1 分钟 Agent，评分 29 秒 | 408,472 输入 / 4,075 输出 tokens | issue-only 正文（官方 issue 自带 `lines[1:]` 建议）；Skills/Memory 开启但无读取调用；本地 `admin_docs` 61/61 通过；官方测试补丁应用成功。报告：`archive/benchmarks/swebench-verified-mini/runs/django-12155-20261104/official-eval`。；固定清单原状态说明：DeepSeek Flash：官方 resolved 1/1 |
| 13 | `django__django-12193` | 完成 | resolved | 1/1 | 11/11 通过 | 15 / 17 | Agent 约 1 分钟，评分 28 秒 | 172,585 输入 / 2,610 输出 tokens | issue-only 正文直接解释了 `CheckboxInput.get_context()` 修改传入 attrs；Sigma 没有收到额外 diff 或提示。Skills/Memory 开启但 trace 无 skill/memory 检索；完成后自动写入 Django 共享记忆 2 条。Agent 报告本地 137 项通过（含 110 跳过）；官方共运行 122 项（110 跳过），目标 F2P 通过。评测日志记录首次候选补丁应用失败，官方流程重置测试文件后成功应用；最终 report 为 `patch_successfully_applied=true`。无单独 gold control。详见 `archive/benchmarks/swebench-verified-mini/runs/django-12193-20261104/official-eval`。；固定清单原状态说明：DeepSeek Flash：官方 resolved 1/1 |
| 14 | `django__django-12209` | 完成 | resolved | 1/1 | 0/0 | 31 / 37 | Agent 约 2 分钟，评分 30 秒；neutral control 29 秒 | 593,728 输入 / 6,254 输出 tokens | issue-only；Skills/Memory 开启，1 次 memory 查询、0 次 Skill 读取；结束后自动写入记忆 1 条。官方测试 4/4 通过；no-op 注释补丁 0/1（4 个 serializer 测试因重复 UUID 主键报错），证明评分依赖行为变化。F2P 标签是测试输出中的 `functools.partial` 说明而非常规测试名。Sigma 自评未完成直接 `save()` 场景并发现一个多余 helper；其 `not raw` 核心改动与官方参考补丁一致，故按官方 fixture/serializer 验收记 resolved，同时注明覆盖范围限制。空补丁控制未运行（evaluator 跳过空补丁）。详见 `archive/benchmarks/swebench-verified-mini/runs/django-12209-20261104/official-eval`。；固定清单原状态说明：DeepSeek Flash：官方 resolved 1/1；neutral control 0/1 |
| 15 | `django__django-12262` | 完成（source-only 官方评分） | resolved | 4/4 | 18/18 通过 | 45 / 48 | 约 3 分钟 | 953,210 输入 / 6,828 输出 tokens | Gold control 1/1；Python Skill 在第 1 轮加载，Memory 开启（38 条）；候选 source-only patch 官方 resolved 1/1，0 evaluator errors。Agent 的原始 patch 含测试修改，单独保留；官方评分使用 source-only。修复 `parse_bits()` 对默认 keyword-only 参数的识别；旧结果 2026-11-04 unresolved，F2P 3/4、P2P 18/18（当时 trace 未读 Skill）。本次独立重测解决；归档：`archive/benchmarks/swebench-verified-mini/runs/batch-20261106-priority-retests-36-27-15/django__django-12262/result-summary.md`。；固定清单原状态说明：原批次 DeepSeek Flash unresolved、F2P 3/4；2026-11-06 Python Skill 加载后 source-only 重测 resolved，F2P 4/4、P2P 18/18 |
| 16 | `django__django-12273` | 完成 | unresolved | 0/2（模块语法错误，未执行） | 0/26（模块语法错误，未执行） | 45 / 50 | Agent 约 3 分钟，评分 28 秒 | 837,300 输入 / 10,358 输出 tokens | issue-only；Agent 尝试修改 `_get_pk_val()` 时把 `django/db/models/base.py` 截断并写入 `RuntimeError("truncated")`；官方 patch 可应用，但模块 `SyntaxError: unexpected EOF`，F2P/P2P 均未执行。环境正常，属于 Agent 编辑事故。详见 `archive/benchmarks/swebench-verified-mini/runs/django-12273-20261104/official-eval`。；固定清单原状态说明：原批次 unresolved（候选截断源码）；2026-11-04 迭代续跑仍 unresolved（F2P 0/2、P2P 8/27） |
| 17 | `django__django-12276` | 达到 128 轮上限，未完成收尾 | unresolved | 2/2 | 98/117 通过（19 回归） | 128 / 126 | Agent 约 10 分钟，评分 29 秒 | 2,746,844 输入 / 52,879 输出 tokens | issue-only；Agent 截断并手工重建 `django/forms/fields.py` 后导致大量表单回归；官方目标测试通过，但 P2P 19 项失败。最终 run 未完成（要求在最后修改后补 `git_diff`）；没有 Codex 修补。详见 `archive/benchmarks/swebench-verified-mini/runs/django-12276-20261104/official-eval`。；固定清单原状态说明：原批次 unresolved（F2P 2/2、P2P 98/117）；2026-11-04 迭代续跑 resolved（F2P 2/2、P2P 117/117） |
| 18 | `django__django-12304` | 未运行 | 未评分 | — | — | — | — | — | 固定清单原状态说明：2026-11-05 DeepSeek Flash：官方 resolved 1/1；F2P 1/1、P2P 17/17 |
| 19 | django__django-12308 | 完成（Python Skill 独立重测；verified） | resolved | 2/2 | 20/20 通过 | 29 / 34 | Agent 约 3 分钟；gold 42 秒、candidate 35 秒 | 452,421 输入 / 4,436 输出 tokens（缓存命中 391,040） | 2026-11-05 原结果 unresolved；本次 Skills/Memory 开启，Skill 已加载，gold 1/1、candidate resolved 1/1，0 evaluator errors。Memory 35 条起始快照，未写入新事实。归档：[重测摘要](experiments/README.md#archive-access) |
| 20 | django__django-12325 | 完成（Python Skill 独立重测；verified） | unresolved | 0/2 | 201/201 通过 | 53 / 53 | Agent 约 15.5 分钟；gold 50 秒、candidate 50 秒 | 1,226,020 输入 / 7,368 输出 tokens（缓存命中 825,344） | 原运行与 2026-11-05 Skill 前重测均 unresolved；本次 Skill 已加载，gold 1/1、candidate patch 应用成功且 0 evaluator errors。Agent 本地 1,468 项相关测试通过，但官方两项 F2P 仍在 Options._prepare() 因未标记字段触发 ImproperlyConfigured；保留了冲突校验，而官方改动过滤非 parent_link=True 字段并删除该校验。Memory auto-write 1 条已从共享索引清除。归档：[重测摘要](experiments/README.md#archive-access) |
| 21 | `django__django-12406` | 未运行 | 未评分 | — | — | — | — | — | 固定清单原状态说明：初次 run invalid/incomplete（128 轮/137 调用）；续接到累计 160 轮/169 调用后 source-only official unresolved，F2P 1/3、P2P 165/165；详情见续测归档 |
| 22 | `django__django-12708` | 未运行 | 未评分 | — | — | — | — | — | 固定清单原状态说明：新门禁 909c58d：原跑 128 轮未验证；续跑后官方 resolved 1/1（F2P 1/1，P2P 101/101）；Agent 最终状态 incomplete（缺最后 diff 检查） |
| 23 | `django__django-12713` | 未运行 | 未评分 | — | — | — | — | — | 固定清单原状态说明：新门禁 909c58d / DeepSeek Flash：resolved，F2P 1/1，P2P 58/58 |
| 24 | `django__django-12774` | 未运行 | 未评分 | — | — | — | — | — | 固定清单原状态说明：新门禁 909c58d / DeepSeek Flash：官方 resolved 1/1；F2P 1/1，P2P 41/41 |
| 25 | `django__django-9296` | 未运行 | 未评分 | — | — | — | — | — | 固定清单原状态说明：新门禁 909c58d / DeepSeek Flash：官方 resolved 1/1；F2P 1/1，P2P 20/20 |
| 26 | `sphinx-doc__sphinx-10323` | 未运行 | 未评分 | — | — | — | — | — | 固定清单原状态说明：2026-11-05 新门禁 909c58d / DeepSeek Flash：官方 resolved 1/1；F2P 1/1，P2P 40/40 |
| 27 | `sphinx-doc__sphinx-10435` | 完成（有效运行 attempt 2） | unresolved（source-only） | 0/1 | 74/74 通过 | 49 / 54（另有无效 attempt 18 / 25） | 约 3 分钟 | 1,304,648 输入 / 7,358 输出 tokens（有效运行） | Gold control 复用相同 evaluator digest，resolved 1/1；Python 与 Sphinx Skills 第 1 轮加载，Memory 索引 14 条。Agent 改成 `\PYGZsp{}` 输出而非满足官方 `%`/换行边界顺序，目标测试失败，0 evaluator errors。Attempt 1 因 Windows linked-worktree Git 元数据无法被 Linux 容器解析而无效；一次 evaluator patch CRLF 输入错误已修复并重评。Docker 镜像/容器和 linked worktree 已清理；3 个临时 clone 因桌面策略拒绝删除而保留。归档：`archive/benchmarks/swebench-verified-mini/runs/batch-20261106-priority-retests-36-27-15/sphinx-doc__sphinx-10435/result-summary.md`。；固定清单原状态说明：2026-11-05 新门禁批次 unresolved 0/1；本次加载 Skills 的独立重测仍 unresolved 0/1、P2P 74/74 |
| 28 | `sphinx-doc__sphinx-10466` | 未运行 | 未评分 | — | — | — | — | — | 固定清单原状态说明：2026-11-05 Goal batch / DeepSeek Flash：source-only 官方 resolved 1/1（F2P 1/1，P2P 6/6）；raw patch 因 Agent 新增 fixture 与官方 test_patch 重叠而 evaluator patch apply 失败，详见 batch 归档 |
| 29 | `sphinx-doc__sphinx-10673` | Agent incomplete（52轮后限；续跑至66轮） | source-only resolved | 1/1 | 9/9 通过（F2P 1/1、P2P 9/9） | 66 / 75 | 约 5 分钟 | 1,566,041 输入 / 11,786 输出 tokens | Gold 正控复用的同 digest 报告为 1/1；隔离 Memory、Sphinx/Python Skill 均开启。Agent 未完成本地 fixture 测试和最终 diff review，source-only patch 仍通过官方 evaluator；F2P 1/1、P2P 9/9。归档：`archive/benchmarks/swebench-verified-mini/runs/batch-20261106-sphinx-skill-retests-32-30-29/sphinx-doc__sphinx-10673/`。；固定清单原状态说明：旧运行 F2P 0/1；本次 source-only 官方重测 resolved 1/1。 |
| 30 | `sphinx-doc__sphinx-11510` | Agent incomplete（新一轮 40 轮封顶） | source-only unresolved | 0/2 | 0/7 通过（7 项回归） | 40 / 57 | Agent 约 2 分钟；评分 27 秒 | 840,309 输入 / 11,002 输出 tokens（缓存读取 572,928） | Gold 正控 1/1（复用相同数据 revision 与 evaluator digest）。Skills/Memory 开启且 Sphinx/Python Skill 已加载。候选改写 `sphinx/directives/other.py` 的多个既有 directive，7 项 Toctree P2P 全失败；两个 include F2P 也都失败。本轮 cap=40、runtime SHA `A4E17F…`；上轮 cap=128、归档 runtime SHA `633861…`，不是只改 Skill 的受控比较。详见新批次归档 `archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sphinx-skill-retest-30/sphinx-doc__sphinx-11510/`。 后续明确提示加载 Sphinx Skill 的运行因 Docker 网络地址池耗尽及同机 #8548 并发，于 56 轮停止；Skill 加载成功但无源码改动、无官方评分，归档为 invalid/incomplete，不改变前述有效评分。详见 `archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sphinx-skill-guided-retest-30-final/`。；固定清单原状态说明：旧运行 F2P 0/2；上一轮 Skill 重测 F2P 1/2、P2P 7/7；最新 source-only F2P 0/2、P2P 0/7。 |
| 31 | `sphinx-doc__sphinx-7590` | Agent invalid/incomplete（turn 33 DeepSeek 400；同会话重试仍 400） | 诊断 source-only unresolved（不计为完成重测） | 0/1 | 24/24 通过 | 33 / 38（续跑 1 turn、0 tokens） | 约 1 分钟 | 694,050 输入 / 4,264 输出 tokens（缓存读取 643,584） | Sphinx Skill 在第 1 个工具回合加载；候选与官方测试补丁可在干净 base 依序应用。官方诊断评分 0 evaluator errors，目标 `test_expressions` 失败；Agent 未完成 focused verification 与最终 diff。Gold 离线对比显示候选对 `_` 既预消费又要求 regex 再匹配，不能处理 issue 的相邻 `q_J`/`q_s` suffix；generic Skill 未能防止该错配。一次同会话续跑立即重复相同 400，未改源码、无额外 token。未向 Agent 暴露 gold 或评分反馈。归档：[本次结果](experiments/README.md#archive-access)。
| 32 | `sphinx-doc__sphinx-7748` | Agent incomplete（软预算停止） | source-only unresolved | 0/2 | 12/12 通过（F2P 0/2、P2P 12/12） | 66 / 72 | 约 5 分钟 | 1,650,336 输入 / 12,053 输出 tokens | Gold 正控 1/1；隔离 Memory、Sphinx/Python Skill 均开启。运行因预算停止，source-only 两个目标测试均失败、无 P2P 回归；未完成 Agent 最终 diff review。归档：`archive/benchmarks/swebench-verified-mini/runs/batch-20261106-sphinx-skill-retests-32-30-29/sphinx-doc__sphinx-7748/`。；固定清单原状态说明：旧运行 F2P 0/2；本次 Skill 重测仍 F2P 0/2，暂不优先再次重跑。 |
| 33 | `sphinx-doc__sphinx-7757` | 完成（verified） | source-only resolved；raw patch apply error | 1/1 | 33/33 通过（F2P 1/1、P2P 33/33） | 69 / 74 | — | 未记录 | Gold 1/1；source-only candidate 官方 resolved，0 evaluator errors；raw patch 因 Cython cache 产生 patch-apply error。Skills/Memory 按本轮设置加载。Sphinx Skill 单题验证。；固定清单原状态说明：2026-11-06 Sphinx Skill 单题验证：Agent `verified`，69 turns / 74 calls；gold 1/1，F2P 1/1、P2P 33/33。raw patch 因包含 Cython cache 产生 patch-apply error；source-only 官方 resolved 1/1，F2P 1/1、P2P 33/33。 |
| 34 | `sphinx-doc__sphinx-7985` | 原运行 incomplete（160 轮）；MiniMax 重测 incomplete（累计 92 轮后 API 429） | 重测未评分；原 source-only unresolved | 原 F2P 0/2 | 原 P2P 3/3；重测未运行目标测试 | 原 160；重测 80 + 12 成功续接 | — | 原 8,520,136 / 75,616；本次主 session 2,285,576 / 16,588，另有 32 轮误设尝试 691,274 / 7,051 | 原运行 source-only 官方 unresolved、0 evaluator errors；raw fixture 与官方测试冲突。MiniMax 重测使用新 Sphinx Skill（turn 1 已加载），turn 80 记录了沿用现有 linkcheck 分类/queue/status 的修改契约，但未改 tracked source、未跑目标测试。续接遇 MiniMax HTTP 429 Token Plan 用量上限（2056）；无新官方候选评分，不改变原分数。保留环境供额度恢复后续接。详见[重测摘要](experiments/README.md#archive-access)。
| 35 | `sphinx-doc__sphinx-8035` | incomplete（200 轮） | unresolved | 0/1 | 1/2（1 项既有测试失败） | 200 / 220 | — | 5,058,588 输入 / 34,794 输出 tokens | Gold 1/1。source-only 官方 unresolved，0 evaluator errors；目标测试与一项既有 private-members 测试失败。200 轮补丁与 160 轮已评分补丁字节一致；raw patch 含 scratch/Cython 产物，单独保存。Skills/Memory 开启、Sphinx Skill 已加载。归档 `batch-20261106-sphinx-34-39-adaptive/sphinx-doc__sphinx-8035`。
| 36 | `sphinx-doc__sphinx-8056` | 完成（200 轮封顶；Agent incomplete） | unresolved | 0/1 | 40/40 通过 | 200 / 204 完成（另 1 次工具调用中断） | 约 15 分钟 Agent，gold/candidate 各约 26 秒 | 5,537,303 输入 / 31,688 输出 tokens；缓存命中 3,184,768（57.5%） | Gold 1/1；raw patch 应用成功，candidate unresolved、0 evaluator errors。新 runtime 防止了源码截断，但目标测试仍失败：输出把 `x1, x2` 保持为一个参数组。初始两个 focused failures 后聚焦至 `test_domain_py_xrefs` 回归；最终官方 P2P 40/40。reference comparison 显示候选改在下游 docfields，而 NumPyDocstring 参数收集仍未拆分多个名称。高 token 消耗；当前 session 200 轮、5.54M 输入 token。归档：`archive/benchmarks/swebench-verified-mini/runs/batch-20261106-priority-retests-36-27-15/sphinx-doc__sphinx-8056/`。
| 37 | `sphinx-doc__sphinx-8265` | Agent failed finalization（128 轮；漏掉最终 git_diff） | source-only resolved | 1/1 | 22/22 通过 | 128 / 94 | 约 11 分钟 | 3,430,153 输入 / 44,537 输出 tokens；缓存命中 2,075,520（60.5%） | Gold 1/1。Agent 在最后改动后跑了 140 项定向测试（34 deselected），但未执行最终 `git_diff`，runtime 标记 failed；未续跑以控制额外 token。Skill 加载两次，Memory 已检索。Raw patch 与官方 test patch 在 `tests/test_pycode_ast.py` 冲突，source-only patch 仅含 `sphinx/pycode/ast.py` 并通过官方评分，0 evaluator errors。归档：`archive/benchmarks/swebench-verified-mini/runs/batch-20261106-sphinx-34-39-adaptive/sphinx-doc__sphinx-8265/`。|
| 38 | `sphinx-doc__sphinx-8269` | 完成但 verification_status=test_failed（29 轮） | resolved（source-only，补充联网评分） | 1/1 | 5/5 通过 | 29 / 34 | Agent 约 1 分钟；官方候选评分约 116 秒、26 秒、35 秒 | 687,382 输入 / 4,877 输出 tokens；缓存命中 477,952（69.5%） | Gold 1/1。候选生产 hunk 与官方 gold 完全一致。前两次 source-only 评分因外链测试 HTTP 502 为 4/5；第三次使用相同 evaluator 镜像、patch 和默认 bridge 网络复评，`test_defaults_json` 通过，结果 resolved 1/1、F2P 1/1、P2P 5/5、0 evaluator errors。按用户判断记 `ok`，保留前两次失败记录。Agent 自写 fixture 未通过，raw patch 与官方 test patch 冲突；source-only 已评分。补充复评归档：`archive/benchmarks/swebench-verified-mini/runs/batch-20261106-sphinx-34-39-adaptive/sphinx-doc__sphinx-8269/candidate-eval/network-connected-rerun-20261106/`。|
| 39 | `sphinx-doc__sphinx-8475` | 完成（同 session 续跑） | resolved | 1/1 | 18/18（F2P 1/1、P2P 17/17） | 27 / 26（attempt 2 累计；attempt 1 另 14 / 19） | 约 1 分钟续跑；官方 gold/candidate 各约 33 秒 | 两 session 合计 849,166 输入 / 5,145 输出 tokens；续跑 session cache-read 424,192（60.8%） | Gold 1/1；raw candidate 官方 resolved 1/1，0 evaluator errors。Attempt 1 空 finish reason 后 HTTP 400；attempt 2 曾在第 19 轮超时，后恢复同 session 并完成。续跑最终 `verification_status=verified`；生产代码与测试均通过。候选额外对 ConnectionError 也启用 GET retry，目标未要求此扩展，P2P 未发现回归；本次 Skill 使用旧快照，复盘已补充精确异常范围指导。归档：`archive/benchmarks/swebench-verified-mini/runs/batch-20261106-sphinx-34-39-adaptive/sphinx-doc__sphinx-8475/result-summary.md`。|
| 40 | `sphinx-doc__sphinx-8548` | M3 旧运行 128 轮未收敛；2026-11-07 MiniMax M3 新尝试：128 轮 + 同 session 续跑，未完成 | 原 source-only 评分仍 unresolved 0/1；新尝试 incomplete，未评分 | 原 F2P 1/1 | 原 P2P 4/5（`test_instance_variable` 失败）；新尝试定向套件 133 passed、1 环境失败 | 原 128 轮；新续跑累计计数未落盘 | 旧运行约 7 分钟；本次详情见归档 | 原运行 3,113,431 输入 / 17,195 输出；新首段 3,341,304 输入 / 25,911 输出，续跑未结算 | 原问题是默认 autodoc 输出包含继承属性。本次 Skill 新增默认行为保留指导且已在第 1 轮加载；Agent 仍先探索大量轮次，之后添加生产代码/回归测试，最后反复启动宽范围测试。定向套件唯一失败 `test_autodoc` 被复现为基线 docutils meta 节点问题。用户要求停止；未做候选官方评分，原分数不变。归档：[2026-11-07 interrupted rerun](experiments/README.md#archive-access)。|
| 41 | `sphinx-doc__sphinx-8551` | M3 到 128 轮上限，最终 diff 门禁未完成 | source-only resolved | 1/1 | 33/33 通过（F2P 1/1、P2P 33/33） | 128 / 123 | 约 13 分钟 | 3,107,707 输入 / 24,896 输出 tokens | Agent 运行状态为失败，但保留的生产补丁通过官方 evaluator；目标 `test_info_field_list` 和 32 项 P2P 全通过，0 evaluator errors。原始补丁保留未评分。运行问题：反复调用 runner 时单题 JSON 数据形状传错，两次都在创建 evaluator 容器前失败；改回原始单元素数组后评分成功。归档：`central archive/legacy\swb5\outputs\archives\2026-11-04-sphinx-doc__sphinx-8551`。；固定清单原状态说明：已评测：Agent 128 轮未完成最终 diff；source-only 官方评分 resolved 1/1，F2P 1/1、P2P 32/32 |
| 42 | `sphinx-doc__sphinx-8638` | 原始 128 轮 + 52 轮有效离线续跑（另有 20 轮误启用 MCP 的无效片段）；续跑达到 52 轮上限，verification incomplete | source-only unresolved | 0/1 | 33/35（F2P 0/1，P2P 33/34） | 52 / 52（另有无效片段 20 轮） | 约 20 分钟 | 1,439,085 输入 / 7,091 输出 tokens | Skills/Memory 开启，GitHub MCP、web search、容器网络关闭。目标 `test_info_field_list_var` 仍因 `pending_xref` 而失败，P2P `test_domain_py_find_obj` 回归；0 evaluator errors。第一段续接误启用 GitHub MCP，10 次 MCP 调用后中止并单独归档，不纳入离线续跑分数。按用户要求不再迭代；容器和专用镜像在归档核验后清理。详情：`archive/benchmarks/swebench-verified-mini/runs/batch-20261105-remaining-9-gate-909c58d/sphinx-doc__sphinx-8638-progress-checkpoint/continuation-52-result-summary.json`。；固定清单原状态说明：已续跑：原 128 轮后另有 20 轮续接因误启 GitHub MCP 作废；再以离线设置续跑 52 轮。source-only unresolved 0/1、F2P 0/1、P2P 33/34；按用户要求不再迭代，评分归档后清理任务容器/镜像 |
| 43 | `sphinx-doc__sphinx-8721` | MiniMax M3，79 轮（Agent 元数据 verification_status=test_failed） | source-only resolved | 1/1 | 4/4（F2P 1/1、P2P 3/3） | 79 | 约 1 分钟评分；Agent 运行时间见原始日志 | 2,019,023 输入 / 10,561 输出 tokens | Skills/Memory 开启；Sphinx Skill 在首轮、源码探索前加载，Memory 索引 6 条；联网、GitHub MCP、其他 MCP、workflow extensions、容器网络关闭。官方 gold control 1/1。Agent 原始 patch 与官方 test patch 同文件重叠，原始 patch 保留未评分；source-only patch 通过官方 evaluator，0 errors。归档：[batch-20261106-sphinx-8721-minimax/sphinx-doc__sphinx-8721](experiments/README.md#archive-access)。
| 44 | `sphinx-doc__sphinx-9229` | MiniMax M3，到 128 轮上限，未完成 | 未评分（candidate patch 只有 scratch 内容） | — | — | 128 / 131 工具调用 | 约 13 分钟；详见运行日志 | 3,395,514 输入 / 24,280 输出 tokens | Skills/Memory 开启；Sphinx Skill 首轮、源码探索前加载，Memory 索引 6 条；联网与 MCP、workflow extensions、容器网络关闭。Gold control 通过 1/1（F2P 1/1、P2P 13/13）。Agent 未修改 tracked source；原始 patch 仅含 `.scratch` 复现文件与生成 doctree，无目标测试结果，因此未启动 candidate evaluator、不记作官方 unresolved。按不收敛规则未续跑；任务容器和专用镜像保留。归档：[batch-20261106-sphinx-9229-minimax/sphinx-doc__sphinx-9229](experiments/README.md#archive-access)。
| 45 | `sphinx-doc__sphinx-9230` | MiniMax M3；原 124 轮中断后同 session 续 44 轮（累计 168） | raw resolved | 1/1 | 45/45（F2P 1/1、P2P 44/44） | 168 / 164 工具调用 | Agent 活跃时间约 28 分钟，分段见日志 | 4,338,020 输入 / 37,092 输出 tokens | Skills/Memory 开启，Sphinx Skill 在两段起始后均加载，Memory 索引 6 条；联网、GitHub MCP、其他 MCP、workflow extensions、容器网络关闭。gold control 1/1。原始 patch 与官方 test patch 顺序应用通过；Agent 本地 domain 相关 185 项通过。raw candidate 官方 evaluator resolved 1/1，F2P 1/1、P2P 44/44，0 errors。归档：[batch-20261106-sphinx-9230-minimax/sphinx-doc__sphinx-9230](experiments/README.md#archive-access)。
| 46 | `sphinx-doc__sphinx-9281` | MiniMax M3，124 轮（Agent verification_status=test_failed） | source-only resolved | 1/1 | 39/39（F2P 1/1、P2P 38/38） | 124 / 92 | Agent 约 18 分钟；官方评分约 27 秒 | 3,159,234 输入 / 38,423 输出 tokens | Skills/Memory 开启；Sphinx Skill 与 Python issue Skill 均加载，Memory 索引 6 条；联网、GitHub MCP、其他 MCP、workflow extensions、容器网络关闭。Gold control 1/1。Raw patch 与官方 test patch 在 `tests/test_util_inspect.py` 冲突，source-only patch 官方评分 resolved 1/1、F2P 1/1、P2P 38/38、0 errors。目标/相邻 112 项通过；5 模块 683 项通过。更广套件 1660 passed、6 failed，六项在 clean base 复现；linkcheck 因离线 DNS 排除。归档：[batch-20261106-sphinx-9281-minimax/sphinx-doc__sphinx-9281](experiments/README.md#archive-access)。详情和环境问题见该批 `evaluation-issues.md`。 |
| 47 | `sphinx-doc__sphinx-9320` | MiniMax M3，71 轮（Agent completed） | raw resolved | 1/1 | 10/10（F2P 1/1、P2P 9/9） | 71 / 82 | Agent 约 7 分钟；gold 与候选评分约 51 秒 | 1,837,224 输入 / 13,290 输出 tokens | Skills/Memory 开启；Sphinx Skill 与 Python issue Skill 均加载，Memory 索引 6 条；web search、GitHub MCP、其他 MCP、workflow extensions、容器网络关闭。Gold control 1/1。Raw patch 与官方 test patch 顺序应用通过，官方评分 resolved 1/1、F2P 1/1、P2P 9/9、0 errors。`tests/test_quickstart.py tests/test_build.py` 21 项通过；`conf.py` 与 `source/conf.py` 两种端到端场景均按预期退出。flake8 未安装，行长手动检查。归档：[batch-20261106-sphinx-9320-minimax/sphinx-doc__sphinx-9320](experiments/README.md#archive-access)。操作记录见该批 `evaluation-issues.md`。 |
| 48 | `sphinx-doc__sphinx-9367` | MiniMax M3，29 轮（Agent completed） | source-only resolved | 1/1 | 26/26（F2P 1/1、P2P 25/25） | 29 / 34 | Agent 约 3 分钟；gold 与候选评分约 25 秒 | 596,622 输入 / 4,700 输出 tokens | Skills/Memory 开启；Sphinx Skill 已加载，Memory 索引 11 条；web search、GitHub MCP、其他 MCP、workflow extensions、容器网络关闭。Gold control 1/1。Raw patch 与官方 test patch 在 `tests/test_pycode_ast.py` 冲突，raw 保留不评分；source-only 官方 resolved 1/1、F2P 1/1、P2P 25/25，0 evaluator errors。Agent 改动单元素 tuple unparse 并记录 CHANGES。归档：[batch-20261106-sphinx-9367-minimax/sphinx-doc__sphinx-9367](experiments/README.md#archive-access)。操作记录见该批 `evaluation-issues.md`。
| 49 | `sphinx-doc__sphinx-9461` | MiniMax M3，128 轮 + 同 session 续跑 48 轮（中断，累计 176） | source-only unresolved | 0/1 | 59/62（F2P 0/3、P2P 59/59） | 176 / 187 | Agent 约 17 分钟；gold 与候选评分约 56 秒 | 4,559,230 输入 / 33,015 输出 tokens | Skills/Memory 开启；Sphinx/Python Skills 已 stage，Memory 索引 16 条；web search、GitHub MCP、其他 MCP、workflow extensions、容器网络关闭。Gold control resolved 1/1，F2P 3/3、P2P 59/59。Raw patch 与官方 test patch 冲突，source-only candidate resolved 0/1、F2P 0/3、P2P 59/59，0 evaluator errors。Agent run `incomplete`，resume `--max-turns` 按续跑段重置计数；误续至累计 176 后中断，未继续。归档：[batch-20261106-sphinx-9461-minimax/sphinx-doc__sphinx-9461](experiments/README.md#archive-access)。操作记录见该批 `evaluation-issues.md`。
| 50 | `sphinx-doc__sphinx-9698` | MiniMax M3，128 轮（runtime failed finalization） | source-only resolved | 1/1 | 46/46（F2P 1/1、P2P 45/45） | 128 / 62 | Agent 约 14 分钟；gold 与候选评分约 52 秒 | 3,064,371 输入 / 41,004 输出 tokens | Skills/Memory 开启，Sphinx/Python Skills 加载，Memory 索引 16 条；web search、GitHub MCP、其他 MCP、workflow extensions、容器网络关闭。Gold control resolved 1/1。Agent 最后编辑后漏掉 `git_diff`，runtime 标记 failed。Raw patch 与官方 test patch 在 `tests/test_domain_py.py` 冲突；source-only 官方 resolved 1/1，F2P 1/1、P2P 45/45，0 errors。归档：[batch-20261106-sphinx-9698-minimax/sphinx-doc__sphinx-9698](experiments/README.md#archive-access)。操作记录见该批 `evaluation-issues.md`。

## 汇报方式

- 首先报告单题试跑的环境是否有效、Agent 实际做了什么、patch 是否通过官方评分，以及失败在哪个阶段。
- 50 题结束后报告 `resolved / infra-invalid / unresolved` 数量与比例，并附每题状态和可复核产物。无效环境题不算 Agent 失败，也不静默剔除；需列明并补跑或报告有效分母。
- 不把 mini 子集结果称为 SWE-bench Verified 官方榜单成绩，不与不同模型、脚手架、预算下的公开成绩作直接等价比较。

## 参考

- [SWE-bench Verified Mini 数据卡](https://huggingface.co/datasets/MariusHobbhahn/swe-bench-verified-mini)
- [Mini 数据集选择方法与仓库](https://github.com/mariushobbhahn/SWEBench-verified-mini)
- [SWE-bench Quickstart 与 Docker 官方评测说明](https://www.swebench.com/SWE-bench/guides/quickstart/)



## 2026-11-04 DeepSeek Flash re-runs of the first four tasks

These are a separate iteration batch using the same shared Django memory snapshot, Python issue Skill enabled, memory auto-write off, DeepSeek Flash, and offline issue-solving settings. They do not overwrite the earlier pilot/full-batch results above. Agent inputs contained only each task's public `problem_statement`; benchmark patches and hidden test patches were kept for the evaluator.

| Task | Agent result | Official F2P | P2P | Skill | Memory search | Outcome |
|---|---|---:|---:|---|---:|---|
| #12276 | Completed, 43 turns | 2/2 | 117/117 | Loaded | 0 | Resolved 1/1 |
| #12273 | Initial session hit 128-turn cap; session resume failed on malformed tool history; fresh continuation completed in 58 turns | 0/2 | 8/27 | Enabled | 3 in initial run | Unresolved 0/1; broad inheritance regressions remained |
| #11848 | Completed, 36 turns | 0/2 | 43/43 | Loaded | 0 | Unresolved 0/1; used real system clock instead of Django's mocked `datetime.datetime.utcnow()` seam |
| #11790 | Completed, 40 turns | 2/2 | 77/77 | Loaded | 0 | Resolved 1/1; integer `max_length` preserved in field/widget data and rendered into HTML |

Aggregate: **2/4 resolved**, 4/8 target tests passed, and two unresolved candidates. #11848 is evidence that merely loading the Skill does not ensure the Agent follows it; #11790's patch followed the type-preservation guidance and passed, but this one run cannot establish causality. In #11790 memory was enabled but never queried, so its success is not evidence for memory retrieval.

Per-task logs, candidate patches, evaluator inputs/reports, configuration, and cleanup records are in `archive/benchmarks/swebench-verified-mini/runs/batch-20261104-agent-iteration-reruns/`. Task-specific Docker containers and images were removed after each evaluation archive was verified. External source checkouts remain where recursive cleanup was blocked by desktop policy.

After these runs, the user-level Python issue Skill was tightened: time-dependent code must use the exact clock seam replaced by existing tests, and the Django rendering example explicitly says to keep Field.max_length typed as int until HTML serialization. That edit occurred after this batch, so it does not change the recorded run conditions or scores above.

## Follow-up iteration decisions

- `django__django-12262`: the always-loaded coding prompt was shortened, and Python-specific argument-validation guidance now lives in the on-demand `python-issue-workflow` Skill. The Skill asks the Agent to cover overlapping invalid-input categories and preserve specific duplicate-binding errors. Treat any rerun as a separate batch and retain the original unresolved score.
- `sphinx-doc__sphinx-8548`: retested once on 2026-11-07 after a Sphinx Skill refinement; the attempt was stopped incomplete and unscored at the user's request. The original official score remains unchanged; see the interrupted-run archive.
