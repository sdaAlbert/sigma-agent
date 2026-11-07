# Pi Agent 50 题评测计划

**状态：**任务候选已筛选；尚未运行 Pi 或 Sigma，也尚未调用模型 API。  
**评测目标：**先让 Pi Agent 在固定的 50 道 Multi-SWE-Bench Go 任务上跑完，建立 Pi 基线。Sigma 过去跑过的任务包含在题集中，但历史 Sigma 结果只作背景；若要得出 Pi/Sigma 对比结论，之后必须在同一协议下重新跑 Sigma。

## 为什么选 50 题

- 覆盖 3 个 Go 代码库：15 道 go-zero、16 道 grpc-go、19 道 cli/cli。
- go-zero 的 15 道全部纳入，因此包含本项目之前尝试过的任务，也包含尚未测过的任务；不会只挑 Sigma 通过的题。
- grpc-go 纳入当前官方已验证镜像清单中的全部 16 道任务。
- cli/cli 的 19 道从固定版本的官方已验证镜像清单中，以随机种子 `20261102` 抽取。抽样池有 397 个 PR 镜像；任务列表按 PR 号排序后写在下表。
- 50 题是本项目自选的 Multi-SWE-Bench Go 子集，不是官方 split、leaderboard 成绩或 Verified 结果。

## 固定任务清单

`Sigma 既有记录` 只表示本机截至 2026-11-02 找到的历史尝试，不是本次匹配评测结果。Pi 本轮对每个任务都从干净 checkout 开始。

| # | Multi-SWE-Bench task ID | 仓库 / evaluator image | Sigma 既有记录（仅供背景） |
|---:|---|---|---|
| 1 | `zeromicro__go-zero-964` | `mswebench/zeromicro_m_go-zero:pr-964` | Issue-only 未解决；详细规格辅助运行曾通过 |
| 2 | `zeromicro__go-zero-990` | `mswebench/zeromicro_m_go-zero:pr-990` | 未发现历史评测记录 |
| 3 | `zeromicro__go-zero-1456` | `mswebench/zeromicro_m_go-zero:pr-1456` | 曾启动但中断，无分数 |
| 4 | `zeromicro__go-zero-1783` | `mswebench/zeromicro_m_go-zero:pr-1783` | Issue-only 1/1 resolved；目前唯一记录的干净成功 |
| 5 | `zeromicro__go-zero-1821` | `mswebench/zeromicro_m_go-zero:pr-1821` | 未发现历史评测记录 |
| 6 | `zeromicro__go-zero-1907` | `mswebench/zeromicro_m_go-zero:pr-1907` | 未发现历史评测记录 |
| 7 | `zeromicro__go-zero-1964` | `mswebench/zeromicro_m_go-zero:pr-1964` | 未发现历史评测记录 |
| 8 | `zeromicro__go-zero-1969` | `mswebench/zeromicro_m_go-zero:pr-1969` | 根因提示辅助运行曾通过 |
| 9 | `zeromicro__go-zero-2032` | `mswebench/zeromicro_m_go-zero:pr-2032` | Issue-only 未解决；hint 辅助的 source-only patch 通过目标测试，原始 patch 未获 resolved |
| 10 | `zeromicro__go-zero-2116` | `mswebench/zeromicro_m_go-zero:pr-2116` | 未发现历史评测记录 |
| 11 | `zeromicro__go-zero-2283` | `mswebench/zeromicro_m_go-zero:pr-2283` | Skill 和 memory 运行均未获 resolved；已有 evaluator 诊断记录 |
| 12 | `zeromicro__go-zero-2363` | `mswebench/zeromicro_m_go-zero:pr-2363` | 未发现历史评测记录 |
| 13 | `zeromicro__go-zero-2463` | `mswebench/zeromicro_m_go-zero:pr-2463` | 未发现历史评测记录 |
| 14 | `zeromicro__go-zero-2537` | `mswebench/zeromicro_m_go-zero:pr-2537` | Issue-only 未解决；给 PR diff 的参考实现曾通过 |
| 15 | `zeromicro__go-zero-2787` | `mswebench/zeromicro_m_go-zero:pr-2787` | 未发现历史评测记录 |
| 16 | `grpc__grpc-go-2371` | `mswebench/grpc_m_grpc-go:pr-2371` | 未发现历史评测记录 |
| 17 | `grpc__grpc-go-2629` | `mswebench/grpc_m_grpc-go:pr-2629` | 未发现历史评测记录 |
| 18 | `grpc__grpc-go-2630` | `mswebench/grpc_m_grpc-go:pr-2630` | 未发现历史评测记录 |
| 19 | `grpc__grpc-go-2631` | `mswebench/grpc_m_grpc-go:pr-2631` | 未发现历史评测记录 |
| 20 | `grpc__grpc-go-2735` | `mswebench/grpc_m_grpc-go:pr-2735` | 未发现历史评测记录 |
| 21 | `grpc__grpc-go-2744` | `mswebench/grpc_m_grpc-go:pr-2744` | 未发现历史评测记录 |
| 22 | `grpc__grpc-go-2760` | `mswebench/grpc_m_grpc-go:pr-2760` | 未发现历史评测记录 |
| 23 | `grpc__grpc-go-2932` | `mswebench/grpc_m_grpc-go:pr-2932` | 未发现历史评测记录 |
| 24 | `grpc__grpc-go-2951` | `mswebench/grpc_m_grpc-go:pr-2951` | 未发现历史评测记录 |
| 25 | `grpc__grpc-go-2996` | `mswebench/grpc_m_grpc-go:pr-2996` | 未发现历史评测记录 |
| 26 | `grpc__grpc-go-3119` | `mswebench/grpc_m_grpc-go:pr-3119` | 未发现历史评测记录 |
| 27 | `grpc__grpc-go-3201` | `mswebench/grpc_m_grpc-go:pr-3201` | 未发现历史评测记录 |
| 28 | `grpc__grpc-go-3258` | `mswebench/grpc_m_grpc-go:pr-3258` | 未发现历史评测记录 |
| 29 | `grpc__grpc-go-3351` | `mswebench/grpc_m_grpc-go:pr-3351` | 未发现历史评测记录 |
| 30 | `grpc__grpc-go-3361` | `mswebench/grpc_m_grpc-go:pr-3361` | 未发现历史评测记录 |
| 31 | `grpc__grpc-go-3476` | `mswebench/grpc_m_grpc-go:pr-3476` | 未发现历史评测记录 |
| 32 | `cli__cli-352` | `mswebench/cli_m_cli:pr-352` | 未发现历史评测记录 |
| 33 | `cli__cli-625` | `mswebench/cli_m_cli:pr-625` | 未发现历史评测记录 |
| 34 | `cli__cli-1279` | `mswebench/cli_m_cli:pr-1279` | 未发现历史评测记录 |
| 35 | `cli__cli-1457` | `mswebench/cli_m_cli:pr-1457` | 未发现历史评测记录 |
| 36 | `cli__cli-2249` | `mswebench/cli_m_cli:pr-2249` | 未发现历史评测记录 |
| 37 | `cli__cli-2888` | `mswebench/cli_m_cli:pr-2888` | 未发现历史评测记录 |
| 38 | `cli__cli-3472` | `mswebench/cli_m_cli:pr-3472` | 未发现历史评测记录 |
| 39 | `cli__cli-5452` | `mswebench/cli_m_cli:pr-5452` | 未发现历史评测记录 |
| 40 | `cli__cli-5588` | `mswebench/cli_m_cli:pr-5588` | 未发现历史评测记录 |
| 41 | `cli__cli-5648` | `mswebench/cli_m_cli:pr-5648` | 未发现历史评测记录 |
| 42 | `cli__cli-7180` | `mswebench/cli_m_cli:pr-7180` | 未发现历史评测记录 |
| 43 | `cli__cli-7376` | `mswebench/cli_m_cli:pr-7376` | 未发现历史评测记录 |
| 44 | `cli__cli-7759` | `mswebench/cli_m_cli:pr-7759` | 未发现历史评测记录 |
| 45 | `cli__cli-7906` | `mswebench/cli_m_cli:pr-7906` | 未发现历史评测记录 |
| 46 | `cli__cli-8146` | `mswebench/cli_m_cli:pr-8146` | 未发现历史评测记录 |
| 47 | `cli__cli-8238` | `mswebench/cli_m_cli:pr-8238` | 未发现历史评测记录 |
| 48 | `cli__cli-8395` | `mswebench/cli_m_cli:pr-8395` | 未发现历史评测记录 |
| 49 | `cli__cli-9866` | `mswebench/cli_m_cli:pr-9866` | 未发现历史评测记录 |
| 50 | `cli__cli-9934` | `mswebench/cli_m_cli:pr-9934` | 未发现历史评测记录 |

## 运行前必须锁定的配置

### Pi 版本

上游 Pi 已迁移到 [`earendil-works/pi`](https://github.com/earendil-works/pi)，官方包是 [`@earendil-works/pi-coding-agent`](https://www.npmjs.com/package/@earendil-works/pi-coding-agent)。本机 `pi` 当前为 `1.5.0`，与官方包一致；评测固定该版本，不再安装旧的 `@mariozechner` 包，也不改写现有全局安装。单独构建可复用的 Linux Pi runner 镜像，在其中安装固定版本。Pi 以 `--mode json` 无交互运行，使用隔离的 `PI_CODING_AGENT_DIR`；配置中只引用容器环境变量里的 API key，不写入镜像或仓库。

### 模型与工具

- Pi 和后续 Sigma 匹配复跑均使用同一个 MiniMax-M3.1-Flash 模型 ID、API endpoint、温度与最大上下文设置。正式启动前保存脱敏配置快照；API key 只从本地 secret/env 读取，不写进仓库、计划文档或运行日志。
- Agent 只收到官方 issue 标题、正文和通用“调查并修复，运行相关测试”的任务说明。不得把 PR 标题/正文、PR URL、commit、hint、fix patch、test patch 或 evaluator 输出交给 Agent。
- 每题从对应 base commit 的干净 checkout 开始。关闭 GitHub MCP、联网搜索、Skills、自定义 extensions、Sigma memory 和跨任务 session；避免 agent 访问 benchmark gold data。
- 保留 Pi 自带的系统提示、内置文件/终端工具和正常上下文管理；这轮测的是 Pi coding agent 产品基线，不做删减 Pi 能力的对照。
- 预算用统一硬上限：每题最多 128 次模型调用或累计 1,000,000 token（输入+输出），先到者停止；provider 错误只允许按固定退避策略重试，重试计入日志。50 个任务均运行一次，不能因结果不理想而只重跑失败题。若上限需调整，须在第一题前改文档并重新冻结。

## 执行顺序

| 阶段 | 工作 | 完成条件 |
|---|---|---|
| 0. 锁版本与配置 | 安装/调用固定的上游 Pi 包；将 MiniMax-M3.1-Flash 配置放在隔离目录；记录 Pi、Node、模型、API 参数和预算 | Pi `--version` 指向固定上游包；脱敏配置快照可复现；不泄露 key |
| 1. 题目预检 | 用数据集 revision 校验 50 个 task ID、issue body、base commit、test patch 与镜像 tag；检查本机 Docker 镜像/拉取能力 | 50 条逐题 manifest；缺项任务标 `invalid` 并附证据，不静默换题；Issue 文本与 benchmark 行一致 |
| 2. 单题冒烟 | 只处理清单首题 `zeromicro__go-zero-964`：Pi runner 挂载干净 base checkout，只收到 issue；评分阶段才取得该题 evaluator 镜像，先跑 gold oracle，再跑 Pi patch | Transcript 和 patch 成功保存，隐藏 patch 未进入 Pi workspace；oracle 和候选评分可复现；随后删除该题专属 evaluator image |
| 3. Pi 全量（严格串行） | 按表格顺序一次只处理一道题。Pi runner 使用同一个可复用容器；对当前题只拉取/构建一个 evaluator image，依次跑 gold oracle 和 Pi patch 评分，保存所有产物后删除当前 PR 专属 image，再处理下一题 | 50 题都有 `resolved / unresolved / invalid / provider-error` 状态与可追溯产物；失败也保留；Docker 并发 worker 全设为 1 |
| 4. 汇总复核 | 分仓库统计 resolved、有效题数、无效题、provider/evaluator 错误；逐题核对 patch 与评测日志 | 输出总体及仓库分项结果；有效分母与所有剔除原因公开 |
| 5. Sigma 配对 | 如要声称 Pi 与 Sigma 谁更强，在同一 50 题、模型、预算和 evaluator 下重新运行 Sigma | 只有这批 fresh paired runs 用于 Pi/Sigma 胜负统计；Sigma 旧结果另列历史列 |

## Docker 与 C 盘占用

本机检查快照（2026-11-02）：C 盘约 **47 GiB 可用**；最近一次 `docker system df` 显示镜像约 **3.3 GB**、卷约 **2.1 GB**。Docker 里已有多份约 1.3 GB 的无标签镜像记录，开始评测前先对照任务清单确认它们是否是旧的 go-zero 评测残留。本轮禁止用 `download_images.ps1 images_verified.txt` 批量拉取，也不使用 8-worker 默认配置。

每道题按下面顺序做，不同时准备多道题的专属镜像：

1. Pi 在单个共享的 `pi-go-runner` 容器中工作。这个 runner 基于现有 `golang:latest` 环境，额外装一次 Node 和固定版本 Pi；只挂载当前题在 base commit 的干净仓库目录，不挂载评测数据 JSONL、`fix.patch`、`test.patch`、PR diff 或评测输出。
2. Pi 保存 patch 和 transcript 后，才为当前 PR 拉取/构建一个 task image。评测镜像仅用于 gold oracle 和候选 patch 的评分；二者顺序运行，评测容器由 evaluator 清理。
3. 把 report、日志、patch、token 统计和校验和复制到评测产物目录；检查归档文件可读后，删除当前 `mswebench/<repo>:pr-<number>` 专属镜像。
4. 同一仓库的 `mswebench/<repo>:base` 与 `golang:latest` 是复用底层，处理该仓库的任务期间保留；该仓库全部完成后再判断是否删除 repo base。若 evaluator 需要 `nix_swe:v1.0` 辅助镜像，只拉取并保留一份供整轮复用。
5. 每题前后查看 `docker system df` 和 C 盘剩余空间；设 **15 GiB** 为停止线，低于此值就暂停，不开始下一题。只删除确认属于当前任务的 PR 镜像，不运行全局 `docker system prune`。

官方 evaluator 会为任务创建临时容器并在运行后移除容器；image 不会因此自动删除。官方说明也支持按需准备镜像，缺失时才构建。Docker 镜像会共享基础层，因此删除 PR tag 可回收该题独有层，共享的 Go 基础层会保留供后续任务复用。`docker system df` 用来确认 Docker 内部实际占用；Docker Desktop 磁盘镜像文件的显示尺寸与实际占用可能不同。[Multi-SWE-Bench 镜像说明](https://github.com/multi-swe-bench/multi-swe-bench#run-evaluation) · [Docker 镜像清理说明](https://docs.docker.com/reference/cli/docker/image/rm/) · [Docker 磁盘占用说明](https://docs.docker.com/reference/cli/docker/system/df/)

## 记分规则与报告

- 主分数：在 gold oracle 通过的有效任务上，官方 evaluator `resolved_instances` / `valid_instances`。同时报告三仓库分项，不只报一个合并数字。
- 每题单列：原始 patch 是否应用、benchmark test patch 是否应用、目标测试结果、baseline 测试状态、Pi 调用/token、provider 错误、是否因 evaluator 无效而剔除。
- Gold oracle 通过是评分链路有效的必要条件，不代表 Agent 的 patch 正确。Agent 未通过时，再做人工的行为层复核；人工复核结论与官方分数分开写，不把“看起来修好了”冒充 `resolved`。
- 一题最多一次正式 Pi 生成。因明确基础设施问题重试时，保存原尝试并标注重试原因；不得只保留较好成绩。
- 旧 Sigma 历史数据包括 issue-only、hint 辅助、详细规格辅助、PR diff 参考、未完成等不同条件。不能和本轮 Pi 的 issue-only 分数直接相减或合并计算胜率。
- 当前结果适合表述为“在自选的 Multi-SWE-Bench Go 50 题子集上的本地评测”，不能表述为官方榜单成绩，也不等同于 MyPi 使用的 Terminal-Bench 测试。

## 数据来源

- Multi-SWE-Bench 数据集 revision：`56ff018c04a38e27ada1e9d0a6d5839a51f88f0d`，见[官方数据集卡](https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench)及[Go-zero 数据文件](https://huggingface.co/datasets/ByteDance-Seed/Multi-SWE-bench/blob/56ff018c04a38e27ada1e9d0a6d5839a51f88f0d/go/zeromicro__go-zero_dataset.jsonl)。
- 本地官方 evaluator checkout：`multi-swe-bench` commit `24f493f8a103e72312ded4f6b9c89f081d69cb09`；本地 `images_verified.txt` 用该 commit 的 task tags 选取任务。该 checkout 有与本实验无关的本地改动；正式跑分前需确认 evaluator 和镜像清单相关文件的内容/校验和。
- 官方 evaluator 接受以 org/repo/PR number 对应的 patch JSONL，并输出 `final_report.json`，见[官方 evaluator 说明](https://github.com/multi-swe-bench/multi-swe-bench#readme)。
- Pi 上游：[`earendil-works/pi`](https://github.com/earendil-works/pi)；npm 包：[`@earendil-works/pi-coding-agent`](https://www.npmjs.com/package/@earendil-works/pi-coding-agent)。
- 既有 Sigma 运行清单：[issue benchmark inventory](../../historical/go-issue-repair/issue-benchmark-inventory.md)。
