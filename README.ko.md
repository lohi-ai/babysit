# babysit

[English](README.md) | [Tiếng Việt](README.vi.md) | [中文](README.zh.md) | [日本語](README.ja.md) | 한국어

**한 줄만 넘겨주세요. 당신이 없는 동안 계획하고, 만들고, review하고, QA합니다. 당신은 branch를 review합니다.**

```
/bbs:autopilot add a settings page with dark mode toggle
```

Codex 사용자는 같은 skill을 `$bbs:autopilot`으로 호출합니다. 이 README는
Codex 전용 command가 아닌 한 Claude Code의 `/bbs:` 표기를 씁니다.

ticket당 약 40분의 자율 작업이며, 어떤 단일 agent session도 한 번에 전부 감당할 수 없어도 끝까지 완료됩니다. 당신은 branch를 review하고, 만족스러우면 PR을 엽니다.

**`autopilot`부터 시작하세요.** command 하나에 제품 전체가 들어 있고, 어떤 터미널과 어떤 repo 배치에서도 동작하며, 모든 병렬 worker가 실행하는 것도 정확히 이것입니다 — 여기서 배우는 것은 하나도 버려지지 않습니다.

**그다음, 일이 ticket 하나보다 클 때 — `foreman`:**

```
/bbs:foreman rebuild the novel request flow across web, API, and migrations
```

Foreman은 프로젝트를 dependency graph로 분해하고, ticket마다 branch와 worktree를 만들고, 각 autopilot assistant를 Orca의 native Run/Task/Dispatch lifecycle로 감독합니다. build 전에 plan을 review하고, 공유 surface와 integration QA를 조율하고, disk와 Orca state에서 복구하며, repo의 finish policy를 dependency 순서대로 적용합니다. [Orca](https://www.onorca.dev)가 필요합니다. serial ticket 하나라면 autopilot을 직접 쓰세요.

*babysit은 babysitter가 필요 없을 때 하는 일입니다.* human in the loop이 필요한 결정보다 agent가 혼자 내리고 검증할 수 있는 결정을 선호합니다 — 예약 실행, orchestrated pipeline, 그리고 그냥 자리를 떠나고 싶은 모든 일을 위해 만들어졌습니다.

## 맛보는 가장 쉬운 방법

작은 팀의 메인 dev라면, 이것이 loop 전체입니다 — 잡일은 이쪽이 하고, gate는 당신이 쥡니다. 프로그램처럼 위에서 아래로 읽으세요:

```bash
/bbs:setup-project                        # repo당 한 번 — profile + QA 기본값
/bbs:autopilot "add dark-mode toggle"     # 어떤 변경이든: 계획 → 코드 → review → QA
#   → tickets/<id>/plan.md를 읽고, 출력된 /goal 블록을 붙여넣고 자리를 뜨세요
#   → autopilot이 코드를 쓰고, review하고, QA를 돌리고, 당신의 branch에 commit합니다
#   → 당신은 evidence를 review한 뒤:
bbs ticket serve bs-ab123                 # ticket이 자체 worktree에서 실행된 경우에만 (foreman / --mode=worktree)
#   → browser에서 돌아가는 모습을 보고, 그 session에 변경을 요청하고 serve를 다시 실행하세요
/bbs:create-pr                            # PR은 당신이 엽니다 — autopilot은 절대 열지 않습니다
```

ticket의 코드는 당신이 이미 서 있는 branch 위에 있으므로, dev server가 reload되면 바로 눈앞에 나타납니다. `bbs ticket serve`는 다른 형태 — 자체 worktree에서 실행된 ticket(`bbs ticket ensure --mode=worktree`, 또는 `/bbs:foreman` batch) — 를 위한 것이고, 그 코드는 이 checkout에 없습니다.

단계별로:

- **`/bbs:setup-project`** 를 한 번 — autopilot에게 당신의 branch naming + QA 기본값을 알려주므로, 이후의 모든 것이 결정적입니다.
- **`/bbs:autopilot "<one-line requirement>"`** 를 여러 단계 작업에. disk에 checkpoint를 남기고(crash와 context compaction을 견딥니다), 계획 → 구현 → review → QA를 수행한 뒤 PR checkpoint에서 멈추므로, 무언가 merge되기 전에 당신이 review합니다.

**인간 checkpoint — 당신이 통제권을 쥐는 지점.** Autopilot은 정말로 당신이 소유해야 하는 순간에만 멈춥니다. 어느 순간인지는 flag로 고르세요:

- `--stop-after=plan` — 코드가 한 줄이라도 쓰이기 전에 접근 방식을 승인합니다.
- `--planner <model>` / `--planner-effort <effort>` — plan과 UI
  prototype 생성을 해당 native model/profile과 reasoning effort에 위임합니다. 값
  을 생략하면 autopilot이 task 난이도와 현재 harness가 실제로 광고하는
  model들에서 고릅니다. OMP에서는 `default`와 `slow`가 설정된 model role을
  고르며, 일반 작업의 기본값은 `slow`입니다.
- *default* — QA-ready 상태로 멈추고, 당신이 evidence를 review합니다.
- **`/bbs:create-pr`** — 당신이 호출합니다. autopilot은 스스로 PR을 열지 않습니다.

**필요할 때 추가하세요:**

- **`/bbs:review-pr`** (일명 `/code-review`) — 작은 팀에는 두 번째 reviewer가 없으니, merge 전의 gate입니다. 당신의 안전망.
- **`/bbs:foreman`** — 자율 프로젝트 flow: 큰 requirement를 분해하고, 의존하는 ticket들을 Orca에서 스케줄하고, 그 worktree들을 소유하며, 프로젝트 QA와 finish를 gate합니다. serial ticket 하나에는 과합니다.

## 왜 잘 동작하는가

- **끝냅니다.** `/bbs:autopilot`은 **goal proxy**입니다. init이 durable state — ticket, requirement, plan, checkpoint — 를 심고, QA와 review verdict가 persist될 때까지 작업을 [`/goal`](#3-실행하기), 즉 agent의 persistent goal mode에 넘깁니다. loop 안에서 model은 직접 요청받았을 때처럼 full context로 자유롭게 작업하고, disk의 checkpoint 덕분에 새 session이 이전 session이 멈춘 지점에서 이어받습니다.
- **멈춰 서지 않습니다.** 모든 결정은 [Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md)를 거칩니다. agent가 결정하고 기록합니다. 정말로 사람이 필요하면, pop-up을 기다리는 대신 ticket에 `NEEDS_CONTEXT` 블록을 씁니다.
- **스스로 검증합니다.** QA는 기본 autopilot loop의 일부입니다. PASS에는 로컬에서 실행 중인 target 또는 이름 붙은 blocker가 필요하고, non-happy-path case도 있어야 합니다. "compile은 되니까 출시" 같은 건 없습니다.
- **감사 가능합니다.** `~/.babysit/analytics/`로 가는 JSONL telemetry와 `[WORK]` checkpoint comment가 남습니다. 사후에 tape를 읽으세요 — 아무도 실시간으로 지켜보지 않을 때의 주된 feedback channel입니다.

## 다섯 가지 archetype

engineering, product, design, data science가 하나의
product-builder로 녹아들면서, 쓸모 있는 작업 단위는 더 이상 job title이 아니라
지금 그 일에 필요한 *archetype*입니다. Babysit은 product-building team입니다:
product team의 다섯 archetype을 skill과 autopilot
workflow에 매핑하므로, 한 번의 run이 task가 필요로 하는 팀원이 될 수 있습니다.

한 사람은 2–3개의 archetype에 걸쳐 있고, babysit run도 마찬가지입니다. 파일의 "type"이
아니라 **일의 형태**로 고르세요. **archetype당 autopilot workflow는 정확히
하나**입니다.

| Archetype | 역할 | 언제 쓰나 | Workflow |
|-----------|---------|-------------------|----------|
| **Prototyper** | 새 아이디어를 마구 쏟아냅니다. 대부분은 출시되지 않습니다. 한 가지를 빠르게 배웁니다. | 아직 artifact가 없고 그냥 느낌뿐일 때 — 만들기로 확정하기 전에 검증합니다. | `prototyper` |
| **Builder** | prototype/아이디어를 production급 product와 infra로 만듭니다. | 검증된 아이디어나 승인된 plan이 있을 때. 새 feature 작업의 기본값. | `builder` |
| **Sweeper** | UI를 정리하고, code와 system을 단순화하고, unship하고, 최적화합니다. | code가 불필요한 무게를 지고 있을 때 — dead code, 중복, 과한 추상화. 덜어냅니다. behavior는 바뀌면 안 됩니다. | `sweeper` |
| **Grower** | 출시된 product를 반복 개선해 product-market fit을 높입니다. | product는 출시됐지만 funnel이 부진할 때. 먼저 측정하고, 되돌릴 수 있는 실험 하나를 돌립니다. | `grower` |
| **Maintainer** | 성숙한 system을 안전하고, 안정적이고, 빠르고, scale에서 효율적으로 유지합니다. | 성숙한 system이 production/scale 압박을 받을 때 — load, security, reliability, cost. | `maintainer` |

**Sweeper vs Maintainer** — 둘 다 performance를 건드리므로 헷갈리기 쉬운 짝입니다.
Sweeper는 *codebase*를 최적화하고(복잡도를 덜어내고, behavior는 byte-identical하게
유지되며, perf는 따라옵니다) 쌓인 cruft가 trigger입니다.
Maintainer는 *production의 system*을 최적화하고(실제 load 아래에서 버티게 하고,
caching/indexing/data-model 변경이 timing을 바꿀 수 있습니다) scale, security, cost가
trigger입니다. "안정적이고 널리 쓰이는 feature"는 Maintainer
trigger이고, 구조적 정리도 필요하다면 Sweeper를 별도의
behavior-preserving pass로 돌리세요.

**모든 archetype이 지키는 invariant** — *역할과 success
criterion*에서만 다르고, rigor에서는 절대 다르지 않습니다. 결정은
[Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md)를 거치고
(taste 결정은 기록되고, 조용히 추측되지 않습니다); "done" 전의
self-verification; 제한된 blast radius (force-push 없음, data 손실 없음, durable
authorization 없는 external message 없음); 크고 로컬하게 실패 (`BLOCKED`/`NEEDS_CONTEXT`
틀린 가정보다 우선).

각 archetype의 세부 사항과 구성 skill:
[`.claude/skills/references/archetypes.md`](.claude/skills/references/archetypes.md).

## 빠른 시작

세 단계입니다. 전역으로 한 번 설치하고, repo마다 한 번 설정한 뒤, 실행하세요.

### 1. plugin 설치

**GitHub에서 바로 — workspace에 아무것도 남지 않습니다.** CLI를 설치한 뒤,
같은 marketplace를 당신의 coding agent에 등록하세요:

```bash
brew install lohi-ai/babysit/bbs        # CLI — 필수, 아래 참고

# Claude Code
claude plugin marketplace add lohi-ai/babysit
claude plugin install bbs@babysit

# Codex CLI
codex plugin marketplace add lohi-ai/babysit
codex plugin add bbs@babysit
```

agent를 재시작하세요. Claude Code는 `/bbs:autopilot`을, Codex는
`$bbs:autopilot`을 노출합니다.

`bbs update`로 CLI와 설치된 모든 agent plugin을 upgrade하세요:

```bash
bbs update
```

**`brew install bbs`는 선택 사항이 아닙니다.** `bin/bbs`는 build artifact이고
commit되지 않으므로, GitHub에서 설치한 plugin에는 compiled binary가 없습니다.
`PATH`에 `bbs`가 없으면 push/PR gate가 ticket의 verdict를 읽을 수 없고,
fail closed합니다 — 모든 `git push`가 거부됩니다. Linux 사용자는 대신
tarball을 쓰세요: [docs/install.md](docs/install.md).

<details>
<summary><b>또는 checkout에서</b> — babysit 자체를 읽거나 수정하고 싶다면</summary>

checkout이 skill 수정을 publish하지 않고도 적용되는 유일한 형태입니다.
`setup-skills`가 binary를 build하고 모든 것을 연결합니다:

```bash
git clone https://github.com/lohi-ai/babysit.git ~/src/babysit
cd ~/src/babysit
./bin/setup-skills --full
```

그런 다음 둘 중 어느 agent에든 checkout을 등록하세요:

```
# Claude Code
/plugin marketplace add ~/src/babysit
/plugin install bbs@babysit

# Codex CLI (shell에서 실행)
codex plugin marketplace add ~/src/babysit
codex plugin add bbs@babysit
```

이렇게 하면 `~/.local/bin/bbs` → 당신의 checkout으로 `bbs`가 `PATH`에 올라가므로,
Homebrew 설치까지 할 필요는 없습니다. Upgrade는 `git pull && ./bin/setup-skills`.

marketplace plugin은 agent의 cache로 *복사*됩니다
(`~/.claude/plugins/cache/` 또는 `~/.codex/plugins/cache/`). `~/.claude/skills/<name>/`
아래의 Claude Code directory는 **제자리에서** load됩니다 — 이것이
working-tree 수정을 살아 있게 하는 형태입니다. 두 Claude 형태를 동시에
설치하지 마세요. 설치된 marketplace plugin이 이름 충돌에서 이깁니다.

</details>

요구 사항: plugin 지원이 있는 Claude Code 또는 Codex CLI, 그리고 Git.

권장하지만 시작에 필수는 아닙니다: **[Orca](https://www.onorca.dev)**, coding agent를 나란히 실행하기 위한 ADE입니다. `/bbs:autopilot`을 비롯한 모든 것은 어떤 터미널에서도 실행됩니다 — Orca는 **`foreman`에만 hard dependency**이고, foreman에는 다른 backend가 없습니다: 각 병렬 worker에게 자기 terminal tab을, editor에 diff를, 실행 중인 app을 Orca의 browser에 줍니다. Orca가 없으면 `foreman`은 설치 안내 메시지와 함께 빠르게 실패합니다.

#### `bbs` CLI는 무엇인가

Go binary 하나이고, 모든 subcommand는 `bbs <sub>`로 접근합니다 — `ticket`, `config`,
`secrets`, `upgrade`, `design`, `dashboard`, `autopilot`, `foreman`. production
bash는 남아 있지 않습니다.
formula는 argv0 alias 두 개, `bbs-config`와 `bbs-env`도 함께 설치합니다 — 딱 그
둘뿐이라서 skill은 항상 공백 형태로 호출합니다.

이것은 skill이 당신을 대신해 호출하는 babysit의 절반입니다: ticket identity,
verdict, git-flow 동작, telemetry. skill pack은 **포함하지 않습니다** —
skill, workflow, DESIGN.md/CSV data는 plugin에서 옵니다. 그래서 설치가 두
command이고, 어느 절반도 혼자서는 동작하지 않습니다.

전체 platform matrix와 Linux tarball: [docs/install.md](docs/install.md).

### 2. 프로젝트 설정

autopilot이 출시 작업을 하길 원하는 아무 repo 안에서:

```
/bbs:setup-project
```

wizard는 가장 작고 유용한 `.babysit/` config를 씁니다: `profile`과 `base_branch`를 담은 `git-flow.yaml`, `url`, `start`, `check`, `flows`를 담은 `qa.yaml`. 다시 실행해도 idempotent합니다 — 설정된 repo에서는 이걸로 profile을 바꿉니다.

#### Git flow: profile 하나 고르기

knob은 하나입니다. wizard는 질문 하나만 합니다 — **이 repo에서 실수는 무엇을 잃게 하는가?** — 팀 규모나 당신이 얼마나 지켜보는지가 아니라, 바로 이것이 git flow가 실제로 답하는 질문이기 때문입니다. 당신의 답이 `profile:`이고, 나머지 전부가 여기서 파생됩니다:

| | `pet` | `startup` | `enterprise` |
|---|---|---|---|
| 누가 | 1인, 취미 프로젝트 | 1인 프리랜서 / 작은 팀 | 팀, enterprise 규모 codebase |
| 우선순위 | 지금 출시 | 출시 속도 > code quality | code quality > 출시 속도 |
| ticket 작업이 가는 곳 | 당신이 있는 branch — 자르지 않음 | 당신이 있는 branch — 자르지 않음 | 당신이 있는 branch — 자르지 않음 |
| review가 일어나는 곳 | 없음 — push가 곧 release | **로컬에서**, browser에서, author가 merge | **GitHub에서**, 다른 사람이 merge |
| QA rigor | `smoke` — 3–5 case | `standard` — 5–10 | `strict` — 8–12 |
| `review-pr` effort | `low` | `medium` | `high` |

각 profile은 *당신*에게도 무언가를 요구합니다 — ticket이 시작될 때 어느 branch에 있어야 하는지, 당신의 local base가 무엇을 위한 것인지. 그 contract와 profile별로 ticket을 병렬 실행하는 방법은 **[docs/profiles.md](docs/profiles.md)**에 있습니다.

**한 번도 설정하지 않은 repo는 `pet`으로 귀결됩니다** — branch도, review 장소도 없고, 작업은 평범한 git을 쓰는 것과 똑같이 당신이 서 있는 branch에 얹힙니다. Ceremony는 repo가 요청하는 것이지, 자동으로 받는 것이 아닙니다.

**처음이거나 확신이 없다면? `startup`을 고르세요.** 아끼는 repo에서 시작하기에 안전한 지점입니다: standard QA rigor, 그리고 당신이 여는 PR 없이는 아무것도 remote에 도달하지 않습니다. 나중에 profile을 바꾸는 건 한 줄입니다.

Rigor가 확장하는 것은 **폭이지 기준이 아닙니다**. `PASS`는 세 profile 모두에서 같은 의미입니다: 해당되는 모든 rubric 차원이 B 이상이고, 최종 code에 대한 새 end-to-end run이 있을 때. pet 프로젝트는 case를 더 적게 돌릴 뿐입니다 — 결코 0개를 돌리지 않고, C급 차원으로 통과하지도 않습니다.

`mode:`, `land:`, `push:`를 직접 설정하면 profile의 preset을 덮어씁니다. 그건 escape hatch이지 정상적인 형태가 아닙니다 — 손으로 적어낸 knob은 더 이상 자기 profile을 따라가지 않습니다.

#### 병렬 ticket은 요청하는 것이지, 설정하는 것이 아니다

**어떤 profile도 branch를 자르거나 당신을 worktree로 옮기지 않습니다.** 세 profile 모두에서 당신은 babysit 없이 일할 때와 똑같이, 서 있는 branch에서 작업합니다 — profile이 정하는 것은 review 장소와 QA rigor뿐이고, 작업이 사는 위치에 대해서는 아무것도 정하지 않습니다. 이건 의도적입니다: 당신의 작업을 조용히 다른 곳으로 옮기는 tool은 관리할 수 없는 tool입니다.

격리는 정말로 원할 때 run마다 요청합니다:

- `/bbs:foreman` — batch: foreman이 config가 뭐라 하든, 어떤 repo에서든 ticket마다 worktree 하나를 만듭니다.
- `bbs ticket ensure --slug-hint <slug> --mode=worktree` — 이 ticket 하나를 자체 worktree에; 그 안에서 autopilot을 실행하세요.
- `bbs ticket ensure --slug-hint <slug> --mode=branch` — `feat/<id>_<slug>`를 제자리에서 자릅니다.

**worktree에는 대가가 따르니, 무엇을 샀는지 아세요.** inner loop는 더 이상 0-step이 아닙니다: code는 worktree에 살고 dev server는 primary checkout을 서비스하므로, test를 한 번 돌릴 때마다 edit-and-refresh 대신 commit 하나에 `bbs ticket surface compose`가 필요합니다. 그 대가로 얻는 것은 분리 가능한 ticket입니다 — 하나를 따로 review하고, 잘못된 것을 버리고, 각각을 독립된 깨끗한 PR로 land할 수 있습니다. 항상 그 형태를 원하는 repo는 `mode: worktree` + `land: local`을 직접 쓸 수 있습니다. 아무도 대신 써주지 않습니다.

worktree와 공유 surface 사이에서 작업을 옮기는 command들 — `bbs ticket surface <acquire|compose|revert|release|status>`, 그리고 인간 계층인 `board`, `serve`, `/bbs:fix-pr` — 은 [ticket을 병렬로 작업하기](#ticket을-병렬로-작업하기-worktree-모드)에 있습니다. 세부: [`references/git-flow.md`](.claude/skills/references/git-flow.md)와 [`references/worktrees.md`](.claude/skills/references/worktrees.md).

### 3. 실행하기

**여기서 시작하세요 — `autopilot`, 단일 ticket flow:**

```
/bbs:autopilot "add a settings page with dark mode toggle"
```

Autopilot은 ticket을 init하고 — requirement, plan — 멈춘 뒤 **마지막 메시지**로 `/goal` 블록을 출력합니다. 그 블록이 당신이 다음에 할 유일한 일입니다: **복사해서, 같은 agent에 다시 붙여넣고, 자리를 뜨세요.** 그러면 goal session이 code를 쓰고, review하고, QA를 돌리고, 당신이 있는 branch에 작업을 commit합니다 — autopilot은 branch를 자르거나 push하거나 PR을 열지 않습니다. review 후 PR은 당신이 직접 여세요. plan과 UI prototype을 만드는 native model을 고르려면 `--planner gpt-5.6-sol --planner-effort high`를 넘기세요. flag를 생략하면 autopilot이 task 난이도와 현재 harness가 광고하는 capability에서 고릅니다.

> **handoff는 이렇게 생겼습니다** — autopilot은 평범한 언어의 preamble로 끝나고, 그다음 복사할 블록이 옵니다:
>
> ```
> bs-ab123 준비 완료. 붙여넣기 전에 무엇이 만들어질지 확인하세요:
>   plan:      tickets/bs-ab123/plan.md
>   prototype: tickets/bs-ab123/prototype.html
> design이 잘못됐다면 지금 바로잡으세요 — 그러지 않으면 붙여넣기 한 번이면 끝입니다.
>
> 👉 아래 블록을 복사해 agent에 붙여넣으면 build됩니다:
>
> /goal bs-ab123 is done: work committed locally, qa verdict PASS/FIXED persisted
> via bbs ticket set-verdict, review-pr verdict persisted, handoff note written — or a
> NEEDS_CONTEXT / BLOCKED status block printed verbatim.
> Work it: /bbs:autopilot builder bs-ab123
> ```

#### 왜 `/goal`이 작업을 소유하는가

`/goal <condition>`은 지원되는 agent의 persistent goal mode를 시작합니다: model은 step 의식 없이 full context로 자유롭게 작업하고, 조건이 성립할 때까지 계속합니다. 그래서 이 단계가 "command 실행"이 아니라 *`/goal` 블록을 붙여넣기*입니다: 붙여넣는 행위가 goal을 무장시킵니다. Autopilot이 출력하는 블록에는 babysit gate와 escape clause가 이미 들어 있습니다.

escape clause 덕분에 loop는 없는 input에 맞서 갈아대는 대신 escalation으로 종료됩니다. 실행 중에 빠져나가려면: `/goal clear`, `Ctrl-C`, 또는 `~/.babysit/projects/<slug>/tickets/<ticket>/STOP`을 touch하세요.

`/goal` 없이도 `/bbs:autopilot bs-ab123`을 다시 호출하면 checkpoint에서 이어집니다 — session 경계를 손으로 넘겨줄 뿐입니다.

#### 고급 — `foreman`, 자율 프로젝트 orchestrator

requirement가 여러 ticket에 걸치면, `foreman`이 프로젝트 전체를 소유합니다. 각 worker는 당신이 아는 그 autopilot을 그대로 돌리지만, checkout을 제공하고 DAG를 조율하는 것은 foreman입니다:

```
/bbs:foreman <large project requirement>  # 분해, 스케줄, 검증, 마무리
/bbs:foreman                              # ticket + Orca state에서 attach/resume
```

Foreman은 parent project 하나와 경계가 정해진 child ticket들을 만들고, 그 dependency edge를 기록하고, Orca orchestration을 통해 감독되는 worker들을 시작합니다. 모든 child는 plan-only Dispatch, 자율 design gate, 그다음 build/QA Dispatch를 받습니다. 각 worker는 그 ticket의 난이도가 벌어주는 model로 시작됩니다 — `simple` / `normal` / `critical` tier는 autopilot의 planner도 쓰는 공유 [model-routing table](.claude/skills/references/model-routing.md)에서 읽고, 결정된 model은 child에 기록됩니다. 거의 모든 ticket은 routine rung(`gpt-5.6-sol` / `opus` / `@default`)에서 돌고, 값비싼 top rung(`gpt-6-astra`, Fable 5.1)은 table의 escalation trigger가 필요하므로 batch가 조용히 거기까지 올라갈 수 없습니다. Foreman은 disk에서 verdict를 검증하고, 공유 test surface를 직렬화하고, ticket들이 상호작용할 때 조합된 integration QA를 돌리고, repo의 `finish:` policy를 dependency 순서대로 자격 있는 각 child에 적용합니다 — `land`는 base로 merge하고, `pr`은 PR을 열고, `review`는 정리된 worker를 release합니다 — 그래서 끝난 ticket이 프로젝트를 기다리는 일은 없습니다. 다만 pending integration gate에 걸린 child는 그것이 통과할 때까지 land를 붙잡습니다. 프로젝트 DAG는 status와 dispatch reply에 함께 실리고, `bbs ticket dag <parent> --mermaid`가 요청 시 출력합니다. Orca message와 Dispatch id가 pane-text polling을 대체하고, 자동 시작된 watcher가 멈춘 coordinator를 찌르고 설정된 interval마다 status를 다시 묻습니다. 그래서 재시작한 session도 대화 기억 없이 이어갑니다.

**hard prerequisite 하나, 그래서 이게 두 번째로 배울 것:** orchestration이 켜진 [Orca](https://www.onorca.dev). Foreman은 설치되어 version이 맞는 Orca orchestration guide를 load하고, 그 runtime이 없으면 빠르게 실패하며, profile과 무관하게 child마다 worktree를 만듭니다. rigor와 finish policy는 여전히 당신의 profile이 정합니다. serial ticket 하나에서는 `/bbs:autopilot` 대비 얻는 것이 없습니다.

## 사용법

Babysit은 변경을 출시하기 위한 작은 조립 라인입니다. 한쪽 끝에 아이디어를 넣으면 다른 쪽 끝에서 review 준비가 된 branch를 집어 듭니다. 라인은 당신이 실제로 가치를 더하는 네 순간에 멈추고, 그 사이의 모든 것은 스스로 진행됩니다.

### 멈추는 네 지점

1. **"이게 만들어야 할 것이 맞나?"** — `requirement.md` 준비 완료. 당신이 읽고 승인합니다.
2. **"이게 만드는 방식이 맞나?"** — `plan.md` 준비 완료. 당신이 읽고, 고치고, 승인합니다.
3. **"실제로 동작하나?"** — code가 작성되고, review되고, QA 확인되고, commit되었습니다.
4. **"이게 PR이 되어야 하나?"** — 당신이 handoff를 review하고 `/bbs:create-pr`을 실행합니다. reviewer comment가 달리면 `/bbs:fix-pr`이 그것들을 처리합니다.

### 어디서 멈출지 고르기

| 멈추는 지점 | 방법 |
|---------|-----|
| pause 1 — `requirement.md` 준비 완료 | `/bbs:autopilot "<idea>" --stop-after=requirement` |
| pause 2 — `plan.md` 준비 완료 | `/bbs:autopilot "<idea>" --stop-after=plan` |
| pause 3 — QA까지 확인된 branch 준비 완료 | `/bbs:autopilot "<idea>"` *(end-to-end, 기본값)* |
| pause 4 — PR handoff | human review 후 `/bbs:create-pr` 실행 |

stage가 끝나면 ticket에 `Next:` 줄이 생깁니다 — 말 그대로 다음에 할 일입니다. `/bbs:autopilot bs-<id>`를 다시 호출하면 probe된 state에서 항상 올바른 다음 stage를 고르므로, 어느 workflow를 불러야 하는지 기억할 필요가 없습니다.

### 세 가지 입력 형태

```
/bbs:autopilot "<one-line idea>"     # 새 feature — ticket을 만들고 end-to-end로 실행
/bbs:autopilot bs-ab123              # 기존 ticket — state에 따라 다음 stage로 라우팅
/bbs:autopilot                       # resume — 결정된 ticket의 checkpoint에서 이어받기
```

이게 surface 전부입니다. flag 세 개가 이를 확장합니다 — 더 이른 checkpoint에서 멈추는 `--stop-after=requirement|plan`, plan/prototype 생성을 라우팅하는 `--planner <model>` / `--planner-effort <effort>`. Autopilot은 항상 시작한 checkout에서 작업합니다. 격리는 `bbs ticket ensure --mode=…` 또는 `/bbs:foreman`이고, autopilot flag로는 절대 안 됩니다. Verb token은 존재하지 않습니다.

### ticket을 병렬로 작업하기 (worktree 모드)

**이것은 foreman batch(또는 수동 `bbs ticket ensure --mode=worktree`)가 당신에게 주는 형태입니다** — 그런 run이 건네주는 command들입니다. `/bbs:foreman`은 batch 전체에 대해 같은 구간을 구동합니다 — dispatch, design gate, verdict 확인, 조합된 surface — 하지만 여기서 작업하는 데 그게 필요한 것은 아닙니다.

repo마다 무거운 checkout 하나가 dev server를 돌리고, 모든 ticket은 각자의 가벼운 worktree에 삽니다. 그래서 누군가 ticket이 실제로 돌아가는 모습을 봐야 하는 순간을 *제외하면* 모든 것이 병렬입니다 — 그리고 그 순간을 위해 command 세 개가 있습니다:
```bash
bbs ticket board            # 모든 ticket을 한눈에: status, verdict, live session, PR, 누가 surface를 쥐고 있는지
bbs ticket serve bs-ab123   # 이 ticket을 human review를 위해 실행 중인 dev server에 올립니다
bbs ticket serve            # 인자 없이: 끝난 모든 ticket(qa + review DONE)을 server에 compose
/bbs:fix-pr                 # reviewer comment가 달린 뒤: 미해결 thread를 가져와 수정, 답변, resolve
```

**review loop.** browser에서 실행 중인 feature를 review하는 것이 가장 긴 필수 단계이므로, babysit은 이것을 가장 싸게 반복할 수 있게 만듭니다:

1. ticket이 pause 3에 도달합니다 — handoff의 `Next:` 줄이 정확한 command를 건넵니다: `bbs ticket serve bs-ab123`.
2. `serve`는 4시간 동안 test surface를 붙잡고(agent들의 QA는 예의 바르게 당신 뒤에 줄을 섭니다) 실행 중인 server를 base + 정확히 이 ticket으로 바꿉니다 — 이 repo에서 **그리고** ticket이 둘 다에 걸쳐 있으면 FE/BE sibling repo에서도요.
3. browser에서 review하세요. ticket의 session에 변경을 요청하면 자체 worktree에서 commit합니다. `serve`를 다시 실행하고(reentrant — hold를 갱신하고 surface를 다시 자릅니다) browser를 새로 고치세요. 만족할 때까지 반복합니다.
   Orca에서는 이 loop 전체가 worktree 하나에 들어갑니다: `orca tab create --url <qa url>`이 실행 중인 app을 내장 browser에 올리고, `orca file open-changed --mode diff --worktree path:<ticket-worktree>`가 그 옆에 ticket의 diff를 엽니다.
4. 승인 → `bbs ticket serve --release`, 그다음 repo마다 `/bbs:create-pr`. 나중에 reviewer comment → `/bbs:fix-pr`.
5. `bbs ticket board --pr`이 merge된 PR을 표시하고 정확한 cleanup command(`surface revert`, `set-status done`)를 출력합니다.

**ticket 하나, repo 둘** (frontend + backend에 걸친 feature): `/bbs:setup-project`가 sibling repo를 한 번 기록해 두면, autopilot의 builder가 스스로 넘어갑니다 — 연결된 sibling ticket을 만들고, 양쪽을 구현하고 QA합니다 — 그리고 `serve`가 command 하나로 그 쌍 전체를 당신 앞에 올립니다. 그동안 다른 ticket의 session들은 각자의 worktree에서 계속 구현하고 review합니다. `board`는 누가 surface를 얼마나 오래 쥐고 있는지 모두에게 보여줍니다. 전체 recipe: [`references/worktrees.md` § Attended parallel review](.claude/skills/references/worktrees.md).

## 더 깊이 들어가기

- **Routing 내부와 debugging** — init이 무엇을 심는지, `/goal` loop가 checkpoint에서 어떻게 이어지는지, 어느 step skill이 각 gate를 소유하는지: [`.claude/skills/autopilot/SKILL.md`](.claude/skills/autopilot/SKILL.md). 실행하지 않고 run이 라우팅할 state를 보려면: `bbs autopilot explain` (workflow prereq matrix는 `--details` 추가).
- **Profile** — [`docs/profiles.md`](docs/profiles.md): 각 profile이 당신의 base branch에 요구하는 것, 그리고 각 profile에서 ticket을 병렬로 돌리는 방법.
- **Config schema** — `.babysit/`를 직접 작성하기 위한 [`.claude/skills/references/git-flow.md`](.claude/skills/references/git-flow.md)와 [`docs/qa-config.md`](docs/qa-config.md).

## Skill 목록

`/bbs:autopilot`은 아래 skill들을 조합해 완전한 workflow로 만듭니다. 조각만 필요할 때는 하나를 직접 쓰세요 — 가장 많이 쓰는 것들:

| 하고 싶은 것 | Skill |
|------------|-------|
| 한 줄 아이디어에서 feature를 end-to-end로 출시 | `/bbs:autopilot "<idea>"` |
| 여러 ticket이나 feature로 이루어진 큰 프로젝트를 완성 | `/bbs:foreman "<project>"` |
| 만들기로 확정하기 전에 아이디어를 stress-test | `/bbs:office-hours` |
| 기존 UI system 안에서 feature를 design | `/bbs:design-ui` |
| requirement를 `plan.md`로 전환 (코딩 없이) | `/bbs:plan-draft` |
| 이미 승인된 plan에서 build | `/bbs:implement` |
| marketing copy나 conversion을 개선 | `/bbs:copy-rewrite`, `/bbs:conversion-fix` |
| growth experiment나 short-form script를 제안 | `/bbs:growth-experiment`, `/bbs:social-content` |
| browser에서 URL이나 frontend flow를 확인 | `/bbs:browse` |
| 전체 browser test/fix loop를 실행 | `/bbs:qa` |
| land 전에 branch를 review | `/bbs:review-pr` |
| bug의 근본 원인을 찾기 | `/bbs:investigate` |
| 이 repo를 autopilot용으로 설정 | `/bbs:setup-project` |
| review 가능한 pull request를 생성 | `/bbs:create-pr` |
| PR review comment를 처리 (수정, 답변, resolve) | `/bbs:fix-pr` |

전체 skill 표(autonomous-ready / interactive-only 분류 포함)는 [`docs/skills.md`](docs/skills.md)에 있습니다.

## 함께 쓰는 CLI

모든 것이 `bbs <sub>`로 접근하는 binary 하나입니다 — `bbs autopilot`(runner), `bbs ticket env`(identity resolver: `BABYSIT_TICKET` → manifest → branch), 그리고 env, config, db snapshot, upgrade 확인용 helper들. `brew install lohi-ai/babysit/bbs`가 이것을 `PATH`에 올립니다. checkout에서는 `setup-skills`가 대신 build해서 `~/.local/bin/bbs`를 symlink하고, legacy 호출자를 위해 `bbs-*` argv0 alias를 `~/.claude/`에 넣습니다. 전체 표와 용도는 [`docs/companion-cli.md`](docs/companion-cli.md)에 있습니다. 각 subcommand의 사용법은 `bbs <sub> --help`를 실행하세요.

## 운영

Day-2 config(`bbs config`), telemetry(`~/.babysit/analytics/`로 가는 JSONL, 기본은 local-only), update 처리(`bbs update check` + `bbs update`)는 [`docs/operations.md`](docs/operations.md)에서 다룹니다.

**업그레이드.** CLI와 당신이 쓰는 agent의 plugin copy를 refresh한 뒤, 그 agent를 재시작하세요:

```bash
bbs update
```

babysit은 서로 다른 tool이 소유하는 두 절반으로 출시됩니다 — brew CLI와 agent plugin. `bbs update`는 CLI와 설치된 Claude Code, Codex plugin copy를 함께 갱신합니다. marketplace plugin은 checkout에서 load되지 않고 cache되므로, checkout을 pull하는 것만으로는 설치된 copy가 refresh되지 않습니다.

## 제거

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
rm -rf ~/.babysit          # 당신의 ticket과 analytics — 유지하려면 건너뛰세요
```

checkout에서 설치했다면, 삭제하기 전에 `./bin/setup-skills --uninstall`도 실행하세요. plugin 이전 설치에서 남은 legacy symlink가 있다면 수동 정리:

```bash
find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete
rm -f ~/.claude/babysit ~/.claude/bbs-*
```

## 문제 해결

| 문제 | 해결 |
|-------|-----|
| 모든 `git push`가 거부됨, "GATE OFFLINE" | `PATH`에 `bbs`가 없음 — `brew install lohi-ai/babysit/bbs`. plugin은 compiled binary를 싣지 않고, gate는 설계상 fail closed |
| update 후 skill이 없거나 오래됨 | `bbs update`를 실행한 뒤 해당 agent를 재시작 |
| Claude Code에서 `/bbs:*`를 찾을 수 없음 | `claude plugin install bbs@babysit` 후 재시작, 또는 `/reload-plugins` |
| Codex에서 `$bbs:*`를 찾을 수 없음 | `codex plugin add bbs@babysit` 후 새 session 시작 |
| skill이 `bbs:` prefix 없이 보임 | legacy 설치 — `find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete` 후 plugin 재설치 |
| `env resolve`가 비어 있음 | `config/<app>/` 아래에 올바른 `.env.base`가 있는지 확인 |

## 라이선스

MIT.
