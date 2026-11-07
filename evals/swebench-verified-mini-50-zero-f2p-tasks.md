# SWE-bench Verified Mini：12 道当前 F2P 零通过题目

统计日期：2026-11-06（已纳入本日后续重测）
清单来源：[50 题评测计划](swebench-verified-mini-50-evaluation-plan.md)

## 统计口径

以下 12 题的候选补丁在官方评分记录中的目标测试 **F2P 通过数为 0**。这表示没有通过基准指定的目标测试，不代表 Agent 没有修改代码或没有任何局部进展。

完整 `resolved` 还要求所有 F2P 测试通过，并且原先通过的 P2P 测试没有回归。因此，部分 F2P 通过但有其他目标测试失败，或造成原有测试失败的题目，仍不属于完整 resolved。

| # | 任务 | 问题名称 | 问题简述 | 官方失败原因与重跑判断 | 当前 Skill 覆盖与预期帮助 | 优先级 | 单次重跑预算（输入 / 输出） |
|---:|---|---|---|---|---|---|---|
| 3 | django__django-11848 | parse_http_date 两位年份判断 | 两位年份应按 RFC 850 规则结合当前年份解释。 | F2P 0/2、P2P 43/43；实现读取系统时钟而非测试 mock 的时钟 seam。另有测试与 mock 耦合争议；已做 Skill 重测仍失败。 | Python Skill 明确要求找出测试替换的时钟对象/方法；已加载但 Agent 未遵守，新增同一提醒的收益低。 | P3 暂缓 | 历次累计 3.57M / 54.5k；若以后有新假设，≤1.0M / 10k。 |
| 16 | django__django-12273 | 重置子模型主键无效 | 多表继承中主键置空后保存应产生预期对象。 | 初次源码截断致测试无法执行；后续运行 F2P 0/2、P2P 8/27，出现继承回归。现有运行时有大幅缩文件保护，但语义缺口未解决。 | Python Skill 强调小范围修改和边界测试；运行时保护能拦截截断，不能证明保存语义已修复。已有跟进失败。 | P3 暂缓 | 已记录一次 837k / 10.4k；若以后重审，≤0.9M / 10k。 |
| 20 | django__django-12325 | 多个 OneToOneField 时父链接识别错误 | 显式 parent link 与其他 OneToOneField 并存时，声明顺序不应改变识别。 | 最近 Skill 重测 F2P 0/2、P2P 201/201；候选仍保留会拒绝未标记字段的旧校验。多次官方评分均未解决。 | Python Skill 新增了在候选集合中落实标记资格并审查旧校验的规则，但该规则是在最近评分后加入；可能有帮助，仍需新证据再重跑。 | P3 暂缓 | 最近单次 1.23M / 7.4k；若后续批准，≤1.3M / 12k。 |
| 27 | sphinx-doc__sphinx-10435 | LaTeX 行内代码空格边界 | 生成的 TeX 不应因注释符和换行丢失代码或括号。 | 原始评分与后续有效 Skill 重测均 F2P 0/1、P2P 74/74；无效 attempt 不计。有效重测已加载 Sphinx Skill，仍用了不同输出方式而失败。 | Sphinx Skill 有精确终止符、换行常量和百分号语义提醒；但已在一次有效 Skill 运行中加载，未见效果。 | P3 不优先 | 有效运行 1.30M / 7.4k；若未来再次批准，≤0.8M / 8k，达到上限无进展即停。 |
| 30 | sphinx-doc__sphinx-11510 | include 文件未触发 source-read | 被 include 的 reStructuredText 内容应在解析前触发 source-read 事件。 | 最新 Skill 重测 source-only F2P 0/2、P2P 0/7；普通与嵌套 include 目标测试都失败，7 项 Toctree 既有测试全失败。候选大范围改写 `other.py` 中 Include 以外的 directive，造成回归。 | Sphinx Skill 已新增保持原 parser/read 路径、区分 parsed/literal include、避免全局 I/O 替换的提醒且本次已加载；但 Agent 仍扩大改动范围。现有提示没有带来有效候选。 | P3 暂缓，不优先重跑 | 最近两次 Skill 重测合计 2.68M / 24.8k；最新一次 0.84M / 11.0k。若未来有明确的新实现假设，再限 ≤0.8M / 8k。详见[本次摘要](../../archive/benchmarks/swebench-verified-mini/runs/batch-20261107-sphinx-skill-retest-30/sphinx-doc__sphinx-11510/result-summary.md)。 |
| 31 | sphinx-doc__sphinx-7590 | C++ 用户定义字面量解析 | C++ 域应解析用户定义数字字面量。 | 续跑 160 轮仍 incomplete，官方 F2P 0/1、P2P 24/24；属于 lexer/parser 表达式边界。 | Sphinx Skill 现在强调 parser state、真实节点/对象形状，以及 syntax change 的 valid/invalid/overlap 邻近用例；有帮助但不是 C++ 专项。 | P2 条件重跑 | 历史 token 未汇总；单次上限 ≤1.2M / 10k。 |
| 32 | sphinx-doc__sphinx-7748 | autodoc 只读取重载 docstring 的首个签名 | 多个重载签名都应被提取并输出。 | 最新 source-only 官方 F2P 0/2、P2P 12/12，仍 unresolved；Agent 在 66 轮、1.65M 输入后因预算停止。 | Sphinx Skill 的 overload/signature guidance 已加载，但本次失败说明它没有让候选通过目标测试；继续重跑前需有新的实现假设或更有效的预算控制。 | P3 暂不优先 | 本次 1.65M / 12.1k，超过 1.5M / 12k 软预算；后续若重审应先修预算监控。 |
| 34 | sphinx-doc__sphinx-7985 | linkcheck 不检查本地链接 | linkcheck 应报告失效的本地/站内链接。 | 160 轮后 incomplete；官方 source-only F2P 0/2。Agent 另建 resolver 偏离现有 ignore/status 流；raw fixture 重叠不是根因。 | Sphinx Skill 后续补了本地/外部 URI 分类、相对路径基准、ignore pattern 和现有 queue/status 流；对失败路径直接相关。 | P2 条件重跑 | 历史 8.52M / 75.6k；仅在严格限额下重跑，≤2.0M / 15k。 |
| 35 | sphinx-doc__sphinx-8035 | private-members 不能指定成员 | autodoc 应能只记录指定私有成员。 | 200 轮后 incomplete，F2P 0/1 且一项 P2P 失败；200 轮 patch 与已评分的 160 轮 patch 字节相同。 | Sphinx Skill 后续补了 option_spec、默认值/合并兼容，以及 filter_members 所有权和 None/全部/指定三种状态；直接覆盖原错误设计。 | P2 条件重跑 | 历史 5.06M / 34.8k；若重跑，≤1.5M / 12k，使用全新 issue-only session。 |
| 36 | sphinx-doc__sphinx-8056 | NumPy docstring 多参数合并显示 | x1, x2 等名称应各自成为参数项。 | 200 轮、约 5.54M 输入后仍 F2P 0/1；初期源码被截断的运行时风险已修，但最终错误在 parser/field collector 仍把名称分组。 | Sphinx Skill 现在要求在 parser/collector 产生语义项时拆分分组输入；运行时也新增文件缩小保护。直接相关，但已有长跑未解决且开销很高。 | P3 暂缓 | 历史 5.54M / 31.7k；如后续重审，≤1.5M / 12k。 |
| 42 | sphinx-doc__sphinx-8638 | 同名实例变量被错误链接 | 无关的同名变量不应被自动链接。 | F2P 0/1、P2P 33/34；目标失败且一项既有测试回归。 | Sphinx Skill 涉及 xref/domain 处理，但用户已明确要求不再迭代。 | P0 不重跑 | 0；遵从用户决定。 |
| 49 | sphinx-doc__sphinx-9461 | classmethod 与 property 组合成员未被记录 | autodoc 应能发现并记录 class property。 | 续跑累计 176 轮后 source-only 仍 F2P 0/3、P2P 59/59，Agent incomplete。 | Sphinx Skill 覆盖 autodoc/member discovery 通用流程，但没有证据表明它已解决 descriptor/class-property 的识别路径。 | P3 暂缓 | 历史 4.56M / 33.0k；若之后有明确新假设，≤1.5M / 12k。 |

预算说明：预算是每次新运行的累计输入/输出软上限；输入包含缓存读取 token。Sigma 当前没有累计 token 硬停止器，运行时需在约 25%、50%、75% 检查点记录用量与实现/测试进展；达到软上限或连续检查点无进展就停止，不靠延长轮数消耗完预算。P1 可优先安排；P2 需前序候选结果或剩余预算允许；P3 暂缓；P0 不运行。所有 P1/P2 均从全新 issue-only session 开始，默认 128 轮封顶。
## 后续重测已解决

- sphinx-doc__sphinx-10673：Sphinx Skill 重测 source-only 官方 resolved，F2P 1/1、P2P 9/9；Agent run 本身在预算处 incomplete。详见 [重测摘要](../../archive/benchmarks/swebench-verified-mini/runs/batch-20261106-sphinx-skill-retests-32-30-29/sphinx-doc__sphinx-10673/result-summary.md)。
- django__django-12308：Python Skill 独立重测 resolved，F2P 2/2、P2P 20/20；已从当前零通过清单移除。详见[重测摘要](../../archive/benchmarks/swebench-verified-mini/runs/batch-20261106-python-skill-followup-reruns/django__django-12308/result-summary.md)。

## 有目标测试进展但未完整 resolved

- `django__django-12406`：F2P 1/3，P2P 165/165。
- `sphinx-doc__sphinx-8548`：F2P 1/1，但 P2P 4/5；目标测试通过，仍有一项原有测试失败。

因此按最新官方评分统计，34 题完整 resolved，2 题有部分目标测试进展但未完整 resolved，12 题当前 F2P 为零，另有 2 题未评分。未评分题为 django__django-11885 和 sphinx-doc__sphinx-9229。
