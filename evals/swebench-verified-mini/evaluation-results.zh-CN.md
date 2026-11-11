# SWE-bench Verified Mini：结果总览

[English](evaluation-results.md) | [简体中文](evaluation-results.zh-CN.md)

更新日期：2026-11-09

## 50 题结果总表

截至本次更新，官方候选评分有 **40 题 resolved、9 题 unresolved**；#9229 没有候选官方评分，按用户确定的进度口径计为未解决，因此当前进度为 **40/50 = 80%**。表中混有不同历史模型、Skill、Memory 和引导条件；短备注标出重要的 issue-only 失败、辅助重跑和未评分情况。逐题运行记录见[合并后的任务档案](tasks/README.md)，失败根因和后续分析见[失败分析](failure-analysis.md)。

**F2P** 是基线版本会失败、用来验证修复目标的测试；**P2P** 是基线已通过、用来检查补丁是否引入回归的测试。resolved 要求所有 F2P 通过且 P2P 无回归。没有候选官方评分的任务不等同于 evaluator 判定的 unresolved。

| # | instance_id | Sigma 执行状态 | 官方评分 | F2P 通过 / 总数 | P2P 通过 / 总数 | 轮数 / 工具数 | 耗时 | Token（输入 / 输出） | 备注 |
|---:|---|---|---|---:|---|---:|---|---|---|
| 1 | [django__django-11790](tasks/README.md#django__django-11790) | DeepSeek Flash, completed on rerun | resolved | 2/2 | 77/77 passed | 40 / 47 | not recorded | 854,540 input / 7,012 output | — |
| 2 | [django__django-11815](tasks/README.md#django__django-11815) | MiniMax M3.1 Flash 多次尝试；官方候选已评分 | resolved | 2/2 | 44/44 通过 | 72 / 83 | 约 11 分钟 | 1,634,776 输入 / 19,227 输出 | — |
| 3 | [django__django-11848](tasks/README.md#django__django-11848) | DeepSeek Flash；更新 Python Skill 后重跑 resolved | resolved | 2/2 | 43/43 通过 | 新跑 85 / 109；旧运行合计 135 / 142 | 新跑约 4 分钟 | 新跑 2,254,275 输入 / 15,604 输出（缓存读取 1,466,240）；旧运行另计 | 旧运行时间接口不匹配；重跑通过 |
| 4 | [django__django-11880](tasks/README.md#django__django-11880) | 完成（保存的 issue-only session 续跑） | resolved | 1/1 | 114/114 通过 | 15 / 13（续跑） | 约 1 分钟续跑 | 366,119 输入 / 7,337 输出 tokens | — |
| 5 | [django__django-11885](tasks/README.md#django__django-11885) | 原 MiniMax M3 未完成；DeepSeek thinking 辅助重跑完成 | 原运行未评分；辅助重跑 resolved | 辅助 F2P 1/1 | 辅助 P2P 43/43 | 63 / 70（辅助重跑） | 约 4 分钟 Agent + 36 秒评测 | 辅助重跑 1,478,198 输入 / 75,718 输出 | thinking 辅助通过 |
| 6 | [django__django-11951](tasks/README.md#django__django-11951) | 完成（429 后同 session 续跑） | resolved | 1/1 | 21/21 通过 | 61 / 71 | 约 4 分钟累计模型运行 | 1,328,297 输入 / 34,723 输出 tokens | — |
| 7 | [django__django-11964](tasks/README.md#django__django-11964) | 完成（首轮越界后，同 session 加命令路径门禁续跑） | resolved | 2/2 | 15/15 通过 | 57 / 63（续跑） | 约 14 分钟续跑 | 1,473,611 输入 / 79,611 输出 tokens（续跑） | — |
| 8 | [django__django-11999](tasks/README.md#django__django-11999) | 完成（MiniMax 流错误后同 session 续跑） | resolved | 1/1 | 30/30 通过 | 125 / 144（两次运行合计） | 约 38 分钟 | 3,128,686 输入 / 209,711 输出 tokens | — |
| 9 | [django__django-12039](tasks/README.md#django__django-12039) | 完成（Token Plan 恢复后同 session 续跑；M3.1 中断后切换 M3） | resolved | 1/1 | 12/12（F2P 1/1、P2P 11/11） | 185 / 216（含 1 次额度失败重试；M3 续跑 96 / 95） | M3 续跑约 6 分 19 秒；评分约 34 / 28 秒 | 4,734,474 输入 / 303,264 输出 tokens（混合模型累计） | — |
| 10 | [django__django-12050](tasks/README.md#django__django-12050) | 完成（中断后续原 session） | resolved | 1/1 | 10/10 通过 | 88 / 86（累计） | Agent 运行约数分钟，官方测试约 30 秒 | 2,095,876 输入 / 22,456 输出 tokens | — |
| 11 | [django__django-12143](tasks/README.md#django__django-12143) | 完成 | resolved | 1/1 | 52/52 通过 | 19 / 25 | 约 1 分钟（含评分约 30 秒） | 266,136 输入 / 3,595 输出 tokens | — |
| 12 | [django__django-12155](tasks/README.md#django__django-12155) | 完成 | resolved | 1/1 | 6/6 通过 | 24 / 27 | 约 1 分钟 Agent，评分 29 秒 | 408,472 输入 / 4,075 输出 tokens | — |
| 13 | [django__django-12193](tasks/README.md#django__django-12193) | 完成 | resolved | 1/1 | 11/11 通过 | 15 / 17 | Agent 约 1 分钟，评分 28 秒 | 172,585 输入 / 2,610 输出 tokens | — |
| 14 | [django__django-12209](tasks/README.md#django__django-12209) | 完成 | resolved | 1/1 | 0/0 | 31 / 37 | Agent 约 2 分钟，评分 30 秒；neutral control 29 秒 | 593,728 输入 / 6,254 输出 tokens | — |
| 15 | [django__django-12262](tasks/README.md#django__django-12262) | 完成（source-only 官方评分） | resolved | 4/4 | 18/18 通过 | 45 / 48 | 约 3 分钟 | 953,210 输入 / 6,828 输出 tokens | — |
| 16 | [django__django-12273](tasks/README.md#django__django-12273) | DeepSeek Flash thinking medium，Skill-guided 重跑完成 | resolved | 2/2 | 27/27 passed | 65 / 78 | Agent 约 5 分钟，评分 28 秒 | 1,553,719 输入 / 60,746 输出 tokens（缓存命中 1,176,832） | Skill 引导重跑通过 |
| 17 | [django__django-12276](tasks/README.md#django__django-12276) | DeepSeek Flash, edit-guard retry | resolved | 2/2 | 117/117 passed | 43 / 48 | not recorded | 903,169 input / 7,529 output | — |
| 18 | [django__django-12304](tasks/README.md#django__django-12304) | DeepSeek Flash, completed issue-only run | resolved | 1/1 | 17/17 passed | 51 / 56 | not recorded | 1,137,187 input / 10,559 output | — |
| 19 | [django__django-12308](tasks/README.md#django__django-12308) | 完成（Python Skill 独立重测；verified） | resolved | 2/2 | 20/20 通过 | 29 / 34 | Agent 约 3 分钟；gold 42 秒、candidate 35 秒 | 452,421 输入 / 4,436 输出 tokens（缓存命中 391,040） | — |
| 20 | [django__django-12325](tasks/README.md#django__django-12325) | DeepSeek Flash thinking，guided 复跑完成 | resolved | 2/2 | 201/201 | 21 / 34 | 未记录 | 438,228 输入 / 14,774 输出 | issue-only失败；guided通过 |
| 21 | [django__django-12406](tasks/README.md#django__django-12406) | DeepSeek Flash thinking，guided Sigma 复跑完成 | resolved | 3/3 | 165/165 | 41 / 51 | 未记录 | 933,954 输入 / 34,305 输出 | issue-only失败；guided通过 |
| 22 | [django__django-12708](tasks/README.md#django__django-12708) | DeepSeek Flash, resumed session | resolved | 1/1 | 101/101 passed | 190 / 205 cumulative | not recorded | 5,294,939 input / 30,076 output | — |
| 23 | [django__django-12713](tasks/README.md#django__django-12713) | DeepSeek Flash, completed run | resolved | 1/1 | 58/58 passed | 39 / 41 | not recorded | 797,307 input / 7,023 output | — |
| 24 | [django__django-12774](tasks/README.md#django__django-12774) | DeepSeek Flash, completed run | resolved | 1/1 | 41/41 passed | 59 / 68 | not recorded | 1,301,749 input / 8,503 output | — |
| 25 | [django__django-9296](tasks/README.md#django__django-9296) | DeepSeek Flash，完成并验证 | resolved | 1/1 | 20/20 | 42 / 46 | 未记录 | 未记录 | — |
| 26 | [sphinx-doc__sphinx-10323](tasks/README.md#sphinx-doc__sphinx-10323) | DeepSeek Flash，verified | resolved | 1/1 | 40/40 通过 | 108 / 111 | 未记录 | 未记录 | — |
| 27 | [sphinx-doc__sphinx-10435](tasks/README.md#sphinx-doc__sphinx-10435) | DeepSeek Flash，完成并验证 | unresolved | 0/1 | 74/74 通过 | 49 / 54 | 未记录 | 未记录 | F2P未通过 |
| 28 | [sphinx-doc__sphinx-10466](tasks/README.md#sphinx-doc__sphinx-10466) | DeepSeek Flash, completed source-only run | resolved | 1/1 | 6/6 passed | not recorded | not recorded | not recorded | — |
| 29 | [sphinx-doc__sphinx-10673](tasks/README.md#sphinx-doc__sphinx-10673) | Agent incomplete（52轮后限；续跑至66轮） | resolved | 1/1 | 9/9 通过（F2P 1/1、P2P 9/9） | 66 / 75 | 约 5 分钟 | 1,566,041 输入 / 11,786 输出 tokens | — |
| 30 | [sphinx-doc__sphinx-11510](tasks/README.md#sphinx-doc__sphinx-11510) | Agent incomplete（新一轮 40 轮封顶） | unresolved | 0/2 | 0/7 通过（7 项回归） | 40 / 57 | Agent 约 2 分钟；评分 27 秒 | 840,309 输入 / 11,002 输出 tokens（缓存读取 572,928） | 既有测试回归 |
| 31 | [sphinx-doc__sphinx-7590](tasks/README.md#sphinx-doc__sphinx-7590) | Agent invalid/incomplete（修复请求序列后续跑至输入 token 硬上限） | unresolved | 0/1 | 24/24 通过 | 56 / 59（含两次零 token 400 重试） | 约 2 分钟 | 1,237,872 输入 / 6,362 输出 tokens（缓存读取 1,042,816；未命中 195,056） | F2P未通过 |
| 32 | [sphinx-doc__sphinx-7748](tasks/README.md#sphinx-doc__sphinx-7748) | 原运行 incomplete；DeepSeek thinking 新跑 completed，仍未解决 | unresolved | 0/2 | 12/12 通过（F2P 0/2、P2P 12/12） | 原 66 / 72；新跑 88 / 91 | 原约 5 分钟；新跑约 9.5 分钟 | 原 1,650,336 / 12,053；新跑 2,252,344 / 56,188 tokens（缓存读取 1,251,200） | F2P未通过 |
| 33 | [sphinx-doc__sphinx-7757](tasks/README.md#sphinx-doc__sphinx-7757) | 完成（verified） | resolved | 1/1 | 33/33 通过（F2P 1/1、P2P 33/33） | 69 / 74 | — | 未记录 | — |
| 34 | [sphinx-doc__sphinx-7985](tasks/README.md#sphinx-doc__sphinx-7985) | DeepSeek Flash thinking 完成（75 轮）；官方未解决 | unresolved | 最新 F2P 0/2 | 最新 P2P 3/3 | 原 160；MiniMax 121；DeepSeek 75 / 80 calls | Agent 约 9 分钟；raw/source-only 评分各约 29 秒 | 原 8,520,136 / 75,616；MiniMax 3,055,144 / 21,426 + 32 轮 691,274 / 7,051；DeepSeek 1,898,434 / 60,181（cache-read 1,284,992） | F2P未通过 |
| 35 | [sphinx-doc__sphinx-8035](tasks/README.md#sphinx-doc__sphinx-8035) | DeepSeek thinking 67 轮 incomplete；此前 MiniMax 已评分 unresolved | unresolved | 0/1 | 1/2 通过（1 项既有测试回归） | MiniMax 200 / 220；DeepSeek 67 / 77 | — | MiniMax 5,058,588 输入 / 34,794 输出；DeepSeek 1,618,725 输入 / 93,797 输出（缓存读取 872,320，未评分） | 未收敛；既有测试回归 |
| 36 | [sphinx-doc__sphinx-8056](tasks/README.md#sphinx-doc__sphinx-8056) | DeepSeek Flash thinking；更新后的 Sphinx Skill 引导重测 | resolved | 1/1 | 40/40 通过 | 41 / 46 | Agent 约 3 分 17 秒；gold 26 秒、candidate 29 秒 | 1,028,260 输入 / 18,412 输出 tokens；缓存读取 636,160（61.9%） | 更新 Skill 后通过 |
| 37 | [sphinx-doc__sphinx-8265](tasks/README.md#sphinx-doc__sphinx-8265) | Agent failed finalization（128 轮；漏掉最终 git_diff） | resolved | 1/1 | 22/22 通过 | 128 / 94 | 约 11 分钟 | 3,430,153 输入 / 44,537 输出 tokens；缓存命中 2,075,520（60.5%） | [官方评分通过；Agent未收尾](tasks/README.md#sphinx-doc__sphinx-8265) |
| 38 | [sphinx-doc__sphinx-8269](tasks/README.md#sphinx-doc__sphinx-8269) | 完成但 verification_status=test_failed（29 轮） | resolved | 1/1 | 5/5 通过 | 29 / 34 | Agent 约 1 分钟；官方候选评分约 116 秒、26 秒、35 秒 | 687,382 输入 / 4,877 输出 tokens；缓存命中 477,952（69.5%） | [官方评分通过](tasks/README.md#sphinx-doc__sphinx-8269) |
| 39 | [sphinx-doc__sphinx-8475](tasks/README.md#sphinx-doc__sphinx-8475) | 完成（同 session 续跑） | resolved | 1/1 | 18/18（F2P 1/1、P2P 17/17） | 27 / 26（attempt 2 累计；attempt 1 另 14 / 19） | 约 1 分钟续跑；官方 gold/candidate 各约 33 秒 | 两 session 合计 849,166 输入 / 5,145 输出 tokens；续跑 session cache-read 424,192（60.8%） | — |
| 40 | [sphinx-doc__sphinx-8548](tasks/README.md#sphinx-doc__sphinx-8548) | MiniMax M3 重试 incomplete；最近官方评分 unresolved | unresolved | 原 F2P 1/1 | 原 P2P 4/5（`test_instance_variable` 失败）；新尝试定向套件 133 passed、1 环境失败 | 原 128 轮；新续跑累计计数未落盘 | 旧运行约 7 分钟；本次详情见归档 | 原运行 3,113,431 输入 / 17,195 输出；新首段 3,341,304 输入 / 25,911 输出，续跑未结算 | F2P通过；P2P回归 |
| 41 | [sphinx-doc__sphinx-8551](tasks/README.md#sphinx-doc__sphinx-8551) | M3 到 128 轮上限，最终 diff 门禁未完成 | resolved | 1/1 | 33/33 通过（F2P 1/1、P2P 33/33） | 128 / 123 | 约 13 分钟 | 3,107,707 输入 / 24,896 输出 tokens | — |
| 42 | [sphinx-doc__sphinx-8638](tasks/README.md#sphinx-doc__sphinx-8638) | 原始 128 轮 + 52 轮有效离线续跑（另有 20 轮误启用 MCP 的无效片段）；续跑达到 52 轮上限，verification incomplete | unresolved | 0/1 | 33/35（F2P 0/1，P2P 33/34） | 52 / 52（另有无效片段 20 轮） | 约 20 分钟 | 1,439,085 输入 / 7,091 输出 tokens | F2P未通过；续跑未收尾 |
| 43 | [sphinx-doc__sphinx-8721](tasks/README.md#sphinx-doc__sphinx-8721) | MiniMax M3，79 轮（Agent 元数据 verification_status=test_failed） | resolved | 1/1 | 4/4（F2P 1/1、P2P 3/3） | 79 | 约 1 分钟评分；Agent 运行时间见原始日志 | 2,019,023 输入 / 10,561 输出 tokens | — |
| 44 | [sphinx-doc__sphinx-9229](tasks/README.md#sphinx-doc__sphinx-9229) | 用户按 DeepSeek 更新 Skill 运行满 128 轮仍无生产补丁，判为 unresolved | 未做候选官方评分 | — | — | M3 128 / 131；DeepSeek 63 / 65；更新 Skill 128 / 146 calls | 首次 thinking 重跑有三次约 302 秒构建；更新 Skill 运行分两段约 22 分钟，scratch 构建命令最长约 8 秒 | 更新 Skill 运行合计 3,335,450 输入 / 150,050 输出；前次 1,643,276 / 57,572 | 未评分；无生产改动 |
| 45 | [sphinx-doc__sphinx-9230](tasks/README.md#sphinx-doc__sphinx-9230) | MiniMax M3；原 124 轮中断后同 session 续 44 轮（累计 168） | resolved | 1/1 | 45/45（F2P 1/1、P2P 44/44） | 168 / 164 工具调用 | Agent 活跃时间约 28 分钟，分段见日志 | 4,338,020 输入 / 37,092 输出 tokens | — |
| 46 | [sphinx-doc__sphinx-9281](tasks/README.md#sphinx-doc__sphinx-9281) | MiniMax M3，124 轮（Agent verification_status=test_failed） | resolved | 1/1 | 39/39（F2P 1/1、P2P 38/38） | 124 / 92 | Agent 约 18 分钟；官方评分约 27 秒 | 3,159,234 输入 / 38,423 输出 tokens | — |
| 47 | [sphinx-doc__sphinx-9320](tasks/README.md#sphinx-doc__sphinx-9320) | MiniMax M3，71 轮（Agent completed） | resolved | 1/1 | 10/10（F2P 1/1、P2P 9/9） | 71 / 82 | Agent 约 7 分钟；gold 与候选评分约 51 秒 | 1,837,224 输入 / 13,290 输出 tokens | — |
| 48 | [sphinx-doc__sphinx-9367](tasks/README.md#sphinx-doc__sphinx-9367) | MiniMax M3，29 轮（Agent completed） | resolved | 1/1 | 26/26（F2P 1/1、P2P 25/25） | 29 / 34 | Agent 约 3 分钟；gold 与候选评分约 25 秒 | 596,622 输入 / 4,700 输出 tokens | — |
| 49 | [sphinx-doc__sphinx-9461](tasks/README.md#sphinx-doc__sphinx-9461) | MiniMax M3，128 轮 + 同 session 续跑 48 轮（中断，累计 176） | unresolved | 0/1 | 59/62（F2P 0/3、P2P 59/59） | 176 / 187 | Agent 约 17 分钟；gold 与候选评分约 56 秒 | 4,559,230 输入 / 33,015 输出 tokens | F2P未通过；运行中断 |
| 50 | [sphinx-doc__sphinx-9698](tasks/README.md#sphinx-doc__sphinx-9698) | MiniMax M3，128 轮（runtime failed finalization） | resolved | 1/1 | 46/46（F2P 1/1、P2P 45/45） | 128 / 62 | Agent 约 14 分钟；gold 与候选评分约 52 秒 | 3,064,371 输入 / 41,004 输出 tokens | [source-only通过](tasks/README.md#sphinx-doc__sphinx-9698) |
