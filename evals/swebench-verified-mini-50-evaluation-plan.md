# SWE-bench Verified Mini（50 题）Sigma Agent 评测计划

更新日期：2026-11-03

## 目标与边界

用固定的 SWE-bench Verified Mini 50 题子集评估 Sigma Agent 的端到端 issue 修复能力。先进行单题试跑，确认模型、仓库准备、Docker 测试和官方评分链路；通过后再逐题顺序执行。MiniMax M3、Sigma 运行配置、Skills/记忆开关、提示词和每题预算在正式批次中固定。

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
| 4. 清理与归档 | 一题完成后先归档结果，再移除该题专属容器/镜像；保留数据集、日志和可复用环境层 | 同时最多运行一个任务；每题结束核对 Docker 占用 |

## 单题试跑验收（第 1 题）

1. 数据集第一题：`django__django-11790`，仓库 `django/django`，Django 3.1，base commit `b1d6b35e146aea83b171c1b921178bbaae2795ed`。
2. 先验证任务镜像、容器内 Python/Django 版本、基线工作区和目标测试环境可用；Sigma 的 `run_command` 使用该任务的 Python 镜像，不使用 Go 镜像。
3. 给 Sigma 的唯一任务输入是数据集 `problem_statement`；无 Skill/记忆人工提示，不读目标 patch/test patch/hints。记录本题实际开关，作为后续 50 题统一配置依据。
4. 保存原始候选 patch；官方 evaluator 分别测候选 patch 和 gold patch。对比目标 fail-to-pass、原有 pass-to-pass，报告明确区分“测试通过”“官方 resolved”“环境无效”。
5. 本题完成后再决定是否调整 agent。任何调整后从新实验批次重新开始，不把调参后结果和原批次混合。

### 第 1 题试跑结果（2026-11-03）

- **环境有效。** 官方 gold patch 正控在原始任务镜像上 `1/1 resolved`，目标 fail-to-pass `2/2` 通过，pass-to-pass `77/77` 通过。Sigma 候选评分前后使用同一 SWE-bench evaluator、任务镜像和 base commit。
- **Sigma 未通过官方验收：`0/1 resolved`。** 原始 Agent patch 和剔除 Agent 自增测试后的 source-only patch 都是 `0/2` fail-to-pass、`77/77` pass-to-pass。patch 可应用，失败属于行为验收，不是镜像或依赖错误。
- Agent 修复了 `django/contrib/auth/forms.py`，并在 `tests/auth_tests/test_forms.py` 写了自己的测试。它的本地命令报告 82 项测试通过；官方评分的两个目标检查却发现 `widget.attrs['maxlength']` 是字符串（例如 `'254'`），预期值是整数 `254`。因此 HTML 序列化可能看起来正确，但原始 Widget 属性的类型契约没有满足官方验收；后续复盘应分开判断对用户可见的 HTML 行为与基准测试要求。
- Sigma 使用 MiniMax-M3，Skills/Memory、web search、GitHub MCP 均关闭；只给数据集 `problem_statement`，没有给 `patch`、`test_patch`、`hints_text` 或 PR diff。运行 `44` 轮、`58` 次工具调用，约 `172,572` 输入 token、`11,786` 输出 token。这个单题 token 消耗偏高，正式跑 50 题前要先决定是否调整上下文/工具策略；若调整，结果需另开批次。
- 此任务使用的数据集 revision 为 `b316c349947c29963fce3f4a65967c9807a4b673`。Sigma checkout commit 为 `780238173e1b98101c75f30d6e73c6179077f2f8`；当时工作区含未提交修改，非 `var`/密钥文件源码清单 SHA-256 为 `5EFCDE8CD2BB9B8CBF28E5E5F6D5713F3313E70337E95FEF5F002DBFEB883876`，pilot config SHA-256 为 `9766C05DA608BBDF68A3726624B1B808B0A8C3872FAB2A9D9735B6345B83D38B`。
- Windows 运行适配：SWE-bench 4.1.0 的评测脚本在 Windows 写出 CRLF，第一次 gold 正控因此无法启动；将本机隔离 venv 中的脚本写入改为 LF 后，gold 正控通过。Agent 命令容器通过薄包装镜像把 `/testbed` 映射到 Sigma 挂载的 `/workspace`，确认 Agent 测试确实使用候选源码。试跑结束后已删除该题的官方和包装镜像；后续任务需逐题拉取并清理。
- 产物均在被 Git 忽略的 `var/benchmark-data/swebench-verified-mini/` 与 `var/runs/343a5689-9f97-41c2-b630-2371177360d2/`，含 Agent 原始 patch、source-only 诊断 patch、官方 gold/candidate 报告及运行日志。试跑作为 50 题清单第 1 题记录，但仅为 pilot，不单独作为 Sigma 能力的结论。

## 50 题固定清单

| # | instance_id | 仓库 / 版本 | base commit | 状态 |
|---:|---|---|---|---|
| 1 | `django__django-11790` | `django/django` / 3.1 | `b1d6b35e146aea83b171c1b921178bbaae2795ed` | Pilot：gold 1/1；Sigma 0/1 |
| 2 | `django__django-11815` | `django/django` / 3.1 | `e02f67ef2d03d48128e7a118bf75f0418e24e8ac` | 待测 |
| 3 | `django__django-11848` | `django/django` / 3.1 | `f0adf3b9b7a19cdee05368ff0c0c2d087f011180` | 待测 |
| 4 | `django__django-11880` | `django/django` / 3.1 | `06909fe084f87a65459a83bd69d7cdbe4fce9a7c` | 待测 |
| 5 | `django__django-11885` | `django/django` / 3.1 | `04ac9b45a34440fa447feb6ae934687aacbfc5f4` | 待测 |
| 6 | `django__django-11951` | `django/django` / 3.1 | `312049091288dbba2299de8d07ea3e3311ed7238` | 待测 |
| 7 | `django__django-11964` | `django/django` / 3.1 | `fc2b1cc926e34041953738e58fa6ad3053059b22` | 待测 |
| 8 | `django__django-11999` | `django/django` / 3.1 | `84633905273fc916e3d17883810d9969c03f73c2` | 待测 |
| 9 | `django__django-12039` | `django/django` / 3.1 | `58c1acb1d6054dfec29d0f30b1033bae6ef62aec` | 待测 |
| 10 | `django__django-12050` | `django/django` / 3.1 | `b93a0e34d9b9b99d41103782b7e7aeabf47517e3` | 待测 |
| 11 | `django__django-12143` | `django/django` / 3.1 | `5573a54d409bb98b5c5acdb308310bed02d392c2` | 待测 |
| 12 | `django__django-12155` | `django/django` / 3.1 | `e8fcdaad5c428878d0a5d6ba820d957013f75595` | 待测 |
| 13 | `django__django-12193` | `django/django` / 3.1 | `3fb7c12158a2402f0f80824f6778112071235803` | 待测 |
| 14 | `django__django-12209` | `django/django` / 3.1 | `5a68f024987e6d16c2626a31bf653a2edddea579` | 待测 |
| 15 | `django__django-12262` | `django/django` / 3.1 | `69331bb851c34f05bc77e9fc24020fe6908b9cd5` | 待测 |
| 16 | `django__django-12273` | `django/django` / 3.1 | `927c903f3cd25c817c21738328b53991c035b415` | 待测 |
| 17 | `django__django-12276` | `django/django` / 3.1 | `53d8646f799de7f92ab9defe9dc56c6125448102` | 待测 |
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
| 40 | `sphinx-doc__sphinx-8548` | `sphinx-doc/sphinx` / 3.4 | `dd1615c59dc6fff633e27dbb3861f2d27e1fb976` | 待测 |
| 41 | `sphinx-doc__sphinx-8551` | `sphinx-doc/sphinx` / 3.4 | `57ed10c68057c96491acbd3e62254ccfaf9e3861` | 待测 |
| 42 | `sphinx-doc__sphinx-8638` | `sphinx-doc/sphinx` / 4.0 | `4b452338f914d4f6b54704222d70ae8a746e3db5` | 待测 |
| 43 | `sphinx-doc__sphinx-8721` | `sphinx-doc/sphinx` / 3.5 | `82ef497a8c88f0f6e50d84520e7276bfbf65025d` | 待测 |
| 44 | `sphinx-doc__sphinx-9229` | `sphinx-doc/sphinx` / 4.1 | `876fa81e0a038cda466925b85ccf6c5452e0f685` | 待测 |
| 45 | `sphinx-doc__sphinx-9230` | `sphinx-doc/sphinx` / 4.1 | `567ff22716ac258b9edd2c1711d766b440ac0b11` | 待测 |
| 46 | `sphinx-doc__sphinx-9281` | `sphinx-doc/sphinx` / 4.1 | `8ec06e9a1bd862cd713b9db748e039ccc7b3e15b` | 待测 |
| 47 | `sphinx-doc__sphinx-9320` | `sphinx-doc/sphinx` / 4.1 | `e05cef574b8f23ab1b57f57e7da6dee509a4e230` | 待测 |
| 48 | `sphinx-doc__sphinx-9367` | `sphinx-doc/sphinx` / 4.1 | `6918e69600810a4664e53653d6ff0290c3c4a788` | 待测 |
| 49 | `sphinx-doc__sphinx-9461` | `sphinx-doc/sphinx` / 4.2 | `939c7bb7ff7c53a4d27df067cea637540f0e1dad` | 待测 |
| 50 | `sphinx-doc__sphinx-9698` | `sphinx-doc/sphinx` / 4.3 | `f050a7775dfc9000f55d023d36d925a8d02ccfa8` | 待测 |

## 结果汇总表

| # | instance_id | Sigma 执行状态 | 官方评分 | F2P 通过 / 总数 | P2P 回归 | 轮数 / 工具数 | 耗时 | Token / 费用 | 备注 |
|---:|---|---|---|---:|---:|---:|---:|---:|---|
| 1 | `django__django-11790` | 完成 | unresolved | 0/2 | 77/77 通过 | 44 / 58 | 约 4.5 分钟 | 172,572 / 11,786 tokens | gold 1/1；raw 与 source-only 均未通过 F2P，属性值类型不符 |
| 2 | `django__django-11815` | 未运行 | 未评分 | — | — | — | — | — | — |
| 3 | `django__django-11848` | 未运行 | 未评分 | — | — | — | — | — | — |
| 4 | `django__django-11880` | 未运行 | 未评分 | — | — | — | — | — | — |
| 5 | `django__django-11885` | 未运行 | 未评分 | — | — | — | — | — | — |
| 6 | `django__django-11951` | 未运行 | 未评分 | — | — | — | — | — | — |
| 7 | `django__django-11964` | 未运行 | 未评分 | — | — | — | — | — | — |
| 8 | `django__django-11999` | 未运行 | 未评分 | — | — | — | — | — | — |
| 9 | `django__django-12039` | 未运行 | 未评分 | — | — | — | — | — | — |
| 10 | `django__django-12050` | 未运行 | 未评分 | — | — | — | — | — | — |
| 11 | `django__django-12143` | 未运行 | 未评分 | — | — | — | — | — | — |
| 12 | `django__django-12155` | 未运行 | 未评分 | — | — | — | — | — | — |
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
| 40 | `sphinx-doc__sphinx-8548` | 未运行 | 未评分 | — | — | — | — | — | — |
| 41 | `sphinx-doc__sphinx-8551` | 未运行 | 未评分 | — | — | — | — | — | — |
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
