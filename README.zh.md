# babysit

[English](README.md) | [Tiếng Việt](README.vi.md) | 中文 | [日本語](README.ja.md) | [한국어](README.ko.md)

**递给它一行话。你不在的时候，它 plan、写码、review、跑 QA。你只看一个 branch。**

```
/bbs:autopilot add a settings page with dark mode toggle
```

Codex 用户以 `$bbs:autopilot` 调用同一个 skill；README 一律使用
Claude Code 的 `/bbs:` 写法，除非某个命令是 Codex 专属。

每个 ticket 约 40 分钟的自主工作，而且即便任何单个 agent session 都装不下全部内容，它也能做完。你 review 那个 branch，满意了再自己开 PR。

**从 `autopilot` 开始。** 一个命令就是整个产品，任何终端、任何 repo 布局都能跑，而且每个并行 worker 跑的正是它 —— 所以你在这里学到的东西都不是一次性的。

**然后，当活儿大过一个 ticket —— 用 `foreman`：**

```
/bbs:foreman rebuild the novel request flow across web, API, and migrations
```

Foreman 把项目拆成依赖图，为每个 ticket 建 branch 和 worktree，并通过 Orca 原生的 Run/Task/Dispatch 生命周期监督每一个 autopilot assistant。它在 build 之前 review plan，协调 shared surface 与 integration QA，从磁盘加 Orca state 中恢复，并按依赖顺序应用 repo 的 finish 策略。它需要 [Orca](https://www.onorca.dev)；只有一个串行 ticket 时直接用 autopilot 就行。

*babysit 就是当你不需要保姆时所做的事。* 它偏爱 agent 能独自决定并验证的决策，而不是需要人介入的决策 —— 为定时运行、编排流水线，以及任何你想甩手离开的事情而生。

## 最省事的试法

如果你是小团队里的主程，这就是整个闭环 —— 它干苦活，你把 gate。像读一段程序那样从头读到尾：

```bash
/bbs:setup-project                        # 每个 repo 一次 —— profile + QA 默认值
/bbs:autopilot "add dark-mode toggle"     # 任何改动：plan → 写码 → review → QA
#   → 读 tickets/<id>/plan.md，然后粘贴打印出来的 /goal block 并走人
#   → autopilot 写代码、review、跑 QA，在你的 branch 上 commit
#   → 你 review 证据，然后：
bbs ticket serve bs-ab123                 # 仅当该 ticket 跑在自己的 worktree 里（foreman / --mode=worktree）
#   → 在浏览器里看它跑起来；向它的 session 提改动，然后重跑 serve
/bbs:create-pr                            # PR 由你来开 —— autopilot 从不自己开
```

这个 ticket 的代码就在你此刻站着的 branch 上，所以 dev server 一重载，它就在你眼前。`bbs ticket serve` 是给另一种形态用的 —— 一个跑在自己 worktree 里的 ticket（`bbs ticket ensure --mode=worktree`，或一个 `/bbs:foreman` 批次），它的代码不在这个 checkout 里。

一步一步：

- **`/bbs:setup-project`** 一次 —— 教会 autopilot 你的 branch 命名 + QA 默认值，于是下游的一切都是确定的。
- **`/bbs:autopilot "<one-line requirement>"`** 用于任何多步工作。它把 checkpoint 落到磁盘（能扛住崩溃和 context 压缩），然后 plan → implement → review → QA，最后停在 PR checkpoint，所以任何东西 merge 之前你都先 review 过。

**人来把关的 checkpoint —— 你继续掌权的地方。** Autopilot 只在真正属于你的时刻暂停；用 flag 挑停在哪一个：

- `--stop-after=plan` —— 在任何代码被写出来之前，先批准方案。
- `--planner <model>` / `--planner-effort <effort>` —— 把 plan 与 UI
  prototype 的创建委派给那个原生 model/profile 和推理 effort。省掉
  任一取值，就让 autopilot 按任务难度以及当前 harness 实际
  宣称拥有的 model 来选。在 OMP 上，`default` 与 `slow` 选中
  它配置好的 model role；常规工作默认 `slow`。
- *默认* —— 停在 QA 就绪，由你 review 证据。
- **`/bbs:create-pr`** —— 由你调用；autopilot 从不自己开 PR。

**按需加载：**

- **`/bbs:review-pr`**（又名 `/code-review`）—— merge 前的一道 gate，因为小团队没有第二个 reviewer。你的安全网。
- **`/bbs:foreman`** —— 自主项目流程：拆分一个大 requirement，在 Orca 里调度有依赖的 ticket，掌管它们的 worktree，并把项目级的 QA 与收尾关。单个串行 ticket 用它属于杀鸡用牛刀。

## 为什么它能行

- **它一定能做完。** `/bbs:autopilot` 是一个 **goal proxy**：init 播下持久的 state —— ticket、requirement、plan、checkpoint —— 然后把活儿交给 [`/goal`](#3-运行它)，也就是 agent 的持久 goal 模式，直到 QA 与 review 的 verdict 都被持久化。循环之内，model 带着完整 context 自由发挥，就像你直接提要求时那样；磁盘上的 checkpoint 让一个全新的 session 能从上一个停下的地方接着跑。
- **它不会卡死。** 每个决策都走 [Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md)。agent 自己决定并记录；如果确实需要人，它就给 ticket 写一个 `NEEDS_CONTEXT` block，而不是干等弹窗。
- **它自己验证自己。** QA 是默认 autopilot 循环的一部分。PASS 要求本地有跑起来的目标，或者一个有名字的 blocker，外加非 happy-path 的 case。没有“能编译，发吧”这种事。
- **它可审计。** JSONL telemetry 写到 `~/.babysit/analytics/`，外加 `[WORK]` checkpoint 注释。事后读带子 —— 没人现场盯着时，这就是主要的反馈通道。

## 五种 archetype

当工程、产品、设计与数据科学融成同一种
product-builder，有用的工作单元就不再是职位 —— 而是工作此刻需要的
*archetype*。Babysit 就是产品构建团队：
它把产品团队的五种 archetype 映射到 skill 与 autopilot
workflow 上，于是一次运行可以扮演任务所需的任何队友。

一个人横跨 2–3 种 archetype；一次 babysit 运行也是。按**工作的形状**来选，
而不是按文件的“类型”。每个 archetype 恰好有**一个 autopilot workflow**。

| Archetype | 使命 | 什么时候选它 | Workflow |
|-----------|---------|-------------------|----------|
| **Prototyper** | 快速捣出全新的想法；大多数不会 ship。快速学到一件事。 | 还没有任何产物，只有个直觉 —— 在投入构建前先验证。 | `prototyper` |
| **Builder** | 把 prototype/想法变成生产级的产品与基础设施。 | 已有验证过的想法或被接受的 plan。新功能工作的默认选项。 | `builder` |
| **Sweeper** | 清理 UI、简化代码与系统、下线功能、做优化。 | 代码背着它不需要的重量 —— 死代码、重复、过度抽象。做减法；行为不得改变。 | `sweeper` |
| **Grower** | 在一个已上线的产品上迭代，改善 product-market fit。 | 产品能 ship，但漏斗不给力。先度量，再跑一个可逆实验。 | `grower` |
| **Maintainer** | 让成熟系统在规模下安全、可靠、快、且高效。 | 成熟系统正承受生产/规模压力 —— 负载、安全、可靠性、成本。 | `maintainer` |

**Sweeper vs Maintainer** —— 最容易搞混的一对，因为两者都碰
性能。Sweeper 优化的是 *codebase*（减复杂度，行为保持 byte-identical；
性能顺带就来了），由累积的 cruft 触发。Maintainer 优化的是*生产中的系统*
（在真实负载下撑住；caching/indexing/data-model 改动可能改变时序），由
规模、安全或成本触发。“一个稳定、被广泛使用的功能”是 Maintainer 的
触发条件；如果它同时还需要结构性清理，就把 Sweeper 当作一次独立的
保行为 pass 来跑。

**每个 archetype 都守的 invariant** —— 它们只在*使命与成功标准*
上不同，在严格程度上从不不同：决策走
[Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md)
（taste 决策留档，绝不悄悄猜）；在“done”之前先自我验证；
爆炸半径有界（不 force-push、不丢数据、没有持久授权就不发外部消息）；
大声且就地失败（`BLOCKED`/`NEEDS_CONTEXT` 好过一个错误假设）。

细节，以及每个 archetype 组合了哪些 skill：
[`.claude/skills/references/archetypes.md`](.claude/skills/references/archetypes.md)。

## 快速开始

三步。全局装一次，每个 repo 配置一次，然后运行。

### 1. 安装 plugin

**直接从 GitHub 装 —— 什么都不会落到你的工作区。** 先装 CLI，
再把同一个 marketplace 注册给你的编码 agent：

```bash
brew install lohi-ai/babysit/bbs        # the CLI —— 必需，见下文

# Claude Code
claude plugin marketplace add lohi-ai/babysit
claude plugin install bbs@babysit

# Codex CLI
codex plugin marketplace add lohi-ai/babysit
codex plugin add bbs@babysit
```

重启 agent。Claude Code 暴露 `/bbs:autopilot`；Codex 暴露
`$bbs:autopilot`。

用 `bbs update` 升级 CLI 和已安装的每一个 agent plugin：

```bash
bbs update
```

**`brew install bbs` 不是可选项。** `bin/bbs` 是构建产物，没有进
commit，所以从 GitHub 装的 plugin 不带任何编译好的二进制。`bbs` 不在你的 `PATH` 上时，
push/PR gate 读不到 ticket 的 verdict，而且它
fail closed —— 每一次 `git push` 都被拒。Linux 用户改用
tarball：[docs/install.md](docs/install.md)。

<details>
<summary><b>或者从 checkout 装</b> —— 如果你想读或改 babysit 本身</summary>

checkout 是唯一一种让你的 skill 改动无需发布就生效的形态。`setup-skills` 构建二进制并把一切接好：

```bash
git clone https://github.com/lohi-ai/babysit.git ~/src/babysit
cd ~/src/babysit
./bin/setup-skills --full
```

然后在任一 agent 里注册这个 checkout：

```
# Claude Code
/plugin marketplace add ~/src/babysit
/plugin install bbs@babysit

# Codex CLI（从 shell 运行）
codex plugin marketplace add ~/src/babysit
codex plugin add bbs@babysit
```

这样 `bbs` 就通过 `~/.local/bin/bbs` → 你的 checkout 出现在 `PATH` 上，所以你
不需要再装 Homebrew 那份。用 `git pull && ./bin/setup-skills` 升级。

注意 marketplace plugin 是*复制*进 agent 的 cache 的
（`~/.claude/plugins/cache/` 或 `~/.codex/plugins/cache/`）。`~/.claude/skills/<name>/`
下的 Claude Code 目录是**就地**加载的 —— 正是这种
形态让工作区里的改动立刻生效。不要同时装 Claude 的两种形态：
已安装的 marketplace plugin 会在重名时胜出。

</details>

要求：支持 plugin 的 Claude Code 或 Codex CLI，外加 Git。

推荐但不必一开始就装：**[Orca](https://www.onorca.dev)**，一个用来并排跑编码 agent 的 ADE。`/bbs:autopilot` 以及其他一切在任何终端里都能跑 —— Orca 只是 **`foreman` 的硬依赖**，foreman 没有别的后端：它给每个并行 worker 自己的终端 tab、编辑器里的 diff，以及 Orca 浏览器里跑着的 app。Orca 缺失时 `foreman` 会带着安装提示快速失败。

#### `bbs` CLI 是什么

一个 Go 二进制，所有子命令都以 `bbs <sub>` 调用 —— `ticket`、`config`、
`secrets`、`upgrade`、`design`、`dashboard`、`autopilot`、`foreman`。生产代码里
不再有 bash。
formula 还会附带两个 argv0 别名，`bbs-config` 和 `bbs-env` —— 只有
这两个，所以 skill 一律使用空格形式。

它是 babysit 里被 skill 替你调用的那一半：ticket 身份、
verdict、git-flow 动作、telemetry。它**不**包含 skill 包 ——
skill、workflow，以及 DESIGN.md/CSV 数据都来自 plugin。这就是为什么
安装是两条命令，也是为什么缺了任何一半都跑不起来。

完整平台矩阵和 Linux tarball：[docs/install.md](docs/install.md)。

### 2. 配置你的项目

在任何一个你想让 autopilot 出货的 repo 里：

```
/bbs:setup-project
```

向导写出最小可用的 `.babysit/` 配置：`git-flow.yaml` 含 `profile` 与 `base_branch`，`qa.yaml` 含 `url`、`start`、`check`、`flows`。重复运行是幂等的 —— 在已配置的 repo 上，它就是切换 profile 的方式。

#### Git flow：选一个 profile

只有一个旋钮。向导只问一个问题 —— **在这个 repo 里，犯错的代价是什么？** —— 因为 git flow 真正回答的是这个，而不是团队规模或你盯得多紧。你的回答就是 `profile:`，其余一切都由它推导：

| | `pet` | `startup` | `enterprise` |
|---|---|---|---|
| 谁 | 单人，业余项目 | 单人自由职业 / 小团队 | 团队，企业级 codebase |
| 优先级 | 现在就出货 | 发布速度 > 代码质量 | 代码质量 > 发布速度 |
| ticket 的活落在哪里 | 你站着的 branch —— 不切 | 你站着的 branch —— 不切 | 你站着的 branch —— 不切 |
| review 在哪里发生 | 哪里都不 —— push 就是发布 | **本地**，在浏览器里，作者自己 merge | **在 GitHub 上**，别人来 merge |
| QA 严格度 | `smoke` —— 3–5 个 case | `standard` —— 5–10 | `strict` —— 8–12 |
| `review-pr` 力度 | `low` | `medium` | `high` |

每个 profile 也对*你*有要求 —— ticket 开始时你该在哪个 branch 上，以及你的本地 base 是干什么用的。这份约定，以及在各 profile 下如何并行跑 ticket，都在 **[docs/profiles.md](docs/profiles.md)**。

**你从未配置过的 repo 会解析为 `pet`** —— 不切 branch、没有 review 场所，活儿搭在你正站着的任何 branch 上，跟裸跑 git 一样。仪式感是 repo 自己要求来的，不是白送的。

**第一次来，或者拿不准？选 `startup`。** 在你真正在乎的 repo 上，这是安全的起点：standard 的 QA 严格度，而且没有你开的 PR，任何东西都到不了 remote。之后换 profile 只需改一行。

严格度伸缩的是**广度，不是门槛**。`PASS` 在三种 profile 下含义完全相同：每一个适用的 rubric 维度都到 B 或更好，并且针对最终代码做过一次全新的端到端运行。业余项目跑的 case 更少 —— 但它从不跑零个，也从不放过一个 C 级维度。

手动设置 `mode:`、`land:` 或 `push:` 会覆盖 profile 的预设。那是逃生口，不是常规形态 —— 手写出来的旋钮就不再跟随它的 profile 了。

#### 并行 ticket 是按需请求的，从不配置

**没有任何 profile 会切 branch，或把你挪进 worktree。** 在三种 profile 下你都在自己站着的 branch 上工作，跟没有 babysit 时完全一样 —— profile 决定你的 review 场所和 QA 严格度，跟活儿住在哪里无关。这是刻意的：一个会悄悄搬走你工作的工具，是你管不住的工具。

隔离是按 run 请求的，在你真的想要它的时候：

- `/bbs:foreman` —— 一个批次：不管配置怎么写，foreman 在任何 repo 里都为每个 ticket 建一个 worktree。
- `bbs ticket ensure --slug-hint <slug> --mode=worktree` —— 让这一个 ticket 待在自己的 worktree 里；然后在里面跑 autopilot。
- `bbs ticket ensure --slug-hint <slug> --mode=branch` —— 就地切出 `feat/<id>_<slug>`。

**worktree 是有代价的，所以搞清楚你买了什么。** 内循环不再是 0 步：因为代码住在 worktree 里，而 dev server 服务的是主 checkout，每次测试迭代都是先 commit 再加 `bbs ticket surface compose`，而不是改完刷新。它换来的是可分离的 ticket —— 你可以单独 review 一个、丢掉坏的那个，让每个都作为自己的干净 PR 落地。想让每次都是这种形态的 repo 可以手写 `mode: worktree` + `land: local`；没有东西会替你写。

在 worktree 与共享 surface 之间搬运活儿的那些命令 —— `bbs ticket surface <acquire|compose|revert|release|status>`，加上人类层 `board`、`serve`、`/bbs:fix-pr` —— 都在 [并行处理 ticket (worktree 模式)](#并行处理-ticket-worktree-模式) 里。细节：[`references/git-flow.md`](.claude/skills/references/git-flow.md) 和 [`references/worktrees.md`](.claude/skills/references/worktrees.md)。

### 3. 运行它

**从这里开始 —— `autopilot`，单 ticket 流程：**

```
/bbs:autopilot "add a settings page with dark mode toggle"
```

Autopilot 初始化 ticket —— requirement、plan —— 然后停下，并把一个 `/goal` block 作为它的**最后一条消息**打印出来。那个 block 就是你接下来唯一要做的事：**复制它，粘回同一个 agent，然后走人。** goal session 随后写代码、review、跑 QA，并在你所在的 branch 上 commit 这些工作 —— autopilot 从不切 branch、从不 push、从不开 PR。review 之后由你自己开 PR。传 `--planner gpt-5.6-sol --planner-effort high` 可以指定创建 plan 与 UI prototype 的原生 model；省掉这些 flag，autopilot 就会按任务难度和当前 harness 实际宣称的能力来选。

> **handoff 长这样** —— autopilot 最后给一段白话前言，然后是你要复制的那段 block：
>
> ```
> bs-ab123 已就绪。粘贴之前，先 review 将要构建的东西：
>   plan:      tickets/bs-ab123/plan.md
>   prototype: tickets/bs-ab123/prototype.html
> 如果设计不对，现在就改方向 —— 否则你离完成只差一次粘贴。
>
> 👉 复制下面这个 block，粘进你的 agent 让它开始构建：
>
> /goal bs-ab123 is done: work committed locally, qa verdict PASS/FIXED persisted
> via bbs ticket set-verdict, review-pr verdict persisted, handoff note written — or a
> NEEDS_CONTEXT / BLOCKED status block printed verbatim.
> Work it: /bbs:autopilot builder bs-ab123
> ```

#### 为什么 `/goal` 拿住了这活儿

`/goal <condition>` 启动受支持 agent 的持久 goal 模式：model 带着完整 context 自由发挥 —— 没有 step 仪式 —— 一直跑到条件成立。这就是为什么这一步是*粘贴 `/goal` block*，而不是“跑一条命令”：粘贴才等于给 goal 上膛。Autopilot 打印的 block 已经编码了 babysit 的 gate 和逃生条款。

逃生条款意味着循环会在升级时终止，而不是对着缺失的输入空转。中途想撤：`/goal clear`、`Ctrl-C`，或者 touch `~/.babysit/projects/<slug>/tickets/<ticket>/STOP`。

没有 `/goal` 时，重新调用 `/bbs:autopilot bs-ab123` 依然会从 checkpoint 恢复 —— 只是每次跨 session 边界都得你手动推它一把。

#### 进阶 —— `foreman`，自主项目编排器

一旦一个 requirement 横跨多个 ticket，`foreman` 就接管整个项目。每个 worker 跑的仍是你已经熟悉的 autopilot，但 checkout 由 foreman 供给，DAG 也由它协调：

```
/bbs:foreman <large project requirement>  # 拆分、排期、验证、收尾
/bbs:foreman                              # 从 ticket + Orca state 附着/恢复
```

Foreman 创建一个父项目和有边界的子 ticket，记录它们的依赖边，并通过 Orca 编排启动受监督的 worker。每个子 ticket 先拿到一个只出 plan 的 Dispatch，再过一个自主 design gate，然后是一个 build/QA Dispatch。每个 worker 都跑在该 ticket 难度应得的 model 上 —— 从 autopilot 的 planner 也在用的共享 [model-routing table](.claude/skills/references/model-routing.md) 里读取 `simple` / `normal` / `critical` 档位，并把解析出的 model 记录在子 ticket 上。几乎每个 ticket 都跑在常规档（`gpt-5.6-sol` / `opus` / `@default`）；昂贵的高档（`gpt-6-astra`、Fable 5.1）需要触及表里的升级触发条件，所以一个批次没法悄悄爬上去。Foreman 从磁盘校验 verdict，串行化共享的测试 surface，在 ticket 互相影响时跑组合式 integration QA，并按依赖顺序把 repo 的 `finish:` 策略应用到每个够格的子 ticket —— `land` 把它 merge 进 base、`pr` 开它的 PR、`review` 放走已定局的 worker —— 所以做完的 ticket 从不等整个项目；某个子 ticket 若被尚未通过的 integration gate 覆盖，它的 land 会一直扣着直到那个 gate 通过。项目 DAG 随它的状态和 dispatch 回复一起走，`bbs ticket dag <parent> --mermaid` 可以按需打印它。Orca 消息和 Dispatch id 取代了 pane 文本轮询，一个自动启动的 watcher 会按配置的间隔轻推卡住的协调者并再次询问它的状态，所以重启的 session 没有对话记忆也能接着跑。

**一个硬前置条件，这也是它为什么是第二个要学的东西：** 启用 orchestration 的 [Orca](https://www.onorca.dev)。Foreman 加载 Orca 已安装、版本匹配的 orchestration 指南，没有那个 runtime 就快速失败，并且无论 profile 如何都为每个子 ticket 建 worktree。你的 profile 仍然决定严格度与收尾策略。只有一个串行 ticket 时，它相比 `/bbs:autopilot` 不带来任何额外好处。

## 怎么用它

Babysit 是一条用来交付改动的流水线。你在一头丢进一个想法，在另一头拿起一个随时可 review 的 branch。这条线只在你真正能加价值的四个时刻暂停；中间的一切自己发生。

### 它暂停的四个地方

1. **“这是该做的东西吗？”** —— `requirement.md` 就绪。你读，然后接受。
2. **“这是正确的做法吗？”** —— `plan.md` 就绪。你读，微调，接受。
3. **“它真的能跑吗？”** —— 代码写完、review 过、QA 检查过、commit 了。
4. **“这该变成一个 PR 吗？”** —— 你 review handoff 并运行 `/bbs:create-pr`；reviewer 的意见来了之后，`/bbs:fix-pr` 逐条处理。

### 挑它停在哪里

| 停在哪 | 怎么做 |
|---------|-----|
| pause 1 —— `requirement.md` 就绪 | `/bbs:autopilot "<idea>" --stop-after=requirement` |
| pause 2 —— `plan.md` 就绪 | `/bbs:autopilot "<idea>" --stop-after=plan` |
| pause 3 —— 过了 QA 的 branch 就绪 | `/bbs:autopilot "<idea>"` *（端到端，默认）* |
| pause 4 —— PR handoff | 人工 review 之后运行 `/bbs:create-pr` |

一个阶段跑完，ticket 会拿到一行 `Next:` —— 字面上就是下一步做什么。重新调用 `/bbs:autopilot bs-<id>` 总会从探测到的 state 里挑出正确的下一个阶段，所以你从来不必记该调哪个 workflow。

### 三种输入形态

```
/bbs:autopilot "<one-line idea>"     # 新功能 —— 创建 ticket，端到端跑完
/bbs:autopilot bs-ab123              # 已存在的 ticket —— 按 state 路由到下一阶段
/bbs:autopilot                       # 恢复 —— 从解析出的 ticket 的 checkpoint 接着跑
```

这就是全部表面。三个 flag 扩展它 —— `--stop-after=requirement|plan` 让它在更早的 checkpoint 停下，`--planner <model>` / `--planner-effort <effort>` 用来路由 plan/prototype 的创建。Autopilot 永远在你启动它的那个 checkout 上工作；隔离靠 `bbs ticket ensure --mode=…` 或 `/bbs:foreman`，从来不是某个 autopilot flag。动词 token 不存在。

### 并行处理 ticket (worktree 模式)

**这就是一个 foreman 批次（或手动的 `bbs ticket ensure --mode=worktree`）给你的形态** —— 这样一次运行交到你手上的那些命令。`/bbs:foreman` 会为整个批次驱动同一套东西 —— dispatch、design gate、verdict 检查、组合 surface —— 但你在这里干活并不需要它。

每个 repo 一个重 checkout 跑 dev server；每个 ticket 住在自己轻量的 worktree 里。这让一切都并行，*除了*有人需要真的看到某个 ticket 跑起来的那一刻 —— 而那一刻由三条命令负责：
```bash
bbs ticket board            # 一眼看全部 ticket：状态、verdict、活着的 session、PR、谁占着 surface
bbs ticket serve bs-ab123   # 把这个 ticket 放到跑着的 dev server 上，供人工 review
bbs ticket serve            # 不带参数：把所有已完成的 ticket（qa + review DONE）组合到 server 上
/bbs:fix-pr                 # reviewer 意见到达后：拉取未解决的 thread，修、回复、解决
```

**review 循环。** 在浏览器里 review 跑着的功能是最长的必做步骤，所以 babysit 让它成为最便宜的重复动作：

1. 一个 ticket 到达 pause 3 —— 它 handoff 里 `Next:` 那行把确切的命令递到你手上：`bbs ticket serve bs-ab123`。
2. `serve` 把测试 surface 占住 4 小时（agent 的 QA 会礼貌地排在你后面），并把跑着的 server 切到 base + 恰好这一个 ticket —— 在这个 repo **以及**当 ticket 横跨两端时的 FE/BE 兄弟 repo 里都是如此。
3. 在浏览器里 review。向这个 ticket 的 session 提改动；它在自己的 worktree 里 commit；重跑 `serve`（可重入 —— 刷新占用、重切 surface）并刷新浏览器。满意为止。
   在 Orca 下整个循环可以装进一个 worktree：`orca tab create --url <qa url>` 把跑着的 app 放进内置浏览器，`orca file open-changed --mode diff --worktree path:<ticket-worktree>` 在它旁边打开该 ticket 的 diff。
4. 通过 → `bbs ticket serve --release`，然后每个 repo 跑 `/bbs:create-pr`。之后有 reviewer 意见 → `/bbs:fix-pr`。
5. `bbs ticket board --pr` 标出已 merge 的 PR 并打印确切的清理命令（`surface revert`、`set-status done`）。

**一个 ticket，两个 repo**（一个横跨前端 + 后端的 feature）：`/bbs:setup-project` 把兄弟 repo 记录一次；autopilot 的 builder 会自己跨过去 —— 创建关联的兄弟 ticket，两边都实现并 QA —— 而 `serve` 用一条命令把这一对一起摆到你面前。与此同时，其他 ticket 的 session 继续在各自的 worktree 里实现和 review；`board` 显示谁占着 surface、占了多久。完整配方：[`references/worktrees.md` § Attended parallel review](.claude/skills/references/worktrees.md)。

## 深入一点

- **路由内部与调试** —— init 播下什么、`/goal` 循环如何从 checkpoint 恢复、每个 gate 归哪个 step skill 管：[`.claude/skills/autopilot/SKILL.md`](.claude/skills/autopilot/SKILL.md)。想看不运行的情况下一次运行会按什么 state 路由：`bbs autopilot explain`（加 `--details` 看 workflow 前置条件矩阵）。
- **Profile** —— [`docs/profiles.md`](docs/profiles.md)：每个 profile 对你的 base branch 有什么要求，以及在各 profile 下如何并行跑 ticket。
- **配置 schema** —— [`.claude/skills/references/git-flow.md`](.claude/skills/references/git-flow.md) 和 [`docs/qa-config.md`](docs/qa-config.md)，用于手写 `.babysit/`。

## Skill 索引

`/bbs:autopilot` 把下面的 skill 组合成完整的 workflow。只想要其中一块时，直接调那个 —— 以下是热门：

| 我想要…… | Skill |
|------------|-------|
| 用一行想法端到端交付一个 feature | `/bbs:autopilot "<idea>"` |
| 完成一个由多个 ticket 或 feature 组成的大项目 | `/bbs:foreman "<project>"` |
| 在投入构建前对一个想法做压力测试 | `/bbs:office-hours` |
| 在现有 UI 体系内设计一个 feature | `/bbs:design-ui` |
| 把一个 requirement 变成 `plan.md`（不写代码） | `/bbs:plan-draft` |
| 从已接受的 plan 开始构建 | `/bbs:implement` |
| 改进营销文案或转化 | `/bbs:copy-rewrite`、`/bbs:conversion-fix` |
| 提出增长实验或短视频脚本 | `/bbs:growth-experiment`、`/bbs:social-content` |
| 在浏览器里检查一个 URL 或前端流程 | `/bbs:browse` |
| 跑完整的浏览器测试/修复循环 | `/bbs:qa` |
| 落地前 review 一个 branch | `/bbs:review-pr` |
| 定位一个 bug 的根因 | `/bbs:investigate` |
| 为 autopilot 配置这个 repo | `/bbs:setup-project` |
| 创建一个可 review 的 pull request | `/bbs:create-pr` |
| 逐条处理 PR review 意见（修、回复、解决） | `/bbs:fix-pr` |

完整 skill 表（含 autonomous-ready / interactive-only 分类）在 [`docs/skills.md`](docs/skills.md)。

## 配套 CLI

一切都在这一个二进制里，以 `bbs <sub>` 调用 —— `bbs autopilot`（runner）、`bbs ticket env`（身份解析器：`BABYSIT_TICKET` → manifest → branch），外加 env、config、db 快照和升级检查的辅助命令。`brew install lohi-ai/babysit/bbs` 把它放进你的 `PATH`；从 checkout 装时，`setup-skills` 构建它并改为软链 `~/.local/bin/bbs`，另外为旧调用者在 `~/.claude/` 下放入 `bbs-*` argv0 别名。完整表格与用途见 [`docs/companion-cli.md`](docs/companion-cli.md)。对其中任何一个运行 `bbs <sub> --help` 看用法。

## 运维

日常配置（`bbs config`）、telemetry（JSONL 写到 `~/.babysit/analytics/`，默认只留在本地），以及更新处理（`bbs update check` + `bbs update`）都在 [`docs/operations.md`](docs/operations.md) 里。

**升级。** 刷新 CLI 和你所用 agent 的 plugin 副本，然后重启那个 agent：

```bash
bbs update
```

babysit 由两个各归不同工具管的半边组成 —— brew 的 CLI 和 agent plugin。`bbs update` 驱动 CLI 以及已安装的 Claude Code 与 Codex plugin 副本。marketplace plugin 是缓存下来的，而不是从 checkout 加载，所以只 pull checkout 不会刷新已安装的副本。

## 卸载

```
# Claude Code
/plugin uninstall bbs@babysit
/plugin marketplace remove babysit

# Codex CLI
codex plugin remove bbs@babysit
codex plugin marketplace remove babysit
```

```bash
brew uninstall bbs
rm -rf ~/.babysit          # 你的 ticket 和 analytics —— 想保留就跳过
```

从 checkout 装的话，删掉它之前还要先跑 `./bin/setup-skills --uninstall`。如果 pre-plugin 时代留下的旧软链还在，手动清理：

```bash
find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete
rm -f ~/.claude/babysit ~/.claude/bbs-*
```

## 故障排查

| 问题 | 修法 |
|-------|-----|
| 每次 `git push` 都被拒，报 "GATE OFFLINE" | `PATH` 上没有 `bbs` —— `brew install lohi-ai/babysit/bbs`。plugin 不带编译好的二进制，而 gate 按设计就是 fail closed |
| 更新后 skill 缺失或过期 | 运行 `bbs update`，然后重启受影响的 agent |
| `/bbs:*` 在 Claude Code 里找不到 | `claude plugin install bbs@babysit`，然后重启；或者 `/reload-plugins` |
| `$bbs:*` 在 Codex 里找不到 | `codex plugin add bbs@babysit`，然后开一个新 session |
| skill 显示时没有 `bbs:` 前缀 | 旧版安装 —— `find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete`，然后重装 plugin |
| `env resolve` 返回空 | 检查 `config/<app>/` 下存在正确的 `.env.base` |

## 许可证

MIT.
