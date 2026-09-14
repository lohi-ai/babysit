# babysit

[English](README.md) | [Tiếng Việt](README.vi.md) | [中文](README.zh.md) | 日本語 | [한국어](README.ko.md)

**一行渡すだけでいい。あなたが席を外している間に計画し、実装し、レビューし、QA までやる。あなたは branch をレビューするだけ。**

```
/bbs:autopilot add a settings page with dark mode toggle
```

Codex ユーザーは同じ skill を `$bbs:autopilot` として呼ぶ。コマンドが Codex 固有でない
限り、この README では Claude Code の `/bbs:` 表記を使う。

1 ticket あたり約 40 分の自律作業。1 つの agent session では一度に抱えきれない量でも、最後までやり切る。あなたは branch をレビューし、納得したら PR を開く。

**まずは `autopilot`。** 製品のすべてが 1 コマンドに入っていて、どんなターミナルでも、どんな repo layout でも動き、並列 worker が全員走らせているのもこれ — だからここで学んだことは捨てにならない。

**そして、仕事が 1 ticket より大きくなったら — `foreman`:**

```
/bbs:foreman rebuild the novel request flow across web, API, and migrations
```

Foreman はプロジェクトを依存グラフに分解し、ticket ごとに branch と worktree を作り、各 autopilot assistant を Orca のネイティブな Run/Task/Dispatch ライフサイクルで監督する。build の前に plan をレビューし、shared surface と統合 QA を調整し、disk と Orca state の両方から復帰し、repo の finish policy を依存順に適用する。[Orca](https://www.onorca.dev) が必要。直列の 1 ticket なら autopilot を直接使う。

*babysit とは、babysitter が要らないときにやるやつのことだ。* 人間が loop に入らないと決められないことより、agent が 1 人で決めて検証できることを優先する — scheduled run、orchestrated pipeline、そして席を立ちたいあらゆるもののために作られている。

## 手っ取り早く味見する

小さなチームのメイン dev なら、これが loop のすべて — 面倒な作業はやつがやり、gate はあなたが握る。プログラムを読むように上から下まで読めばいい:

```bash
/bbs:setup-project                        # repo ごとに 1 回 — profile + QA デフォルト
/bbs:autopilot "add dark-mode toggle"     # どんな変更でも: plan → code → review → QA
#   → tickets/<id>/plan.md を読み、出力された /goal ブロックを貼って席を立つ
#   → autopilot が code を書き、レビューし、QA を回し、あなたの branch に commit する
#   → あなたはエビデンスをレビューし、それから:
bbs ticket serve bs-ab123                 # ticket が自前の worktree で走った場合のみ (foreman / --mode=worktree)
#   → ブラウザで動いているものを見る; ticket の session に変更を頼み、serve を再実行する
/bbs:create-pr                            # PR を開くのはあなた — autopilot は決してやらない
```

ticket の code は、あなたがすでに立っている branch の上にある。dev server が reload した瞬間に目の前にあるということだ。`bbs ticket serve` はもう一方の形 — ticket が自前の worktree で走った場合 (`bbs ticket ensure --mode=worktree`、または `/bbs:foreman` の batch) — のためのもので、その code はこの checkout にはない。

段階ごとに:

- **`/bbs:setup-project`** を 1 回 — autopilot に branch naming と QA デフォルトを教え込む。以降のすべてが決定的になる。
- **`/bbs:autopilot "<one-line requirement>"`** を複数ステップの作業なら何にでも。disk に checkpoint を書いて (crash と context compaction に耐える)、plan → implement → review → QA と進み、PR checkpoint で止まるので、何かが merge される前にあなたがレビューできる。

**人間の checkpoint — あなたが制御を保つ場所。** autopilot が止まるのは、本当にあなたが持つべき瞬間だけ。どれにするかは flag で選ぶ:

- `--stop-after=plan` — code が 1 行も書かれる前にアプローチを承認する。
- `--planner <model>` / `--planner-effort <effort>` — plan と UI
  prototype の作成を、その native model/profile と reasoning effort に委譲する。どちらかの
  値を省くと、autopilot がタスクの難易度と、現在の harness が実際に advertise している
  model から選ぶ。OMP では `default` と `slow` が設定済みの model role を選び、
  通常の作業はデフォルトで `slow` になる。
- *default* — QA-ready で止まる。あなたがエビデンスをレビューする。
- **`/bbs:create-pr`** — 呼ぶのはあなた。autopilot 自身は決して PR を開かない。

**必要になったら足す:**

- **`/bbs:review-pr`** (別名 `/code-review`) — 小さなチームには 2 人目の reviewer がいないので、merge 前の gate。あなたの safety net。
- **`/bbs:foreman`** — 自律プロジェクトの flow。大きな requirement を分解し、依存する ticket を Orca でスケジュールし、それらの worktree を所有し、プロジェクトの QA と finish を gate する。直列の 1 ticket には過剰。

## なぜうまくいくのか

- **やり切る。** `/bbs:autopilot` は **goal proxy** だ。init が永続 state — ticket、requirement、plan、checkpoint — を蒔き、QA と review の verdict が永続化されるまで作業を [`/goal`](#3-実行する) に渡す。`/goal` は agent の persistent goal mode だ。loop の中では model は直接頼まれたときと同じように full context で自由に働き、disk 上の checkpoint のおかげで新しい session が前の session の止まった場所から再開できる。
- **ハングしない。** すべての決定は [Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md) を通る。agent が決定して log に残す。本当に人間が必要なら、pop-up を待つ代わりに `NEEDS_CONTEXT` ブロックを ticket に書く。
- **自分で検証する。** QA はデフォルトの autopilot loop の一部だ。PASS にはローカルで動く対象か、名前の付いた blocker、加えて happy path 以外のケースが要る。「コンパイルは通った、出荷だ」はない。
- **監査できる。** `~/.babysit/analytics/` への JSONL telemetry と `[WORK]` checkpoint comment。あとからテープを読める — 誰もライブで見ていないときの主要な feedback channel だ。

## 5 つの archetype

engineering、product、design、data science が 1 種類の
product-builder に溶け合うと、仕事の有用な単位は職種ではなくなり、
そのときその仕事が求める *archetype* になる。Babysit は product-building team だ。
プロダクトチームの 5 つの archetype を skill と autopilot
workflow に対応させるので、1 回の run がタスクの求める teammate として振る舞える。

人間は 2〜3 の archetype をまたぐ。babysit の run も同じだ。選ぶ基準は仕事の **形** で
あって、ファイルの「type」ではない。**1 archetype につき autopilot workflow はちょうど 1 つ**。

| Archetype | 役割 | 使う場面 | Workflow |
|-----------|---------|-------------------|----------|
| **Prototyper** | 真新しいアイデアを量産する。ほとんどは ship しない。1 つだけ速く学ぶ。 | まだ artifact がなく、勘しかない — 作り込むと決める前に検証する。 | `prototyper` |
| **Builder** | prototype/idea を production-grade なプロダクトと infra に変える。 | 検証済みのアイデアか、承認済みの plan がある。新機能のデフォルト。 | `builder` |
| **Sweeper** | UI を片付け、code と system を単純化し、unship し、最適化する。 | code が不要な重さを抱えている — dead code、重複、過剰な抽象化。引き算し、挙動は変えてはならない。 | `sweeper` |
| **Grower** | ship 済みのプロダクトを反復して product-market fit を改善する。 | プロダクトは ship しているが funnel が振るわない。まず測定し、それから可逆な実験を 1 つ回す。 | `grower` |
| **Maintainer** | 成熟した system を安全・信頼でき・速く・スケールしても効率的に保つ。 | 成熟した system が production/scale の圧力下にある — 負荷、security、reliability、コスト。 | `maintainer` |

**Sweeper と Maintainer** — 混同しやすい組み合わせだ。どちらも
performance に触るからだ。Sweeper が最適化するのは *codebase* (複雑さを引き算し、挙動は
byte 単位で同じまま、perf はついてくる) で、引き金は積み上がった
cruft だ。Maintainer が最適化するのは *本番の system* (実際の
負荷に耐えさせる。caching/indexing/data-model の変更はタイミングを変えうる) で、引き金は
scale、security、コスト。「安定して広く使われている機能」は Maintainer の
引き金だ。それが構造的な cleanup も必要なら、Sweeper を別の
behavior-preserving な pass として回す。

**あらゆる archetype が守る invariant** — 違うのは *mandate と成功
基準* だけで、厳しさは決して変わらない: 決定は
[Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md) を通る
(taste の決定は記録され、黙って推測されることはない)。「done」の前に
自己検証。blast radius は限定 (force-push なし、data loss なし、永続的な
authorization なしの外部メッセージなし)。失敗は大きく、ローカルに
(間違った前提を抱えるより `BLOCKED`/`NEEDS_CONTEXT`)。

各 archetype が組み立てる skill の詳細:
[`.claude/skills/references/archetypes.md`](.claude/skills/references/archetypes.md)。

## クイックスタート

3 ステップ。グローバルに 1 回インストールし、repo ごとに 1 回設定し、あとは実行する。

### 1. plugin をインストールする

**GitHub から直接 — workspace には何も落ちてこない。** CLI をインストールし、
同じ marketplace をあなたの coding agent に登録する:

```bash
brew install lohi-ai/babysit/bbs        # CLI — 必須、下記参照

# Claude Code
claude plugin marketplace add lohi-ai/babysit
claude plugin install bbs@babysit

# Codex CLI
codex plugin marketplace add lohi-ai/babysit
codex plugin add bbs@babysit
```

agent を再起動する。Claude Code は `/bbs:autopilot` を公開し、Codex は
`$bbs:autopilot` を公開する。

CLI とインストール済みの全 agent plugin を `bbs update` でアップグレードする:

```bash
bbs update
```

**`brew install bbs` は任意ではない。** `bin/bbs` は build artifact で
commit されていないので、GitHub から入れた plugin にコンパイル済み binary は入っていない。
`bbs` が `PATH` にないと push/PR gate が ticket の verdict を読めず、
fail closed する — すべての `git push` が拒否される。Linux ユーザーは
代わりに tarball を使う: [docs/install.md](docs/install.md).

<details>
<summary><b>あるいは checkout から</b> — babysit 自体を読みたい、改造したいなら</summary>

checkout は、skill への編集を publish せずに効かせられる唯一の形だ。
`setup-skills` が binary を build し、すべてを配線する:

```bash
git clone https://github.com/lohi-ai/babysit.git ~/src/babysit
cd ~/src/babysit
./bin/setup-skills --full
```

それからどちらかの agent に checkout を登録する:

```
# Claude Code
/plugin marketplace add ~/src/babysit
/plugin install bbs@babysit

# Codex CLI (シェルから実行)
codex plugin marketplace add ~/src/babysit
codex plugin add bbs@babysit
```

これで `bbs` が `~/.local/bin/bbs` → あなたの checkout として `PATH` に入るので、
Homebrew のインストールは不要だ。アップグレードは `git pull && ./bin/setup-skills`。

marketplace plugin は agent の cache
(`~/.claude/plugins/cache/` または `~/.codex/plugins/cache/`) に *コピー* される点に注意。
`~/.claude/skills/<name>/` 配下の Claude Code
ディレクトリは **in place** で読み込まれる — これが working-tree の編集を
生かす形だ。Claude の 2 つの形を同時に入れないこと。インストール済みの marketplace plugin が
名前の衝突に勝つ。

</details>

要件: plugin をサポートする Claude Code または Codex CLI、加えて Git。

推奨だが、始めるのに必須ではない: **[Orca](https://www.onorca.dev)** — coding agent を並べて走らせるための ADE。`/bbs:autopilot` もそれ以外もどんなターミナルでも動く — Orca が **hard dependency なのは `foreman` だけ** で、foreman に他の backend はない。並列 worker ごとに専用の terminal tab を与え、editor に diff を出し、動いているアプリを Orca の browser に出す。Orca がないと `foreman` は install メッセージを出して即座に失敗する。

#### `bbs` CLI とは

1 つの Go binary で、すべての subcommand が `bbs <sub>` — `ticket`、`config`、
`secrets`、`upgrade`、`design`、`dashboard`、`autopilot`、`foreman` として届く。
production の bash はもう残っていない。
formula は 2 つの argv0 alias、`bbs-config` と `bbs-env` も落とす — その 2 つだけ
であり、だから skill は常にスペース形式で呼ぶ。

これは babysit の片割れで、skill があなたの代わりに呼ぶものだ: ticket identity、
verdict、git-flow の移動、telemetry。skill pack は **含まない** —
skill、workflow、DESIGN.md/CSV データは plugin から来る。だからインストールが 2 コマンドで、
どちらの半分も単独では動かない。

platform の全 matrix と Linux tarball: [docs/install.md](docs/install.md).

### 2. プロジェクトを設定する

autopilot に ship させたい repo の中で:

```
/bbs:setup-project
```

wizard は最小限の役に立つ `.babysit/` config を書く: `profile` と `base_branch` を持つ `git-flow.yaml`、`url`、`start`、`check`、`flows` を持つ `qa.yaml`。再実行は冪等だ — 設定済みの repo では、profile を切り替える方法がこれになる。

#### Git flow: profile を 1 つ選ぶ

knob は 1 つだけ。wizard は質問を 1 つしかしない — **この repo でミスはいくらかかるか?** — なぜなら git flow が本当に答えているのは、チームの大きさでも監視の密さでもなく、それだからだ。あなたの答えが `profile:` で、他のすべてはそこから導かれる:

| | `pet` | `startup` | `enterprise` |
|---|---|---|---|
| 誰が | 個人、趣味プロジェクト | 個人フリーランス / 小さなチーム | チーム、エンタープライズ codebase |
| 優先 | 今すぐ ship | release の速さ > code の質 | code の質 > release の速さ |
| ticket の作業の置き場 | あなたが立っている branch — 切らない | あなたが立っている branch — 切らない | あなたが立っている branch — 切らない |
| review はどこで | どこでもしない — push が release | **ローカルで**、ブラウザで、author が merge | **GitHub 上で**、他人が merge |
| QA の厳しさ | `smoke` — 3〜5 ケース | `standard` — 5〜10 | `strict` — 8〜12 |
| `review-pr` の effort | `low` | `medium` | `high` |

各 profile は *あなた* にも何かを期待する — ticket が始まるときどの branch にいるべきか、そしてあなたのローカル base が何のためのものか。その contract と、各 profile で ticket を並列に回す方法は **[docs/profiles.md](docs/profiles.md)** にある。

**一度も設定していない repo は `pet` に解決される** — branch も review の場もなく、作業はあなたが立っている branch に乗る。素の git を走らせるのと同じだ。儀式は repo が求めるものであって、勝手に与えられるものではない。

**初めてなら、あるいは迷っているなら `startup` を選べ。** 大事な repo で始めるのに安全な場所だ: standard な QA の厳しさで、あなたが開く PR なしに remote へ届くものは何もない。profile の変更は後から 1 行でできる。

厳しさが変えるのは **広さであって、基準ではない**。`PASS` は 3 つすべてで同じ意味だ: 該当する rubric dimension がすべて B 以上で、最終 code に対して新しく end-to-end で走らせていること。pet プロジェクトはケースが少ないだけ — ゼロにはならず、C 評価の dimension で通ることもない。

`mode:`、`land:`、`push:` を手で設定すると profile の preset を上書きする。それは escape hatch であって通常の形ではない — 手で書き出した knob はその profile を追わなくなる。

#### 並列 ticket は要求するものであって、設定するものではない

**どの profile も branch を切らないし、あなたを worktree に移さない。** 3 つすべてで、あなたは自分が立っている branch で作業する。babysit なしの場合とまったく同じだ — profile が決めるのは review の場と QA の厳しさで、作業がどこに住むかではない。これは意図的だ。あなたの作業を黙って移すツールは、あなたが管理できないツールだ。

分離は、本当に欲しいときに run ごとに要求する:

- `/bbs:foreman` — batch。foreman が ticket ごとに 1 つの worktree を作る。どんな repo でも、config が何と言っていようと関係ない。
- `bbs ticket ensure --slug-hint <slug> --mode=worktree` — この 1 ticket だけを自前の worktree に。その中で autopilot を回す。
- `bbs ticket ensure --slug-hint <slug> --mode=branch` — その場で `feat/<id>_<slug>` を切る。

**worktree にはコストがかかる。何を買ったのかは分かっておけ。** 内側の loop はもう 0 ステップではない。code は worktree にあり、dev server は primary checkout を serve するので、テストの 1 反復は edit-and-refresh ではなく commit と `bbs ticket surface compose` になる。その代わりに手に入るのは分離できる ticket だ — 1 つを単独でレビューし、ダメなものを捨て、それぞれを独立したきれいな PR として land できる。毎回その形が欲しい repo は `mode: worktree` + `land: local` を手で書けばいい。誰もあなたの代わりには書いてくれない。

worktree と shared surface の間で作業を動かすコマンド — `bbs ticket surface <acquire|compose|revert|release|status>`、加えて人間向けの層 `board`、`serve`、`/bbs:fix-pr` — は [ticket を並列で回す](#ticket-を並列で回す-worktree-mode) にある。詳細: [`references/git-flow.md`](.claude/skills/references/git-flow.md) と [`references/worktrees.md`](.claude/skills/references/worktrees.md)。

### 3. 実行する

**ここから始める — `autopilot`、single-ticket flow:**

```
/bbs:autopilot "add a settings page with dark mode toggle"
```

autopilot は ticket を init する — requirement、plan — それから止まり、`/goal` ブロックを **最後のメッセージ** として出力する。そのブロックが、あなたが次にやる唯一のことだ: **コピーして、同じ agent に貼り戻して、席を立つ。** すると goal session が code を書き、レビューし、QA を回し、あなたが立っている branch に作業を commit する — autopilot は決して branch を切らず、push せず、PR を開かない。PR はレビューのあと自分で開く。plan と UI prototype を作る native model を選ぶには `--planner gpt-5.6-sol --planner-effort high` を渡す。これらの flag を省くと、autopilot がタスクの難易度と、現在の harness が advertise している capability から選ぶ。

> **handoff はこんな形だ** — autopilot は平易な言葉の前置きで終わり、そのあとにコピーするブロックが続く:
>
> ```
> bs-ab123 の準備完了。貼る前に、何を作るのかを確認しろ:
>   plan:      tickets/bs-ab123/plan.md
>   prototype: tickets/bs-ab123/prototype.html
> 設計が違うなら今すぐ差し戻せ — そうでなければ、あとは 1 回貼るだけで終わる。
>
> 👉 下のブロックをコピーして、あなたの agent に貼れば build が始まる:
>
> /goal bs-ab123 is done: work committed locally, qa verdict PASS/FIXED persisted
> via bbs ticket set-verdict, review-pr verdict persisted, handoff note written — or a
> NEEDS_CONTEXT / BLOCKED status block printed verbatim.
> Work it: /bbs:autopilot builder bs-ab123
> ```

#### なぜ `/goal` が作業を所有するのか

`/goal <condition>` は、対応 agent の persistent goal mode を開始する: model は full context で自由に働き — ステップの儀式はなく — 条件が満たされるまで続ける。だからこのステップは「コマンドを実行する」ではなく *`/goal` ブロックを貼る* なのだ。貼ることが goal を起動する。autopilot が出力するブロックには、babysit の gate と escape clause がすでに織り込まれている。

escape clause のおかげで、loop は欠けた入力を相手に削り続けるのではなく、escalation で終了する。途中で抜けるには: `/goal clear`、`Ctrl-C`、または `~/.babysit/projects/<slug>/tickets/<ticket>/STOP` を touch する。

`/goal` なしでも、`/bbs:autopilot bs-ab123` を再呼び出しすれば checkpoint から再開する — session の境界を手で押し越すだけだ。

#### 発展 — `foreman`、自律プロジェクトの orchestrator

requirement が複数 ticket にまたがったら、`foreman` がプロジェクト全体を所有する。各 worker はあなたがすでに知っている autopilot を走らせるが、checkout を用意し DAG を調整するのは foreman だ:

```
/bbs:foreman <large project requirement>  # 分解、スケジュール、検証、finish
/bbs:foreman                              # ticket + Orca state から attach/resume
```

foreman は 1 つの親プロジェクトと、範囲を限定された子 ticket を作り、依存 edge を記録し、監督下の worker を Orca orchestration 経由で起動する。子はすべて plan-only の Dispatch、自律 design gate、そして build/QA Dispatch を受け取る。各 worker は、その ticket の難易度が稼ぐ model で起動される — `simple` / `normal` / `critical` の tier は、autopilot の planner も使う共有の [model-routing table](.claude/skills/references/model-routing.md) から読まれ、解決された model は子に記録される。ほぼすべての ticket は routine の段 (`gpt-5.6-sol` / `opus` / `@default`) で走る。コストの高い最上位の段 (`gpt-6-astra`、Fable 5.1) には table の escalation trigger が要るので、batch が黙ってそこまで上がることはない。Foreman は verdict を disk から検証し、共有の test surface を直列化し、ticket が相互作用するときは composed な統合 QA を回し、repo の `finish:` policy を依存順に該当する子へ適用する — `land` は base に merge し、`pr` は PR を開き、`review` は落ち着いた worker を release する — なので done の ticket がプロジェクトを待つことはない。pending の統合 gate に覆われた子は、それが通るまで land を保留する。プロジェクトの DAG は status と dispatch の返信とともに運ばれ、`bbs ticket dag <parent> --mermaid` が要求時に出力する。Orca の message と Dispatch id が pane-text の polling を置き換え、自動起動の watcher が停滞した coordinator をつつき、設定された間隔で status を再要求するので、再起動した session は会話 memory なしで再開する。

**hard prerequisite が 1 つあり、だからこれは 2 番目に学ぶべきものだ:** orchestration を有効にした [Orca](https://www.onorca.dev)。foreman は Orca にインストールされた version の一致する orchestration guide を読み込み、その runtime がなければ即座に失敗し、profile に関係なく子ごとに worktree を作る。厳しさと finish policy を決めるのは依然あなたの profile だ。直列の 1 ticket では `/bbs:autopilot` に対して何も得るものはない。

## 使い方

Babysit は変更を ship するための小さな assembly line だ。片端でアイデアを落とすと、もう片端でレビュー可能な branch を受け取る。line が止まるのは、あなたが実際に価値を足す 4 つの瞬間だけ。その間のすべては自分で進む。

### 止まる 4 つの場所

1. **「これは作るべきものか?」** — `requirement.md` が ready。あなたが読んで承認する。
2. **「これは正しい作り方か?」** — `plan.md` が ready。読んで、手を入れ、承認する。
3. **「実際に動くのか?」** — code が書かれ、レビューされ、QA で確認され、commit 済み。
4. **「これは PR にすべきか?」** — handoff をレビューして `/bbs:create-pr` を実行する。reviewer のコメントが来たら `/bbs:fix-pr` が片付ける。

### どこで止めるかを選ぶ

| どこで止まるか | 方法 |
|---------|-----|
| pause 1 — `requirement.md` が ready | `/bbs:autopilot "<idea>" --stop-after=requirement` |
| pause 2 — `plan.md` が ready | `/bbs:autopilot "<idea>" --stop-after=plan` |
| pause 3 — QA 済みの branch が ready | `/bbs:autopilot "<idea>"` *(end-to-end、デフォルト)* |
| pause 4 — PR handoff | human review のあとに `/bbs:create-pr` を実行 |

stage が終わると、ticket に `Next:` 行が付く — 次にやることをそのまま書いたものだ。`/bbs:autopilot bs-<id>` を再呼び出しすれば、probe した state から常に正しい次の stage を選ぶので、どの workflow を呼ぶか覚えておく必要はない。

### 3 つの入力の形

```
/bbs:autopilot "<one-line idea>"     # 新機能 — ticket を作り、end-to-end で走る
/bbs:autopilot bs-ab123              # 既存 ticket — state に応じて次の stage へ route する
/bbs:autopilot                       # resume — 解決された ticket の checkpoint から再開する
```

表面はこれで全部だ。3 つの flag がそれを広げる — より手前の checkpoint で止める `--stop-after=requirement|plan`、plan/prototype の作成を route する `--planner <model>` / `--planner-effort <effort>`。autopilot は常に、起動した checkout で作業する。分離は `bbs ticket ensure --mode=…` か `/bbs:foreman` であって、autopilot の flag ではない。verb token は存在しない。

### ticket を並列で回す (worktree mode)

**これが foreman の batch (または手動の `bbs ticket ensure --mode=worktree`) が与える形だ** — そういう run があなたに渡すコマンド。`/bbs:foreman` は batch 全体に対して同じ section を回す — dispatch、design gate、verdict check、composed surface — が、ここで作業するのに foreman は要らない。

repo ごとに 1 つの重い checkout が dev server を回し、ticket はそれぞれ自分の軽い worktree に住む。これで、誰かが ticket の実際に動いているところを見たい瞬間 *以外* はすべて並列になる — そしてその瞬間には 3 つのコマンドがある:
```bash
bbs ticket board            # 全 ticket を一覧: status、verdict、live session、PR、surface を握っているのは誰か
bbs ticket serve bs-ab123   # この ticket を、動いている dev server に載せて人間がレビューできるようにする
bbs ticket serve            # 引数なし: 完了した ticket (qa + review DONE) をすべて server 上に compose する
/bbs:fix-pr                 # reviewer のコメントが来たら: 未解決の thread を取得し、修正し、返信し、resolve する
```

**review loop。** 動いている機能をブラウザでレビューするのが最も長い必須ステップなので、babysit はそれを最も安く反復できるようにする:

1. ticket が pause 3 に達する — handoff の `Next:` 行が正確なコマンドを渡してくれる: `bbs ticket serve bs-ab123`。
2. `serve` は test surface を 4 時間保持し (agent の QA は行儀よくあなたの後ろに並ぶ)、動いている server を base + まさにこの ticket に切り替える — この repo で、そして ticket が両方にまたがるときは FE/BE の sibling repo でも **両方**。
3. ブラウザでレビューする。ticket の session に変更を頼む。session は自分の worktree で commit する。`serve` を再実行し (再入可能 — hold を更新し、surface を切り直す)、ブラウザを refresh する。満足するまで繰り返す。
   Orca ならこの loop 全体が 1 つの worktree に収まる: `orca tab create --url <qa url>` で動いているアプリが組み込みブラウザに入り、`orca file open-changed --mode diff --worktree path:<ticket-worktree>` で ticket の diff がその隣に開く。
4. 承認したら → `bbs ticket serve --release`、そのあと repo ごとに `/bbs:create-pr`。あとで reviewer のコメントが来たら → `/bbs:fix-pr`。
5. `bbs ticket board --pr` は merge された PR に印を付け、正確な cleanup コマンド (`surface revert`、`set-status done`) を出力する。

**1 ticket、2 repo** (frontend + backend にまたがる機能): `/bbs:setup-project` が sibling repo を 1 回記録すれば、autopilot の builder が自分で越境する — リンクされた sibling ticket を作り、両側を implement して QA する — そして `serve` が 1 コマンドでそのペア全体をあなたの前に出す。その間も他の ticket の session は自分の worktree で implement と review を続ける。`board` は surface を握っているのが誰で、どれだけの間かを表示する。レシピ全体: [`references/worktrees.md` § Attended parallel review](.claude/skills/references/worktrees.md)。

## さらに深く

- **Routing の内部とデバッグ** — init が何を蒔くか、`/goal` loop が checkpoint からどう再開するか、どの gate をどの step skill が所有するか: [`.claude/skills/autopilot/SKILL.md`](.claude/skills/autopilot/SKILL.md)。run が route する state を、走らせずに見るには: `bbs autopilot explain` (workflow の prereq matrix には `--details` を足す)。
- **Profiles** — [`docs/profiles.md`](docs/profiles.md): 各 profile があなたの base branch に何を期待するか、そして各 profile で ticket を並列に回す方法。
- **Config schema** — `.babysit/` を手書きするための [`.claude/skills/references/git-flow.md`](.claude/skills/references/git-flow.md) と [`docs/qa-config.md`](docs/qa-config.md)。

## Skill 一覧

`/bbs:autopilot` は下の skill を組み合わせて完全な workflow にする。部品だけが欲しいときは直接呼べ — よく使うもの:

| やりたいこと | Skill |
|------------|-------|
| 一行のアイデアから機能を end-to-end で ship する | `/bbs:autopilot "<idea>"` |
| 複数の ticket や feature からなる大きなプロジェクトを完遂する | `/bbs:foreman "<project>"` |
| 作り込むと決める前にアイデアを叩き潰す | `/bbs:office-hours` |
| 既存の UI system の中に機能を design する | `/bbs:design-ui` |
| requirement を `plan.md` にする (code は書かない) | `/bbs:plan-draft` |
| 承認済みの plan から build する | `/bbs:implement` |
| marketing copy や conversion を改善する | `/bbs:copy-rewrite`、`/bbs:conversion-fix` |
| growth 実験や short-form script を提案する | `/bbs:growth-experiment`、`/bbs:social-content` |
| URL や frontend flow をブラウザで確認する | `/bbs:browse` |
| ブラウザの test/fix loop を丸ごと回す | `/bbs:qa` |
| landing の前に branch をレビューする | `/bbs:review-pr` |
| bug を root-cause する | `/bbs:investigate` |
| この repo を autopilot 用に設定する | `/bbs:setup-project` |
| レビュー可能な pull request を作る | `/bbs:create-pr` |
| PR の review コメントを片付ける (fix、reply、resolve) | `/bbs:fix-pr` |

全 skill の表 (autonomous-ready / interactive-only の分類付き) は [`docs/skills.md`](docs/skills.md) にある。

## 付属 CLI

すべては `bbs <sub>` として届く 1 つの binary だ — `bbs autopilot` (runner)、`bbs ticket env` (identity resolver: `BABYSIT_TICKET` → manifest → branch)、加えて env、config、db snapshot、upgrade check の helper 群。`brew install lohi-ai/babysit/bbs` が `PATH` に入れる。checkout からなら `setup-skills` が build して代わりに `~/.local/bin/bbs` を symlink し、legacy な呼び出し元のために `bbs-*` の argv0 alias を `~/.claude/` に入れる。全表と目的は [`docs/companion-cli.md`](docs/companion-cli.md) にある。どれでも `bbs <sub> --help` で使い方を表示する。

## 運用

Day-2 config (`bbs config`)、telemetry (`~/.babysit/analytics/` への JSONL、デフォルトはローカルのみ)、update の扱い (`bbs update check` + `bbs update`) は [`docs/operations.md`](docs/operations.md) で扱っている。

**アップグレード。** 使っている agent の CLI と plugin のコピーを更新し、その agent を再起動する:

```bash
bbs update
```

babysit は別々のツールが所有する 2 つの半分として出荷される — brew の CLI と agent plugin だ。`bbs update` は CLI と、インストール済みの Claude Code・Codex の plugin コピーを駆動する。marketplace plugin は checkout から読み込まれるのではなく cache されるので、checkout を pull しただけではインストール済みのコピーは更新されない。

## アンインストール

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
rm -rf ~/.babysit          # あなたの ticket と analytics — 残したいなら飛ばす
```

checkout から入れた場合は、消す前に `./bin/setup-skills --uninstall` も実行する。plugin 以前のインストールから legacy symlink が残っている場合の手動 cleanup:

```bash
find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete
rm -f ~/.claude/babysit ~/.claude/bbs-*
```

## トラブルシューティング

| 症状 | 対処 |
|-------|-----|
| `git push` がすべて拒否される、"GATE OFFLINE" | `PATH` に `bbs` がない — `brew install lohi-ai/babysit/bbs`。plugin にコンパイル済み binary は入っておらず、gate は設計上 fail closed する |
| update 後に skill が消えた、または古い | `bbs update` を実行し、影響を受けた agent を再起動する |
| Claude Code で `/bbs:*` が見つからない | `claude plugin install bbs@babysit` を実行して再起動する。または `/reload-plugins` |
| Codex で `$bbs:*` が見つからない | `codex plugin add bbs@babysit` を実行し、新しい session を始める |
| skill が `bbs:` prefix なしで表示される | legacy install — `find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete` を実行し、plugin を入れ直す |
| `env resolve` が空を返す | `config/<app>/` の下に正しい `.env.base` があるか確認する |

## ライセンス

MIT.
