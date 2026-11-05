# SWE-bench Verified Mini（50 题）Sigma Agent 评测计划

更新日期：2026-11-04

## 目标与边界

用固定的 SWE-bench Verified Mini 50 题子集评估 Sigma Agent 的端到端 issue 修复能力。先进行单题试跑，确认模型、仓库准备、Docker 测试和官方评分链路；通过后再逐题顺序执行。后续评测先统一使用 DeepSeek `deepseek-flash`，固定 Sigma 运行配置、Skills/记忆开关、提示词和每题预算；与此前 MiniMax M3/M3.1 结果分开记录和比较。旧批次按实际使用的模型记录，不回写历史结果。

Docker 镜像准备、网络故障、运行中断和清理一律遵循 [benchmark protocol](benchmark-protocol.md)。

`full-sigma-5` 的 #11815、#11848 和 #11880 首轮使用的是各目标 checkout
自己的 `.sigma-agent/MEMORY.md`，未加载仓库外的 `AGENTS.md` profile。现在增加了
`configs/full-sigma-5-django-11880-shared-profile.json`：它会把 Sigma 的
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

1. **固定题目与输入。** 从固定 revision `b316c349947c29963fce3f4a65967c9807a4b673` 的 Mini 数据集读取 `django__django-11790`，核对仓库、版本和 base commit。将题目元数据、原始 `problem_statement` 和数据集清单保存在 `var/benchmark-data/swebench-verified-mini/`；本题输入快照为 `task-01-issue-only.json`。Agent 不接收 gold patch、test patch、hints 或 PR 信息。
2. **准备候选工作区。** 在 `var/benchmark-data/swebench-verified-mini/task-01-seed/` 准备 Django checkout，并确认起始 commit 为 `b1d6b35e146aea83b171c1b921178bbaae2795ed`。运行前保存 Sigma 源码清单及哈希；当时 Sigma 位于 `780238173e1b98101c75f30d6e73c6179077f2f8`，工作区有未提交改动，因此把源码清单哈希 `5EFCDE8CD2BB9B8CBF28E5E5F6D5713F3313E70337E95FEF5F002DBFEB883876` 和配置哈希 `9766C05DA608BBDF68A3726624B1B808B0A8C3872FAB2A9D9735B6345B83D38B` 与结果一起记录。
3. **准备并检查 Docker 环境。** 使用该题官方任务镜像 `swebench/sweb.eval.x86_64.django_1776_django-11790:latest`，构建薄包装命令镜像；其 Dockerfile 删除镜像内的 `/testbed`，并把 Sigma 挂载的 `/workspace` 链接为 `/testbed`。核对 Python 和任务依赖，并确认 Agent 命令实际读写的是候选 checkout。首次 gold 评测因 Windows 生成的 SWE-bench 4.1.0 脚本含 CRLF 未能启动；在隔离 venv 中将脚本写入改为 LF 后重跑，之后才进入有效评测。
4. **运行 Sigma。** 使用当时保存的 pilot 配置和 MiniMax-M3；开启 Skills 和 Memory，关闭 web search、GitHub MCP，容器网络关闭。只传入上述 issue 正文，允许 Agent 在候选工作区检查、修改和运行项目命令。记录开关状态，不据此推断 Agent 一定实际读取了 Skill 或检索了 Memory。保留完整 transcript、trace、stdout/stderr、运行元数据和候选 patch；本次运行 ID 为 `343a5689-9f97-41c2-b630-2371177360d2`。
5. **冻结候选并准备评分输入。** 保存 Agent 原始 patch，不覆盖原件；另生成移除 Agent 自增测试改动的 source-only 诊断 patch。分别记录 patch 应用检查结果，确保评分使用的候选源码来自该次 Sigma checkout。
6. **先做 gold 正控，再评候选。** 在同一 SWE-bench evaluator、任务镜像和 base commit 上先运行官方 gold patch，确认环境与目标测试可用；再分别评原始 patch 和 source-only patch。保存每次 predictions、aggregate report、实例级 report、容器日志及命令输出，按 F2P、P2P 和 resolved 状态判读。
7. **归档后再处理环境。** 将 issue 输入、运行配置、源码/配置哈希、原始及诊断 patch、gold 与候选评分产物、运行日志放入 `var/benchmark-data/swebench-verified-mini/`，Sigma session 记录保存在 `var/runs/343a5689-9f97-41c2-b630-2371177360d2/`。只有 Agent 运行、官方评分和归档均完成后，才按协议判断任务专属镜像、容器与缓存是否可清理；中断时标记 `invalid/incomplete` 并保留环境。

## 50 题固定清单

| # | instance_id | 仓库 / 版本 | base commit | 状态 |
|---:|---|---|---|---|
| 1 | `django__django-11790` | `django/django` / 3.1 | `b1d6b35e146aea83b171c1b921178bbaae2795ed` | Pilot：gold 1/1、Sigma unresolved；2026-11-04 迭代续跑：resolved（DeepSeek Flash，F2P 2/2、P2P 77/77） |
| 2 | `django__django-11815` | `django/django` / 3.1 | `e02f67ef2d03d48128e7a118bf75f0418e24e8ac` | 已评分：resolved |
| 3 | `django__django-11848` | `django/django` / 3.1 | `f0adf3b9b7a19cdee05368ff0c0c2d087f011180` | 原批次 unresolved；2026-11-04 迭代续跑仍 unresolved（DeepSeek Flash，F2P 0/2、P2P 43/43） |
| 4 | `django__django-11880` | `django/django` / 3.1 | `06909fe084f87a65459a83bd69d7cdbe4fce9a7c` | 已评分：code-only resolved 1/1；raw patch 有 memory sidecar 冲突 |
| 5 | `django__django-11885` | `django/django` / 3.1 | `04ac9b45a34440fa447feb6ae934687aacbfc5f4` | M3 已尝试；128 轮无补丁，未评分 |
| 6 | `django__django-11951` | `django/django` / 3.1 | `312049091288dbba2299de8d07ea3e3311ed7238` | 已评分：source-only resolved 1/1；raw patch 与官方 test_patch import 冲突 |
| 7 | `django__django-11964` | `django/django` / 3.1 | `fc2b1cc926e34041953738e58fa6ad3053059b22` | 官方评分 resolved 1/1（边界门禁后同 session 续跑；继承首轮越界读取上下文，不算 pristine issue-only） |
| 8 | `django__django-11999` | `django/django` / 3.1 | `84633905273fc916e3d17883810d9969c03f73c2` | 已评分：source-only resolved 1/1；raw patch 与官方 test patch 路径冲突 |
| 9 | `django__django-12039` | `django/django` / 3.1 | `58c1acb1d6054dfec29d0f30b1033bae6ef62aec` | 已完成：历史 M3.1 阶段因 Token Plan 中断，同一 session 改用 M3 续跑；source-only 与 raw 均 resolved 1/1 |
| 10 | `django__django-12050` | `django/django` / 3.1 | `b93a0e34d9b9b99d41103782b7e7aeabf47517e3` | 已评分：DeepSeek Flash，source-only resolved 1/1 |
| 11 | `django__django-12143` | `django/django` / 3.1 | `5573a54d409bb98b5c5acdb308310bed02d392c2` | DeepSeek Flash：官方 resolved 1/1 |
| 12 | `django__django-12155` | `django/django` / 3.1 | `e8fcdaad5c428878d0a5d6ba820d957013f75595` | DeepSeek Flash：官方 resolved 1/1 |
| 13 | `django__django-12193` | `django/django` / 3.1 | `3fb7c12158a2402f0f80824f6778112071235803` | DeepSeek Flash：官方 resolved 1/1 |
| 14 | `django__django-12209` | `django/django` / 3.1 | `5a68f024987e6d16c2626a31bf653a2edddea579` | DeepSeek Flash：官方 resolved 1/1；neutral control 0/1 |
| 15 | `django__django-12262` | `django/django` / 3.1 | `69331bb851c34f05bc77e9fc24020fe6908b9cd5` | 原批次 DeepSeek Flash：unresolved，F2P 3/4；针对参数重复绑定与未知参数错误优先级迭代中，待独立重测 |
| 16 | `django__django-12273` | `django/django` / 3.1 | `927c903f3cd25c817c21738328b53991c035b415` | 原批次 unresolved（候选截断源码）；2026-11-04 迭代续跑仍 unresolved（F2P 0/2、P2P 8/27） |
| 17 | `django__django-12276` | `django/django` / 3.1 | `53d8646f799de7f92ab9defe9dc56c6125448102` | 原批次 unresolved（F2P 2/2、P2P 98/117）；2026-11-04 迭代续跑 resolved（F2P 2/2、P2P 117/117） |
| 18 | `django__django-12304` | `django/django` / 3.1 | `4c1b401e8250f9f520b3c7dc369554477ce8b15a` | 待测 |
| 19 | `django__django-12308` | `django/django` / 3.1 | `2e0f04507b17362239ba49830d26fec504d46978` | 待测 |
| 20 | `django__django-12325` | `django/django` / 3.1 | `29c126bb349526b5f1cd78facbe9f25906f18563` | 待测 |
| 21 | `django__django-12406` | `django/django` / 3.1 | `335c9c94acf263901fb023404408880245b0c4b4` | 待测 |
| 22 | `django__django-12708` | `django/django` / 3.1 | `447980e72ac01da1594dd3373a03ba40b7ee6f80` | 待测 |
| 23 | `django__django-12713` | `django/django` / 3.1 | `5b884d45ac5b76234eca614d90c83b347294c332` | 待测 |
| 24 | `django__django-12774` | `django/django` / 3.1 | `67f9d076cfc1858b94f9ed6d1a5ce2327dcc8d0d` | 待测 |
| 25 | `django__django-9296` | `django/django` / 3.1 | `84322a29ce9b0940335f8ab3d60e55192bef1e50` | 待测 |
| 26 | `sphinx-doc__sphinx-10323` | `sphinx-doc/sphinx` / 5.0 | `31eba1a76dd485dc633cae48227b46879eda5df4` | 待测 |
| 27 | `sphinx-doc__sphinx-10435` | `sphinx-doc/sphinx` / 5.0 | `f1061c012e214f16fd8790dec3c283d787e3daa8` | 待测 |
| 28 | `sphinx-doc__sphinx-10466` | `sphinx-doc/sphinx` / 5.0 | `cab2d93076d0cca7c53fac885f927dde3e2a5fec` | 待测 |
| 29 | `sphinx-doc__sphinx-10673` | `sphinx-doc/sphinx` / 5.2 | `f35d2a6cc726f97d0e859ca7a0e1729f7da8a6c8` | 待测 |
| 30 | `sphinx-doc__sphinx-11510` | `sphinx-doc/sphinx` / 7.2 | `6cb783c0024a873722952a67ebb9f41771c8eb6d` | 待测 |
| 31 | `sphinx-doc__sphinx-7590` | `sphinx-doc/sphinx` / 3.1 | `2e506c5ab457cba743bb47eb5b8c8eb9dd51d23d` | 待测 |
| 32 | `sphinx-doc__sphinx-7748` | `sphinx-doc/sphinx` / 3.1 | `9988d5ce267bf0df4791770b469431b1fb00dcdd` | 待测 |
| 33 | `sphinx-doc__sphinx-7757` | `sphinx-doc/sphinx` / 3.1 | `212fd67b9f0b4fae6a7c3501fdf1a9a5b2801329` | 待测 |
| 34 | `sphinx-doc__sphinx-7985` | `sphinx-doc/sphinx` / 3.2 | `f30284ef926ebaf04b176f21b421e2dffc679792` | 待测 |
| 35 | `sphinx-doc__sphinx-8035` | `sphinx-doc/sphinx` / 3.2 | `5e6da19f0e44a0ae83944fb6ce18f18f781e1a6e` | 待测 |
| 36 | `sphinx-doc__sphinx-8056` | `sphinx-doc/sphinx` / 3.2 | `e188d56ed1248dead58f3f8018c0e9a3f99193f7` | 待测 |
| 37 | `sphinx-doc__sphinx-8265` | `sphinx-doc/sphinx` / 3.3 | `b428cd2404675475a5c3dc2a2b0790ba57676202` | 待测 |
| 38 | `sphinx-doc__sphinx-8269` | `sphinx-doc/sphinx` / 3.3 | `1e2ccd8f0eca0870cf6f8fce6934e2da8eba9b72` | 待测 |
| 39 | `sphinx-doc__sphinx-8475` | `sphinx-doc/sphinx` / 3.4 | `3ea1ec84cc610f7a9f4f6b354e264565254923ff` | 待测 |
| 40 | `sphinx-doc__sphinx-8548` | `sphinx-doc/sphinx` / 3.4 | `dd1615c59dc6fff633e27dbb3861f2d27e1fb976` | 已评测：M3 到 128 轮上限；source-only unresolved 0/1，target 通过但 P2P 1 项回归；按用户决定不再迭代或重测 |
| 41 | `sphinx-doc__sphinx-8551` | `sphinx-doc/sphinx` / 3.4 | `57ed10c68057c96491acbd3e62254ccfaf9e3861` | 已评测：Agent 128 轮未完成最终 diff；source-only 官方评分 resolved 1/1，F2P 1/1、P2P 32/32 |
| 42 | `sphinx-doc__sphinx-8638` | `sphinx-doc/sphinx` / 4.0 | `4b452338f914d4f6b54704222d70ae8a746e3db5` | 待测 |
| 43 | `sphinx-doc__sphinx-8721` | `sphinx-doc/sphinx` / 3.5 | `82ef497a8c88f0f6e50d84520e7276bfbf65025d` | 待测 |
| 44 | `sphinx-doc__sphinx-9229` | `sphinx-doc/sphinx` / 4.1 | `876fa81e0a038cda466925b85ccf6c5452e0f685` | 待测 |
| 45 | `sphinx-doc__sphinx-9230` | `sphinx-doc/sphinx` / 4.1 | `567ff22716ac258b9edd2c1711d766b440ac0b11` | 待测 |
| 46 | `sphinx-doc__sphinx-9281` | `sphinx-doc/sphinx` / 4.1 | `8ec06e9a1bd862cd713b9db748e039ccc7b3e15b` | 待测 |
| 47 | `sphinx-doc__sphinx-9320` | `sphinx-doc/sphinx` / 4.1 | `e05cef574b8f23ab1b57f57e7da6dee509a4e230` | 待测 |
| 48 | `sphinx-doc__sphinx-9367` | `sphinx-doc/sphinx` / 4.1 | `6918e69600810a4664e53653d6ff0290c3c4a788` | 待测 |
| 49 | `sphinx-doc__sphinx-9461` | `sphinx-doc/sphinx` / 4.2 | `939c7bb7ff7c53a4d27df067cea637540f0e1dad` | 待测 |
| 50 | `sphinx-doc__sphinx-9698` | `sphinx-doc/sphinx` / 4.3 | `f050a7775dfc9000f55d023d36d925a8d02ccfa8` | 待测 |

表中对 #11790、#11848、#12273、#12276 分别列出原批次与 2026-11-04 迭代续跑结果。续跑属于独立实验批次，不覆盖原结果，也不与原 50 题批次合并计算总分。

## 结果汇总表

| # | instance_id | Sigma 执行状态 | 官方评分 | F2P 通过 / 总数 | P2P 回归 | 轮数 / 工具数 | 耗时 | Token / 费用 | 备注 |
|---:|---|---|---|---:|---:|---:|---:|---:|---|
| 1 | `django__django-11790` | 完成 | unresolved | 0/2 | 77/77 通过 | 44 / 58 | 约 4.5 分钟 | 172,572 / 11,786 tokens | gold 1/1；raw 与 source-only 均未通过 F2P，属性值类型不符 |
| 2 | `django__django-11815` | 完成 | resolved | 2/2 | 44/44 通过 | 72 / 83 | 约 11 分钟（按记录时间估算） | 1,634,776 / 19,227 tokens | code-only candidate 1/1；raw patch 因 `.sigma-agent/MEMORY.md` sidecar 冲突未评分；运行配置启用 Skills/Memory，但 trace 没有 `read_skill` / `search_memory` 调用。 |
| 3 | `django__django-11848` | 完成（额度恢复后续跑） | unresolved | 0/2 | 43/43 通过 | 135 / 142（3 次运行） | 约 13 分钟（不含等待额度） | 3,571,625 / 54,527 tokens（累计） | 官方测试中两个 F2P 因 `datetime.datetime` mock 接口不匹配而报错；第一次 evaluator 调用的 CRLF 补丁格式错误，修正为 LF 后评分。续跑前修复了 Skill 误匹配。 |
| 4 | `django__django-11880` | 完成（保存的 issue-only session 续跑） | code-only resolved | 1/1 | 114/114 通过 | 15 / 13（续跑） | 约 1 分钟续跑 | 366,119 输入 / 7,337 输出 tokens | gold control 1/1；原始补丁因 session 生成的 `.sigma-agent/MEMORY.md` 与 evaluator 现存文件冲突，code-only 评分补丁移除此单个 sidecar 后通过。Agent 本地组测 816（9 skip）及 319（22 skip）通过。完整日志：`C:\dev\Agent\swb5\outputs\archives\2026-11-04-django__django-11880-resumed-m31`。 |
| 5 | `django__django-11885` | M3 续跑到 128 轮；无目标源码改动 | 未评分（无候选补丁） | — | — | 128 / 130 | 19 分 26 秒 | 3,204,270 输入 / 24,249 输出 tokens | 本地 `delete` 测试通过；没有官方评测。启用 `/workspace` 边界后未再访问 `/tmp`；反复读取包含路径核实和上下文压缩导致的重复。完整记录：`C:\dev\Agent\swb5\outputs\runs\django-11885-20261104\attempts\m3-continued-after-recheck`。 |
| 6 | `django__django-11951` | 完成（429 后同 session 续跑） | source-only resolved | 1/1 | 21/21 通过 | 61 / 71 | 约 4 分钟累计模型运行 | 1,328,297 输入 / 34,723 输出 tokens | 本地 bulk_create 组测 27 通过、5 跳过；raw patch 与官方 test_patch 在 imports 处冲突，保留 raw 并对只含源码的独立 patch 评分。归档：`C:\dev\Agent\swb5\outputs\archives\2026-11-04-django__django-11951`。
| 7 | `django__django-11964` | 完成（首轮越界后，同 session 加命令路径门禁续跑） | raw resolved 1/1 | 2/2 | 15/15 通过 | 57 / 63（续跑） | 约 14 分钟续跑 | 1,473,611 输入 / 79,611 输出 tokens（续跑） | 首轮仍记 `invalid/incomplete`；续跑继承其越界读取上下文，因此不是 pristine issue-only 结果。`command_filesystem_scope=workspace` 在 Docker 启动前拦截显式越界路径；本地目标组 120 测试、1 跳过通过；官方评测无错误。归档：`C:\dev\Agent\swb5\outputs\archives\2026-11-04-django__django-11964-boundary-guard-rerun`。|
| 8 | `django__django-11999` | 完成（MiniMax 流错误后同 session 续跑） | source-only resolved | 1/1 | 30/30 通过 | 125 / 144（两次运行合计） | 约 38 分钟 | 3,128,686 输入 / 209,711 输出 tokens | Agent 本地 `model_fields` 313 项通过、2 跳过；raw patch 因 Agent 测试文件与官方 test patch 同路径而未评分，source-only patch 官方评分无错误。归档：`C:\dev\Agent\swb5\outputs\archives\2026-11-04-django__django-11999`。|
| 9 | `django__django-12039` | 完成（Token Plan 恢复后同 session 续跑；M3.1 中断后切换 M3） | source-only 与 raw 均 resolved | 1/1 | 12/12（F2P 1/1、P2P 11/11） | 185 / 216（含 1 次额度失败重试；M3 续跑 96 / 95） | M3 续跑约 6 分 19 秒；评分约 34 / 28 秒 | 4,734,474 输入 / 303,264 输出 tokens（混合模型累计） | M3 续跑本地定向测试 51 通过、1 跳过；官方 source-only 与 raw 补丁均通过，0 evaluator errors。该结果包含 M3.1 首段，不能记为纯 M3 从头运行。归档：`C:\dev\Agent\swb5\outputs\archives\2026-11-04-django__django-12039`。|
| 10 | `django__django-12050` | 完成（中断后续原 session） | resolved | 1/1 | 10/10 通过 | 88 / 86（累计） | Agent 运行约数分钟，官方测试约 30 秒 | 2,095,876 输入 / 22,456 输出 tokens | DeepSeek Flash；Skills/Memory 配置开启，联网、MCP 关闭。74 个本地 focused tests 通过；官方 source-only patch resolved。审计：0 次 read_skill；1 次 memory 查询只返回测试环境信息。通用系统提示含“保留领域类型”原则，属于相关但非解法的引导。首次连接中断后，第一次续跑被 Windows NTFS symlink 的 Git 预检阻塞（56 次调用）；修复后继续同 session 完成。报告与日志：`var/benchmark-data/swebench-verified-mini/runs/django-12050-20261104/official-eval`。|
| 11 | `django__django-12143` | 完成 | resolved | 1/1 | 52/52 通过 | 19 / 25 | 约 1 分钟（含评分约 30 秒） | 266,136 输入 / 3,595 输出 tokens | issue-only 正文（官方 issue 自带 `re.escape()` 建议）；Skills/Memory 开启但 trace 无读取调用；本地定向 3/3 通过；官方测试补丁先还原测试文件后应用成功。报告：`var/benchmark-data/swebench-verified-mini/runs/django-12143-20261104/official-eval`。|
| 12 | `django__django-12155` | 完成 | resolved | 1/1 | 6/6 通过 | 24 / 27 | 约 1 分钟 Agent，评分 29 秒 | 408,472 输入 / 4,075 输出 tokens | issue-only 正文（官方 issue 自带 `lines[1:]` 建议）；Skills/Memory 开启但无读取调用；本地 `admin_docs` 61/61 通过；官方测试补丁应用成功。报告：`var/benchmark-data/swebench-verified-mini/runs/django-12155-20261104/official-eval`。|
| 13 | `django__django-12193` | 完成 | resolved | 1/1 | 11/11 通过 | 15 / 17 | Agent 约 1 分钟，评分 28 秒 | 172,585 输入 / 2,610 输出 tokens | issue-only 正文直接解释了 `CheckboxInput.get_context()` 修改传入 attrs；Sigma 没有收到额外 diff 或提示。Skills/Memory 开启但 trace 无 skill/memory 检索；完成后自动写入 Django 共享记忆 2 条。Agent 报告本地 137 项通过（含 110 跳过）；官方共运行 122 项（110 跳过），目标 F2P 通过。评测日志记录首次候选补丁应用失败，官方流程重置测试文件后成功应用；最终 report 为 `patch_successfully_applied=true`。无单独 gold control。详见 `var/benchmark-data/swebench-verified-mini/runs/django-12193-20261104/official-eval`。|
| 14 | `django__django-12209` | 完成 | resolved | 1/1 | 0/0 | 31 / 37 | Agent 约 2 分钟，评分 30 秒；neutral control 29 秒 | 593,728 输入 / 6,254 输出 tokens | issue-only；Skills/Memory 开启，1 次 memory 查询、0 次 Skill 读取；结束后自动写入记忆 1 条。官方测试 4/4 通过；no-op 注释补丁 0/1（4 个 serializer 测试因重复 UUID 主键报错），证明评分依赖行为变化。F2P 标签是测试输出中的 `functools.partial` 说明而非常规测试名。Sigma 自评未完成直接 `save()` 场景并发现一个多余 helper；其 `not raw` 核心改动与官方参考补丁一致，故按官方 fixture/serializer 验收记 resolved，同时注明覆盖范围限制。空补丁控制未运行（evaluator 跳过空补丁）。详见 `var/benchmark-data/swebench-verified-mini/runs/django-12209-20261104/official-eval`。|
| 15 | `django__django-12262` | 完成 | unresolved | 3/4 | 18/18 通过 | 70 / 75 | Agent 约 3 分钟，评分 28 秒 | 1,603,537 输入 / 16,280 输出 tokens | issue-only；Skills/Memory 开启但 trace 无读取调用，结束后自动写入记忆 1 条。Agent 报告本地整套 1,387 tests 通过；官方测试仍有 `test_simple_tag_errors` 失败：重复 keyword-only 参数预期报 duplicate-value，却报 unexpected-keyword。属于 Agent 行为/验证缺口，环境正常。48 次压缩、约 160 万输入 token。详见 `var/benchmark-data/swebench-verified-mini/runs/django-12262-20261104/official-eval`。|
| 16 | `django__django-12273` | 完成 | unresolved | 0/2（模块语法错误，未执行） | 0/26（模块语法错误，未执行） | 45 / 50 | Agent 约 3 分钟，评分 28 秒 | 837,300 输入 / 10,358 输出 tokens | issue-only；Agent 尝试修改 `_get_pk_val()` 时把 `django/db/models/base.py` 截断并写入 `RuntimeError("truncated")`；官方 patch 可应用，但模块 `SyntaxError: unexpected EOF`，F2P/P2P 均未执行。环境正常，属于 Agent 编辑事故。详见 `var/benchmark-data/swebench-verified-mini/runs/django-12273-20261104/official-eval`。|
| 17 | `django__django-12276` | 达到 128 轮上限，未完成收尾 | unresolved | 2/2 | 98/117 通过（19 回归） | 128 / 126 | Agent 约 10 分钟，评分 29 秒 | 2,746,844 输入 / 52,879 输出 tokens | issue-only；Agent 截断并手工重建 `django/forms/fields.py` 后导致大量表单回归；官方目标测试通过，但 P2P 19 项失败。最终 run 未完成（要求在最后修改后补 `git_diff`）；没有 Codex 修补。详见 `var/benchmark-data/swebench-verified-mini/runs/django-12276-20261104/official-eval`。|
| 13 | `django__django-12193` | 未运行 | 未评分 | — | — | — | — | — | — |
| 14 | `django__django-12209` | 未运行 | 未评分 | — | — | — | — | — | — |
| 15 | `django__django-12262` | 未运行 | 未评分 | — | — | — | — | — | — |
| 16 | `django__django-12273` | 未运行 | 未评分 | — | — | — | — | — | — |
| 17 | `django__django-12276` | 未运行 | 未评分 | — | — | — | — | — | — |
| 18 | `django__django-12304` | 未运行 | 未评分 | — | — | — | — | — | — |
| 19 | `django__django-12308` | 未运行 | 未评分 | — | — | — | — | — | — |
| 20 | `django__django-12325` | 未运行 | 未评分 | — | — | — | — | — | — |
| 21 | `django__django-12406` | 未运行 | 未评分 | — | — | — | — | — | — |
| 22 | `django__django-12708` | 未运行 | 未评分 | — | — | — | — | — | — |
| 23 | `django__django-12713` | 未运行 | 未评分 | — | — | — | — | — | — |
| 24 | `django__django-12774` | 未运行 | 未评分 | — | — | — | — | — | — |
| 25 | `django__django-9296` | 未运行 | 未评分 | — | — | — | — | — | — |
| 26 | `sphinx-doc__sphinx-10323` | 未运行 | 未评分 | — | — | — | — | — | — |
| 27 | `sphinx-doc__sphinx-10435` | 未运行 | 未评分 | — | — | — | — | — | — |
| 28 | `sphinx-doc__sphinx-10466` | 未运行 | 未评分 | — | — | — | — | — | — |
| 29 | `sphinx-doc__sphinx-10673` | 未运行 | 未评分 | — | — | — | — | — | — |
| 30 | `sphinx-doc__sphinx-11510` | 未运行 | 未评分 | — | — | — | — | — | — |
| 31 | `sphinx-doc__sphinx-7590` | 未运行 | 未评分 | — | — | — | — | — | — |
| 32 | `sphinx-doc__sphinx-7748` | 未运行 | 未评分 | — | — | — | — | — | — |
| 33 | `sphinx-doc__sphinx-7757` | 未运行 | 未评分 | — | — | — | — | — | — |
| 34 | `sphinx-doc__sphinx-7985` | 未运行 | 未评分 | — | — | — | — | — | — |
| 35 | `sphinx-doc__sphinx-8035` | 未运行 | 未评分 | — | — | — | — | — | — |
| 36 | `sphinx-doc__sphinx-8056` | 未运行 | 未评分 | — | — | — | — | — | — |
| 37 | `sphinx-doc__sphinx-8265` | 未运行 | 未评分 | — | — | — | — | — | — |
| 38 | `sphinx-doc__sphinx-8269` | 未运行 | 未评分 | — | — | — | — | — | — |
| 39 | `sphinx-doc__sphinx-8475` | 未运行 | 未评分 | — | — | — | — | — | — |
| 40 | `sphinx-doc__sphinx-8548` | M3 运行未收敛（128 轮上限） | source-only unresolved | 0/1 | 5/6（F2P 1/1，P2P 4/5） | 128 / 141 | 约 7 分钟 | 3,113,431 输入 / 17,195 输出 tokens | 目标测试通过，但 `test_instance_variable` 因额外输出继承属性而回归。raw patch 因 `.sigma-agent` 内 Cython 二进制产物无法应用，未评分。验证门禁反复拦截测试调用，导致 Agent 继续探索直至轮数上限。归档：`C:\dev\Agent\swb5\outputs\archives\2026-11-04-sphinx-doc__sphinx-8548`。|
| 41 | `sphinx-doc__sphinx-8551` | M3 到 128 轮上限，最终 diff 门禁未完成 | source-only resolved | 1/1 | 33/33（F2P 1/1，P2P 32/32） | 128 / 123 | 约 13 分钟 | 3,107,707 输入 / 24,896 输出 tokens | Agent 运行状态为失败，但保留的生产补丁通过官方 evaluator；目标 `test_info_field_list` 和 32 项 P2P 全通过，0 evaluator errors。原始补丁保留未评分。运行问题：反复调用 runner 时单题 JSON 数据形状传错，两次都在创建 evaluator 容器前失败；改回原始单元素数组后评分成功。归档：`C:\dev\Agent\swb5\outputs\archives\2026-11-04-sphinx-doc__sphinx-8551`。|
| 42 | `sphinx-doc__sphinx-8638` | 未运行 | 未评分 | — | — | — | — | — | — |
| 43 | `sphinx-doc__sphinx-8721` | 未运行 | 未评分 | — | — | — | — | — | — |
| 44 | `sphinx-doc__sphinx-9229` | 未运行 | 未评分 | — | — | — | — | — | — |
| 45 | `sphinx-doc__sphinx-9230` | 未运行 | 未评分 | — | — | — | — | — | — |
| 46 | `sphinx-doc__sphinx-9281` | 未运行 | 未评分 | — | — | — | — | — | — |
| 47 | `sphinx-doc__sphinx-9320` | 未运行 | 未评分 | — | — | — | — | — | — |
| 48 | `sphinx-doc__sphinx-9367` | 未运行 | 未评分 | — | — | — | — | — | — |
| 49 | `sphinx-doc__sphinx-9461` | 未运行 | 未评分 | — | — | — | — | — | — |
| 50 | `sphinx-doc__sphinx-9698` | 未运行 | 未评分 | — | — | — | — | — | — |

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

Per-task logs, candidate patches, evaluator inputs/reports, configuration, and cleanup records are in `var/benchmark-data/swebench-verified-mini/runs/batch-20261104-agent-iteration-reruns/`. Task-specific Docker containers and images were removed after each evaluation archive was verified. External source checkouts remain where recursive cleanup was blocked by desktop policy.

After these runs, the user-level Python issue Skill was tightened: time-dependent code must use the exact clock seam replaced by existing tests, and the Django rendering example explicitly says to keep Field.max_length typed as int until HTML serialization. That edit occurred after this batch, so it does not change the recorded run conditions or scores above.

## Follow-up iteration decisions

- `django__django-12262`: the always-loaded coding prompt was shortened, and Python-specific argument-validation guidance now lives in the on-demand `python-issue-workflow` Skill. The Skill asks the Agent to cover overlapping invalid-input categories and preserve specific duplicate-binding errors. Treat any rerun as a separate batch and retain the original unresolved score.
- `sphinx-doc__sphinx-8548`: no further iteration or rerun, per user decision.
