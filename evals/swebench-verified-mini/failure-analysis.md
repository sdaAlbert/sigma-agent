# 失败任务分析与复盘

> 本文保留逐题失败根因、运行证据、Skill/Memory 条件和后续突破假设。当前状态与官方分数以[50 题结果总表](evaluation-results.md#results-for-all-50-tasks)为准；本文不重复维护榜单或统计表。

## 背景：从 Pi 对标到 benchmark 驱动迭代

Sigma 的交互和编码体验以 Pi 为重要参照。Sigma 是用 Go 独立实现的 Agent；这里的“对标”指比较功能、行为和任务轨迹，不是把 Pi 的源码逐段复制进来。真实 benchmark 暴露出代码编辑安全、上下文交接、工具输出、模型协议、thinking 处理和验证闭环等问题后，Sigma 才逐步增加对应的 runtime 能力。逐阶段的代码改动、运行证据和归因边界见[Sigma 编码 Agent 迭代史](../../docs/SIGMA_CODING_AGENT_ITERATION_HISTORY.md)。

下面保留失败任务的具体原因、Agent 轨迹与改进依据，以及已有证据支持的下一步突破方向。最终状态仍以[结果总表](evaluation-results.md#results-for-all-50-tasks)为准。

## 已完成离线复盘：Sigma 为什么没解决这些题

**复盘状态：已完成，2026-11-07。** 我以本页逐题记录和可用归档摘要为索引；对关键失败进一步比对了 Agent transcript/events、候选补丁、官方报告/测试输出及可用的官方修复，包括 Django #12406、Sphinx #8056、#8638、#9461、#7590 和 #11510。#8035、#9229 等未产生正确候选的运行按归档产物状态归因，不假装存在可供比较的生产补丁。原始材料见 [`archive/benchmarks/swebench-verified-mini/runs`](../../../archive/benchmarks/swebench-verified-mini/runs)。

### 结论

这批失败没有一个能由“缺少某个 Skill”或“Qdrant 没记住答案”统一解释。证据更支持下面这个根因链：**模型经常没把 issue 约束变成可检验的行为，再把失败定位到负责该行为的代码层；Sigma 能提醒它测试和看 diff，但目前不能判断它选的测试是否可信、补丁是否覆盖验收要求，或它是不是只是在反复消耗预算。**

1. **实现契约没有稳定下来，导致改错层或扩大改动。** #12406 的候选改了通用 `ChoiceWidget`，没有把模型字段 `blank` 沿 `ForeignKey.formfield()` 传到 `ModelChoiceField`；官方修复沿字段构造链传递该属性。#8056 的候选在下游 docfield formatter 才拆 `x1, x2`，而 official fix 在 NumPy 参数收集时把名字拆成独立字段。#9461 的候选虽找到了 class property descriptor，却没把 classmethod 身份传到文档输出，三个 F2P 仍失败。#8638 的候选删掉 classname/module scoped fallback，只保留 exact-name lookup，目标 `pending_xref` 仍失败且 `find_obj` 回归。#11510 则把修改扩到 `Toctree` 等无关 directive，`other.py` 从 348 行缩到 252 行，两个 F2P 与七个 P2P 全失败。这些是“主题方向大致对，但没有守住行为所有权、完整数据流和补丁边界”的直接例子。
2. **局部验证不等于验证了原问题。** #12406 的 transcript 有大量测试与编辑活动，但 Agent 也修改了 `test_radioselect.py` 和 `test_modelchoicefield.py` 来适配自己的设计；官方 source-only 候选仍 F2P 0/3、P2P 161/165。#10435 的自测对 TeX `%` 注释符与换行的结尾顺序，和官方预期相反。问题不只是“没跑测试”，而是测试可能被改、测试范围可能偏离、或断言没有表达 issue 的真实语义。
3. **运行时 review 提供了流程约束，但不是语义验收器。** [`workflow_policy.go`](../../internal/agent/workflow_policy.go) 会在源码变化后要求针对最新版本跑测试并看 diff，也会提示失败诊断；它记录的是工具顺序、`purpose=test` 和命令退出状态。它不会检查测试文件是否被 Agent 改过、测试是否覆盖 issue 的验收条件，也不会比较候选是否满足所有 F2P/P2P。重复失败命令的保护也不等于检测“补丁没有进展”。因此运行时可保证“做过某种测试和 diff 检查”，不能保证“验证了正确行为”。
4. **长运行里存在探索停滞和预算管理失败。** #8035 的 160 轮与 200 轮候选补丁字节一致，后 40 轮没有带来实现进展；#9229 消耗约 3.40M 输入 token，128 轮后仍只有 scratch 文件，没有 tracked source patch 或目标测试。#34 也出现了 Skill 思路停留在分析、没落到生产代码的记录。这不是轮数不足就能自动解决的问题；一旦没有新的代码变化或更有区分力的测试结果，继续调用模型只会增加成本。
5. **环境问题存在，但不是这组有效失败的主因。** #31 的首轮确有 runtime 消息排序错误；修复后 continuation 不再触发 HTTP 400，但候选仍把下划线分隔符消费两次，`5_udl` 目标继续失败。#16 的早期源码截断也确实污染了第一次尝试。这些都要与后续有效候选分开归因。另一方面，#12406、#11510、#9461 等候选对应的 gold control 通过、候选 evaluator errors 为零，而 source-only 候选仍失败，说明这些具体失败来自候选实现，不是 evaluator 没启动或基线环境不可用。

### 对 Sigma 本体的判断

Sigma 提供工具、Skill、Memory 和基于变更/测试/diff 时序的工作流检查；**最关键的不足不在“没有能力运行命令”，而在运行时没把 issue 的验收语义、基准测试的可信性、最小行为复现和代码层归属变成可验证的硬约束。** Skill/Memory 内容被加载，只证明内容可用；#20、#27、#30、#49 等记录显示，加载后 Agent 仍可能违背其中的边界或没有实现完整语义。历史实验的模型、runtime、预算和 Skill/Memory 条件也变化过，不能从这份失败清单判断某一个模型、Skill 或记忆开关的因果效果。

所以，准确结论是：**在这批任务和这些历史配置下，Sigma 的 Agent loop 经常无法从分析可靠地走到“范围正确的生产补丁 + 未被篡改的目标测试通过 + 邻近回归通过 + 最终 diff 复核”；部分题甚至没有走到可评分补丁。** 这是当前实现质量与验证闭环的短板，不等于 Sigma 永远做不了这些题，也不能用零 F2P 子集估算一般任务成功率。

后续改进应优先针对这条闭环：保留基准测试原样并明确区分 scratch tests；每题先记录行为验收点和对应的最小复现；要求 Agent 指出负责该语义的数据流/代码层后再改；在检查点用 tracked-source diff 与目标测试进展判断是否继续；最后分别检查原始候选和 source-only 候选，并以官方 evaluator 作为得分依据。单纯增加 Skill 文字或 turn 上限没有这些机制的证据，不能当作修复。

## 逐题归因、Skill 证据与可行突破

### Task 03

**django__django-11848 — parse_http_date 两位年份判断**

问题：两位年份应按 RFC 850 规则结合当前年份解释。

官方结果与失败原因：F2P 0/2、P2P 43/43。候选用 `time.strftime('%Y')`，而官方测试只冻结了 `datetime.datetime.utcnow()`，因此两个 1970/1971 边界仍按实际运行年份解释成 2070/2071。这里有测试覆盖疑点：测试固定了某个具体时钟接口，没有覆盖其他取当前年份的写法；但 `utcnow()` 返回 UTC 年份，`time.strftime()` 默认按本地时区取年，两者在跨年时并不保证相同，所以候选也不能直接视为等价实现。

复盘与下一步：Agent 没注意到项目测试冻结的是 `datetime.datetime.utcnow()`，还用自己写的 `time.strftime()` mock 测试验证了实现，因此本地检查没有暴露问题。Python Skill 已提醒要沿用测试替换的时钟接口，但 Agent 没照做。这里也应记录一项测试设计疑点：测试只控制 `utcnow()`，无法验证其他可能正确的 UTC 时钟实现，说明覆盖范围与具体实现接口绑定得比较紧。不过这不等于候选一定正确，因为 `time.strftime()` 默认读取本地时间，在跨年时可能和 UTC 年份不同。稳妥的重测应使用测试控制的 UTC 时钟，并核对 50 年边界；复盘报告时则把它同时标作“Agent 未沿用测试 seam”和“测试接口可能过度耦合”，而不是单方面归为 Agent 算法错误或官方测试无效。

判断与预算：P3；历次累计 3.57M / 54.5k；若以后有新假设，≤1.0M / 10k。

任务档案：[Task 03 页面](tasks/README.md#django__django-11848)

### Task 05

**django__django-11885 — 合并按表执行的 fast delete 查询**

问题：Collector 对同一张表生成多条 fast-delete 查询时，应合并为一条带 OR 条件的查询，减少数据库往返。

官方结果与失败原因：未评分：128 轮、130 次工具调用后没有 tracked source patch；本地 delete 测试通过，但没有运行官方 evaluator。

复盘与下一步：Agent 找到了 Django 删除流程中的 Collector 和 QuerySet.delete，也跑过本地 delete 测试，但没有改生产代码。这个 issue 要解决的是：对同一张数据库表准备的多条 fast-delete 查询，应合并成一条带 OR 条件的查询，减少数据库往返。离线证据将失败定位在“分析没有收敛成生产改动”：没有候选补丁、没有官方 candidate evaluator，因而不能归因于某段错误实现，也不能用来判断 Skills 是否有效或题目是否可解。下一次若重新运行，应以同表查询合并作为验收行为，并设置能停止空转的进展检查；这不是尚未完成的离线代码比对。旧运行约消耗 320 万输入 token。

判断与预算：P2；无官方候选评分；旧运行约 3.20M / 24.2k。

任务档案：[Task 05 页面](tasks/README.md#django__django-11885)

### Task 16

**django__django-12273 — 重置子模型主键无效**

问题：多表继承中主键置空后保存应产生预期对象。

官方结果与失败原因：早期两次尝试分别发生源码截断和多表继承回归，均未通过。2026-11-09 的 DeepSeek Flash thinking medium 重跑官方 resolved 1/1，F2P 2/2、P2P 27/27；此轮包含通用 Python Skill 加载指引，属于 Skill-guided 辅助结果。

复盘：本次 Agent 在第 42 轮开始修改 `django/db/models/base.py`，添加新建对象与已有对象的父链接主键处理，并补上回归用例。它先通过 model inheritance 测试，再通过 basic、model_forms、serializers 和 update_only_fields 定向测试；官方评分随后确认两个 F2P 与 27 个 P2P 全部通过。相较早期直接改 `_get_pk_val()` 或破坏赋值行为，这次改动落在 `_save_parents()` 的保存路径，测试也覆盖了多继承场景。由于运行启用了 thinking 并通过通用指引加载 Python Skill，不能把成功单独归因于 thinking 或 Skill；但这次结果证明任务并非模型无法完成。

判断与预算：早期运行预算不足以消除实现偏差；本次 65 轮、78 次工具调用，1,553,719 输入 / 60,746 输出 tokens（缓存命中 1,176,832）。

任务档案：[Task 16 页面](tasks/README.md#django__django-12273)

### Task 20

**django__django-12325 — 多表继承中的 parent_link 校验**

固定清单里的 issue-only 运行仍为 unresolved：F2P 0/2、P2P 201/201。2026-11-07 又用开启 thinking 的 Sigma 重测，官方 source-only 候选 resolved，F2P 2/2、P2P 201/201；Agent 在 21 轮、34 次工具调用后完成验证。

这次成功不能直接归因于 thinking。加载的 Python Skill 已明确提示：在收集候选项时按显式标记过滤，并检查后续校验是否还会拒绝选中的候选。这与本题修复逻辑高度吻合，所以应记为强 answer-adjacent，而不是盲测。Agent 没有联网：web search、GitHub MCP 和任务网络均关闭，轨迹里也没有外部搜索工具。不过公开 issue 可能出现在模型训练资料中，无法排除预训练记忆；因此只能说这次没有主动上网抄答案，不能证明模型没有答案先验。

评分使用 source-only patch，因为 Agent 自己加的测试与官方 test patch 冲突；原始 patch 已保留但未评分。详情见[本次 Sigma 归档](../../../archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sigma-thinking-django-12325/run-result.md)。固定 issue-only 分数不变。


### Task 21

**django__django-12406 — RadioSelect 不应显示无效空选项**

问题：模型字段 `blank=False` 时，ModelForm 的 RadioSelect 应呈现未选择状态，不显示被勾选的空选项。

最新官方结果：fresh 重测 unresolved，F2P 0/3、P2P 161/165、0 evaluator errors。报告列出 `test_non_blank_foreign_key_with_radio`、`test_choices_radio_blank` 和 `test_clean_model_instance` 三项 F2P 失败；test output 显示最后一项通过，报告与输出仍有一项不一致。Raw patch 与官方 test patch 冲突，source-only patch 成功评分。

对照官方修复后，差异很具体：官方沿 `ForeignKey.formfield()` 把模型的 `blank` 传给 `ModelChoiceField`，再由字段决定是否保留 `empty_label`。Sigma 没有把模型元数据传到生成的表单字段，而是修改通用 `ChoiceWidget` 的选择行为；目标测试仍失败，普通 ModelForm 还出现 4 项 P2P 回归。Agent 用到 128 轮上限，runtime 保存了 diff，但最终验证未完成。

固定清单的原始 issue-only 运行中，Python Skill 未提供这条 Django 表单数据流指引；Sigma 改了通用 `ChoiceWidget`，没有传递模型 `blank`，导致 F2P 0/3 和 4 项 P2P 回归。运行后 Skill 加入了 `formfield()` 数据流检查。后来的 Pi 与 Sigma guided 重测均 official resolved（F2P 3/3、P2P 165/165），但这版 Skill 的导航提示与本题解题路径高度相关，不能计作盲测通过。详见 [合并任务档案中的 Pi/Sigma 对照记录](tasks/README.md#django__django-12406) 与 [Sigma 运行归档](../../../archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sigma-deepseek-thinking-django-12406/)。


判断与预算：P3；原 issue-only 运行 3.47M 输入 / 19.3k 输出 tokens，前次续跑 4.35M / 20.9k；两次 guided 重测资源用量分别见对照报告。

任务档案：[Task 21 页面](tasks/README.md#django__django-12406)

### Task 27

**sphinx-doc__sphinx-10435 — LaTeX 行内代码空格边界**

问题：生成的 TeX 不应因注释符和换行丢失代码或括号。

官方结果与失败原因：原始评分与有效 Skill 重测均 F2P 0/1、P2P 74/74；官方 gold 正控通过。候选改用 `\PYGZsp{}`，未满足 `%` 前代码紧邻、注释后换行再输出 `}}` 的 TeX 边界；Agent 自测还采用了与官方相反的结尾顺序。

复盘与下一步：这题不是环境出了问题，而是 TeX 的换行和 `%` 注释位置会改变输出含义。候选保留了 `%` 前的换行，又把闭合符放到了 `%` 注释覆盖的行上；Agent 自己的测试还按相反顺序检查结尾，所以没能发现和官方预期不一致。Sphinx Skill 已经提醒 `%` 会注释整行、要逐字符检查生成文本，而且在重测时确实加载了，但这条提醒没有改变 Agent 的选择。理论上可以换另一种 TeX 表达，前提是它的实际输出与目标行为等价并通过官方测试。再次重跑前应先把预期输出中的内容、`%` 和换行逐字核清；继续扩充同一类 Skill 提示，预计收益不大。

判断与预算：P3；有效运行 1.30M / 7.4k；若未来再次批准，≤0.8M / 8k，达到上限无进展即停。

任务档案：[Task 27 页面](tasks/README.md#sphinx-doc__sphinx-10435)

### Task 30

**sphinx-doc__sphinx-11510 — include 文件未触发 source-read**

问题：被 include 的 reStructuredText 内容应在解析前触发 source-read 事件。

官方结果与失败原因：最新 Skill 重测 source-only F2P 0/2、P2P 0/7；普通与嵌套 include 目标测试失败，7 项 Toctree 既有测试全失败。候选改写 `other.py` 中 Include 以外的 directive，属于明显范围扩张。

复盘与下一步：Agent 没有把改动限制在 Include 的读取流程里，而是同时改了同一个文件中的其他 directive；结果普通和嵌套 include 的目标测试都失败，Toctree 的既有测试也全部回归。Sphinx Skill 已提醒保留 Docutils 原有的读取和解析流程、区分不同 include 模式，并避免全局替换 I/O 行为；失败运行加载了这些内容，却没有按它们收窄补丁。因此目前更像是找错改动范围、没有守住调用路径，不能简单归为缺少某条 Skill 知识。若要再试，应先从测试确认 source-read 事件在读取链路的哪个时点触发，只改 Include 所需的路径，再跑 Toctree 邻近测试检查是否引入回归。当前没有一个有效的局部候选证明它做不到。

判断与预算：P3；最近两次 Skill 重测合计 2.68M / 24.8k；最新一次 0.84M / 11.0k。若未来有明确的新实现假设，再限 ≤0.8M / 8k。详见[本次摘要](tasks/README.md#sphinx-doc__sphinx-11510)。

任务档案：[Task 30 页面](tasks/README.md#sphinx-doc__sphinx-11510)

### Task 31

**sphinx-doc__sphinx-7590 — C++ 用户定义字面量解析**

问题：C++ 域应解析相邻数字与 UDL suffix。

官方结果与失败原因：历史 160 轮候选 source-only F2P 0/1、P2P 24/24；本轮 Sphinx Skill 加载后，首轮遇 runtime 消息排序 bug 的 400，修复后再跑 21 回合到输入 token 上限；诊断候选 `test_expressions` 仍在 `5_udl` 失败，0 evaluator errors。

复盘与下一步：离线比对候选、`5_udl` 失败输出和官方修复后，候选错误已定位：实现先消费 `_`，再调用同样要求以下划线开头的 `_parse_udl_suffix`，导致分隔符被要求两次；候选也把 numeric literal UDL 限制在下划线形式，未覆盖 `q_J`/`q_s`，而官方逻辑会先消费标准整数/浮点后缀，再解析当前位置的 suffix identifier，并包含 C 域 operator literal 处理。运行中还确有 Sigma 的消息排序 bug，修复后 continuation 不再 400，但仅继续 21 轮便到 token cap，Agent 未完成验证。此题因此有两个独立问题：Sigma 请求序列曾中断运行；修复后留下的候选仍有明确 lexer/parser 错误。加 Skill 或重试 provider 本身都不能修复这个候选。

判断与预算：P3；本轮累计 1.238M / 6.4k（缓存读取 1.043M）；原 cap 1.2M / 10k，超出约 38k 是单个响应记账延迟。

任务档案：[Task 31 页面](tasks/README.md#sphinx-doc__sphinx-7590)

### Task 32

**sphinx-doc__sphinx-7748 — autodoc 只读取重载 docstring 的首个签名**

问题：多个重载签名都应被提取并输出。

官方结果与失败原因：最新 source-only 官方 F2P 0/2、P2P 12/12；Agent 在 66 轮、1.65M 输入后因预算停止，未完成最终 diff review。

复盘与下一步：Agent 加载了关于 overload 和签名的 Sphinx Skill，但 66 轮后预算就停止了，两个目标测试都没通过，最终 diff 也没检查完。因此能确认当前补丁不够，却不能据此认定 Skill 完全没用或模型已经试尽所有办法。若继续分析，应沿着 docstring 提取、构造 `inspect.Signature`、autodoc 输出这几步，查清重载内容具体在哪一步丢失，并先用两个失败测试做定向验证，再检查普通单签名有没有受影响。考虑到运行已超过原定 token 预算，先看旧轨迹是否留下了明确假设；没有新线索时，不建议直接再开一次高成本长跑。

判断与预算：P3；本次 1.65M / 12.1k，超过 1.5M / 12k 软预算；后续若重审应先修预算监控。

任务档案：[Task 32 页面](tasks/README.md#sphinx-doc__sphinx-7748)

### Task 34

**sphinx-doc__sphinx-7985 — linkcheck 不检查本地链接**

问题：linkcheck 应报告失效的本地/站内链接。

官方结果与失败原因：原官方 source-only F2P 0/2、P2P 3/3；MiniMax Skill 重测未评分：累计 121 轮仍无 tracked source/test 改动，因此没有新的候选验收结果。

复盘与下一步：Agent 在第 80 轮左右提出了符合 Sphinx Skill 的思路：沿用现有 URI 分类和检查队列处理本地链接。但它之后没有把思路落实为源码修改或目标测试，所以这次重测没有新评分，也无法判断 Skill 能否带来正确实现。现有记录显示它卡在从分析转入动手阶段，而不是已经写出错误补丁。若要继续，应先选一个目标测试中的本地 URI 情况，确认路径和锚点怎样进入现有检查队列，再做一处小改动并验证 ignore 规则和支持的 scheme 没被破坏。除非出现这样的代码或测试进展，单纯续跑或再写几条 linkcheck 提示都不太有帮助。

判断与预算：P2；主 session 3.06M / 21.4k；另有32轮尝试 0.69M / 7.1k。归档摘要：[主批次](../../../archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sphinx-7985-minimax-skill-retest/sphinx-doc__sphinx-7985/result-summary.md)，[续接](../../../archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sphinx-7985-minimax-followup/sphinx-doc__sphinx-7985/result-summary.md)。专属环境清理命令被自动策略审查拒绝，尚不能确认已清理。

任务档案：[Task 34 页面](tasks/README.md#sphinx-doc__sphinx-7985)

### Task 35

**sphinx-doc__sphinx-8035 — private-members 不能指定成员**

问题：autodoc 应能只记录指定私有成员。

官方结果与失败原因：200 轮后 Agent incomplete，F2P 0/1 且一项 P2P 失败；200 轮 patch 与已评分的 160 轮 patch 字节一致，表明后 40 轮没有候选进展。

复盘与下一步：Agent 在前 160 轮形成的补丁，到 200 轮结束时仍完全没变，目标测试没通过，还有一项既有测试回归；继续加轮数并没有带来进展。现在的 Sphinx Skill 已要求检查选项解析、默认值和合并逻辑，并区分“没指定选项”“只开启全部私有成员”和“指定成员名称”这几种情况。它与这题很相关，但需要核对这些具体提醒是否在原运行之后才加入。若规则是新的，可以考虑一次严格限额的全新运行：先看失败断言，再确认选项从解析到成员过滤时具体是什么值，避免不同阶段各自维护一套状态。Agent 曾经写出补丁，所以目前更像是思路卡住或反复沿用同一实现，不能据此断定题目做不到。

判断与预算：P2；历史 5.06M / 34.8k；若重跑，≤1.5M / 12k，使用全新 issue-only session。

任务档案：[Task 35 页面](tasks/README.md#sphinx-doc__sphinx-8035)

### Task 36

**sphinx-doc__sphinx-8056 — NumPy docstring 多参数合并显示**

问题：x1, x2 等名称应各自成为参数项。

官方结果与失败原因：200 轮、约 5.54M 输入后仍 F2P 0/1；最终候选在下游 docfields 处理，NumPyDocstring 参数收集层仍将名称分组。早期源码截断风险已由 runtime 修复。

复盘与下一步：Agent 花了 200 轮、消耗约 554 万输入 token，最后仍把 `x1, x2` 当作一个参数组处理。复盘发现它改的是后面的 docfield 输出层，但这组名称在更早的 NumPy docstring 收集阶段就没有拆开。Sphinx Skill 现在已经提醒应在 parser 或 collector 把分组输入转成独立语义项，不过历史运行仍改错了层级；仅凭文字提示不能保证 Agent 会据此改对。继续重跑前，最好先从失败测试和参考改动确认分组最早出现的位置，再决定是否值得给它一次有严格预算的机会。考虑到此前成本很高，先离线看轨迹和补丁更合算，不建议盲目重跑。

判断与预算：P3；历史 5.54M / 31.7k；如后续重审，≤1.5M / 12k。

任务档案：[Task 36 页面](tasks/README.md#sphinx-doc__sphinx-8056)

### Task 40

**sphinx-doc__sphinx-8548 — 继承成员文档与 autodoc 默认成员范围**

问题：修复继承属性注释查找时，仍需保持 autodoc 默认成员集合，不应额外输出继承属性。

官方结果与失败原因：原 source-only 评分 unresolved：目标 F2P 1/1，但 P2P 4/5，test_instance_variable 回归。2026-11-07 Skill 更新后的续跑被用户叫停，未做官方 candidate 评分，不能替代原分数。

复盘与下一步：离线复盘确认原候选修复了“从父类找到属性注释”的目标行为，却把继承属性加入 autodoc 默认成员输出，造成 `test_instance_variable` P2P 回归；注释查找可以沿父类进行，但默认成员集合仍应保持原规则。Sphinx Skill 后来加入了保留默认成员范围的提醒。续跑中唯一失败的 `test_autodoc` 在本地复现为基线 docutils meta 节点问题，因此没有官方候选评分，不能覆盖原结果。用户已明确要求不再重跑，保留原始 unresolved 结论。

判断与预算：P0；原运行约 3.11M / 17.2k；中断续跑未结算。

任务档案：[Task 40 页面](tasks/README.md#sphinx-doc__sphinx-8548)

### Task 42

**sphinx-doc__sphinx-8638 — 同名实例变量被错误链接**

问题：无关的同名变量不应被自动链接。

官方结果与失败原因：F2P 0/1、P2P 33/34；目标失败且一项既有测试回归。

复盘与下一步：离线检查 source-only patch 后，失败点已明确：候选把对象解析改成 exact-name 优先并删除原有 classname/module scoped fallback；注释声称要按“most specific”查找，但实现没有实际生成和排序这些 scoped candidates。因此 `pending_xref` 目标仍失败，原有 `test_domain_py_find_obj` 也回归。这里不是缺少“检查 xref”的通用 Skill，而是实现与注释中的查找规则不一致。用户已明确要求不再迭代，因此不安排重跑；这不代表题目无法修复。

判断与预算：P0；0；遵从用户决定。

任务档案：[Task 42 页面](tasks/README.md#sphinx-doc__sphinx-8638)

### Task 44

**sphinx-doc__sphinx-9229 — 类型别名的多行文档字符串显示不一致**

问题：不同 type alias 的独立文档字符串应优先于默认的 alias-of 文本。

官方结果与失败原因：原 MiniMax M3 运行未评分：gold control 通过 1/1，但 Agent 到 128 轮没有 tracked Sphinx 源码改动或目标测试。2026-11-07 DeepSeek Flash thinking 辅助重跑确认两项 Skills 已加载；Agent 做了多个 `.scratch` 探针，但三次 Sphinx 构建各运行约 302 秒，仍未改生产代码。用户因耗时要求停止；candidate evaluator 未启动，仍无 F2P/P2P 评分，标为 `invalid/incomplete`。

复盘与下一步：旧运行中的 scratch 示例没有重现报告中的差异。第一次 thinking 辅助重跑重复构建耗时过长；更新 Skill 后，Agent 改用 issue 自带的复现，scratch 构建命令约 8 秒内完成，并追踪了注释解析、`GenericAliasMixin` 和 alias 输出路径。第 66 轮 checkpoint 后根据用户条件续跑至总计 128 轮（146 次工具调用），但始终没有生产代码改动或仓库目标测试。Skill 提醒降低了探针耗时并使探索更贴近 issue，仍未促成实现。按用户要求状态记为 `unresolved`；**候选未跑官方评分，F2P/P2P 无数据**，这不是 evaluator 给出的 unresolved 分数。Docker 检查时已无 #9229 专属容器或镜像；3 个 checkout 因桌面策略拒绝递归删除而保留，未尝试其他删除方式。详见[第一次 thinking 辅助重跑报告](../../../archive/benchmarks/swebench-verified-mini/runs/batch-20261107-deepseek-thinking-unscored-reruns/sphinx-doc__sphinx-9229/task-report.md)及[更新 Skill 重跑报告](../../../archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sphinx-9229-skill-retest/task-report.md)。

判断与预算：P3；原运行约 3.40M 输入，thinking 辅助运行被用户停止，另消耗约 1.64M 输入 / 57.6k 输出。当前没有官方 candidate 评分；若再做，应先解决 scratch 构建的长耗时和停止条件，并保留现有 Docker 环境。

任务档案：[Task 44 页面](tasks/README.md#sphinx-doc__sphinx-9229)

### Task 49

**sphinx-doc__sphinx-9461 — classmethod 与 property 组合成员未被记录**

问题：autodoc 应能发现并记录 class property。

官方结果与失败原因：累计 176 轮后 source-only F2P 0/3、P2P 59/59；raw patch 与官方 test patch 冲突，剔除测试改动后的诊断候选仍未通过目标，Agent incomplete。

复盘与下一步：离线对照 source-only 候选与官方修复后，差异已定位：Sigma 增加了沿类层级和 metaclass 查找 class property 的逻辑，也把找到的 property 交给 `PropertyDocumenter`，但没有把该成员标记为 classmethod 并输出官方测试要求的 classmethod 属性/节点。官方修复则让 `PropertyDocumenter` 识别 class property，并在 `import_object()` 中保留 descriptor 的原始 property。也就是说 Agent 找到了 descriptor 枚举问题，却没有完成“发现成员 → 保存 descriptor 语义 → 生成文档标记”的整条链。历史成本已达约 456 万输入 token，且运行因预算边界处理错误而 incomplete；这个明确的实现缺口不构成模型做不到的证明，也不支持盲目重跑。

判断与预算：P3；历史 4.56M / 33.0k；若之后有明确新假设，≤1.5M / 12k。

任务档案：[Task 49 页面](tasks/README.md#sphinx-doc__sphinx-9461)
