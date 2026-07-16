# babysit

[English](README.md) | Tiếng Việt

**Giao một loạt yêu cầu. Worker xây song song ngay trước mắt bạn. Review một bản gộp duy nhất.**

```
/bbs:foreman product-wide search on the home page
/bbs:foreman rebuild the novel request flow
```

**`foreman` là luồng chính**: mỗi yêu cầu một worker tmux nhìn thấy được (mỗi worker chạy autopilot trọn gói — plan, code, review, QA, push), design được duyệt trước khi viết dòng code nào, và mọi ticket xong đều được merge lên base local để bạn review cả lô đang chạy trong một trình duyệt — rồi mới tạo PR.

Với **một ticket lẻ**, gọi thẳng autopilot (cũng chính là thứ mỗi worker chạy):

```
/bbs:autopilot add a settings page with dark mode toggle
```

~40 phút tự chạy mỗi ticket — mà vẫn xong xuôi, dù chẳng session Claude nào ôm nổi ngần ấy việc trong một hơi. Bạn ngó lại branch, ưng thì tự bấm mở PR.

*babysit là việc bạn làm khi khỏi cần ai trông.* Nó chuộng mấy quyết định Claude tự làm tự kiểm được, hơn là mấy quyết định phải có người ngồi kè kè — đẻ ra cho các lần chạy theo lịch, pipeline được điều phối, và bất cứ thứ gì bạn muốn giao rồi đi chơi.

## Vì sao nó chạy được

- **Nó chạy tới cùng.** `/bbs:autopilot` là một **goal proxy**: phần init gieo state bền — ticket, requirement, plan, checkpoint — rồi giao phần việc cho [`/goal`](#3-chạy), cái Stop hook theo session của Claude Code chặn không cho session dừng chừng nào verdict QA và review chưa được ghi lại. Trong vòng lặp, model làm việc thoải mái với đầy đủ context, y như khi bạn hỏi thẳng nó; checkpoint trên disk giúp một session mới nối tiếp đúng chỗ con cũ dừng.
- **Nó không treo.** Mọi quyết định đều đi qua [Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md). Claude quyết rồi ghi log; nếu thật sự cần người, nó viết một block `NEEDS_CONTEXT` vào ticket chứ không ngồi đợi một cái pop-up.
- **Nó tự kiểm.** QA nằm sẵn trong vòng lặp mặc định của autopilot. Muốn PASS thì phải có target chạy được ở local hoặc một blocker gọi tên rõ ràng, kèm thêm mấy ca không-suôn-sẻ. Không có cái kiểu "compile được là ship".
- **Nó soi lại được.** Telemetry dạng JSONL đổ vào `~/.babysit/analytics/`, cộng với mấy comment checkpoint `[WORK]`. Xem lại băng sau cũng được — đây là kênh feedback chính khi chẳng ai ngồi coi trực tiếp.

## Năm archetype

Khi engineering, product, design và data science tan vào nhau thành một kiểu
người dựng sản phẩm, đơn vị công việc đáng nói tới không còn là chức danh nữa —
mà là cái *archetype* việc đang cần ngay lúc đó. Babysit chính là cái team dựng
sản phẩm ấy: nó ánh xạ năm archetype của team Claude Code vào các skill và
workflow autopilot, nên một lần chạy đóng được vai bất kỳ đồng đội nào việc cần.

Một người ôm 2–3 archetype; một lần chạy babysit cũng vậy. Chọn theo **hình dạng
của việc**, đừng theo "loại" của file. Mỗi archetype có đúng **một workflow
autopilot**.

| Archetype | Nhiệm vụ | Dùng khi | Workflow |
|-----------|----------|----------|----------|
| **Prototyper** | Đẻ ý tưởng mới toanh; phần lớn sẽ không ship. Học nhanh một điều. | Chưa có gì trong tay, mới chỉ là linh cảm — kiểm chứng trước khi lao vào dựng. | `prototyper` |
| **Builder** | Biến một prototype/ý tưởng thành sản phẩm và infra đạt chuẩn production. | Đã có ý tưởng được kiểm chứng hoặc plan được duyệt. Mặc định cho việc làm feature mới. | `builder` |
| **Sweeper** | Dọn UI, làm gọn code và hệ thống, gỡ tính năng, tối ưu. | Code đang gánh những thứ nó không cần — dead code, trùng lặp, over-abstraction. Bớt đi; hành vi phải giữ nguyên. | `sweeper` |
| **Grower** | Lặp trên một sản phẩm đã ship để cải thiện product-market fit. | Sản phẩm đã ship nhưng funnel yếu. Đo trước, rồi chạy một experiment đảo ngược được. | `grower` |
| **Maintainer** | Giữ một hệ thống trưởng thành an toàn, tin cậy, nhanh và tiết kiệm ở quy mô lớn. | Một hệ thống trưởng thành đang chịu áp lực production/scale — tải, bảo mật, độ tin cậy, chi phí. | `maintainer` |

**Sweeper vs Maintainer** — cặp dễ nhầm nhất, vì cả hai đều đụng tới
performance. Sweeper tối ưu *codebase* (bớt độ phức tạp, hành vi giữ nguyên
từng byte; perf tự nhiên có thêm) và được kích hoạt bởi đống cruft tích lại.
Maintainer tối ưu *hệ thống đang chạy production* (giữ nó sống dưới tải thật;
đổi caching/indexing/data-model có thể đổi timing) và được kích hoạt bởi scale,
bảo mật, hoặc chi phí. "Một feature ổn định, nhiều người xài" là trigger của
Maintainer; nếu nó còn cần dọn cấu trúc, chạy Sweeper thành một pass riêng giữ
nguyên hành vi.

**Những bất biến mọi archetype đều giữ** — chúng chỉ khác nhau ở *nhiệm vụ và
tiêu chí thành công*, không bao giờ khác về độ chặt: quyết định đi qua
[Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md)
(quyết định taste được ghi log, không bao giờ đoán lén); tự kiểm trước khi
"xong"; bán kính sát thương có giới hạn (không force-push, không mất dữ liệu,
không nhắn ra ngoài nếu chưa có phép bền); fail lớn tiếng và tại chỗ
(`BLOCKED`/`NEEDS_CONTEXT` còn hơn một giả định sai).

Chi tiết và các skill mỗi archetype ghép lại:
[`.claude/skills/references/archetypes.md`](.claude/skills/references/archetypes.md).

## Bắt đầu nhanh

Ba bước. Cài một lần cho toàn máy, cấu hình mỗi repo một lần, rồi chạy.

### 1. Cài plugin

**Nhanh nhất — để Claude Code làm hộ.** Dán đoạn này vào Claude Code, nó sẽ clone repo, chạy trình cài đặt, rồi chỉ cho bạn từng bước còn lại:

```
Cài plugin babysit giúp mình: clone https://github.com/lohi-ai/babysit.git
vào ~/.claude/skills/babysit, chạy ./bin/setup-skills --full, rồi liệt kê chính xác các
bước tiếp theo mình cần tự chạy (các lệnh /plugin, cấu hình repo, chạy lần đầu).
```

Claude lo phần clone + `setup-skills`; các lệnh `/plugin` bên dưới là slash command bạn tự chạy, nên nó sẽ trả lại cho bạn dưới dạng hướng dẫn từng bước.

**Hoặc làm thủ công:**

```bash
git clone --single-branch --depth 1 https://github.com/lohi-ai/babysit.git ~/.claude/skills/babysit
cd ~/.claude/skills/babysit
./bin/setup-skills --full
```

Rồi trong Claude Code:

```
/plugin marketplace add ~/.claude/skills/babysit
/plugin install bbs@babysit
```

Yêu cầu: Claude Code có hỗ trợ plugin, Git.

### 2. Cấu hình project của bạn

Trong bất kỳ repo nào bạn muốn autopilot ship từ đó:

```
/bbs:setup-project
```

Wizard viết ra config `.babysit/` nhỏ nhất mà vẫn đủ xài: `git-flow.yaml` với `base_branch`, `branch_prefix`, `push`, `mode`, và `qa.yaml` với `url`, `start`, `check`, `flows`. Chạy lại thì idempotent.

#### Git flow: chọn một trong ba mode

`mode:` trong `.babysit/git-flow.yaml` quyết định branch của mỗi ticket nằm ở đâu — đây là thuộc tính của repo, đặt một lần:

| Mode | Hình dạng | Hợp với |
|------|-----------|---------|
| `trunk` | không cắt branch — ticket đi chung trên branch dùng chung (vd. `develop`); danh tính đi theo `BABYSIT_TICKET` | repo cá nhân: nhiều session trong một thư mục, một dev server test hết cùng lúc |
| `branch` *(mặc định)* | cắt `feat/<id>_<slug>` ngay tại chỗ khi checkout là base sạch; tự né sang worktree khi không sạch | PR sạch một-ticket với việc phần lớn tuần tự — QA rẻ nhất, server phục vụ thẳng branch của ticket |
| `worktree` | mỗi ticket có worktree riêng; checkout chính ghim ở base làm bề mặt test dùng chung | repo team/doanh nghiệp: nhiều ticket song song, mỗi cái một PR sạch |

Ở mode `worktree`, QA đưa một ticket lên bề mặt dùng chung bằng `bbs-ticket merge-base` (chạy từ worktree), hoặc nhảy bề mặt qua lại giữa các ticket bằng `bbs-ticket switch <ticket>...` (chạy từ checkout chính — reset về base, rồi merge đúng các ticket được gọi tên). Sau khi các PR merge lên upstream, `bbs-ticket reset-base` kéo base local về lại origin. Cả ba đều từ chối lớn tiếng thay vì làm mất việc. Lớp dành cho người ngồi trên cùng — `board`, `serve`, `/bbs:fix-pr` — nằm ở mục [Làm nhiều ticket song song](#làm-nhiều-ticket-song-song-mode-worktree). Chi tiết: [`references/git-flow.md`](.claude/skills/references/git-flow.md).

### 3. Chạy

**Luồng chính — `foreman`, chạy song song có người trông:**

```
/bbs:foreman <yêu cầu một dòng>     # mỗi yêu cầu một worker; lặp lại để giao thêm
/bbs:foreman                               # attach/resume: điểm danh worker đang sống + board
```

Foreman mở một worker tmux cho mỗi ticket (`tmux attach -t <session>` để xem hoặc tự lái bất kỳ worker nào), theo dõi các pane, và giữ chốt chặn giữa design và build: khi một worker dừng ở bản bàn giao plan/prototype, foreman review design, góp ý, rồi hoặc bật đèn xanh cho build hoặc hỏi bạn khi tiếng nói của bạn có thể đổi hướng kết quả. Nó tự trả lời các câu hỏi máy móc của worker, chuyển cho bạn những câu cần bạn, kiểm chứng mọi verdict QA/review trên đĩa, và — với `land: local` (mặc định ở mode worktree) — merge hết các ticket xong lên base local để bạn review sản phẩm gộp trên dev server trước khi quyết: PR từng ticket hay một compose PR.

**Luồng phụ — `autopilot`, cho một ticket lẻ** (cũng là thứ mỗi worker của foreman chạy):

```
/bbs:autopilot "add a settings page with dark mode toggle"
```

Autopilot init ticket — requirement, plan, branch — rồi dừng lại và in ra một block `/goal` làm **tin nhắn cuối cùng**. Block đó chính là việc duy nhất bạn làm tiếp theo: **copy nó, dán lại vào Claude Code, rồi đi chơi.** Session goal sẽ viết code, review, chạy QA, và push branch. Review xong thì tự mở PR.

> **Bản bàn giao trông như vầy** — autopilot kết thúc bằng một đoạn dẫn bằng lời thường, rồi tới block để copy:
>
> ```
> Ready for bs-ab123. Before you paste, review what will be built:
>   plan:      tickets/bs-ab123/plan.md
>   prototype: tickets/bs-ab123/prototype.html
> Redirect the design now if it's wrong — otherwise you're one paste from done.
>
> 👉 Copy the block below and paste it into Claude Code to build it:
>
> /goal bs-ab123 is done: qa verdict PASS/FIXED persisted via bbs-ticket set-verdict,
> review-pr verdict persisted, branch pushed, handoff note written — or a
> NEEDS_CONTEXT / BLOCKED status block printed verbatim.
> Work it: /bbs:autopilot builder bs-ab123
> ```

#### Vì sao `/goal` nắm phần việc

`/goal <condition>` (có sẵn, Claude Code 2.1.139+) gắn một Stop hook theo session: model làm việc thoải mái với đầy đủ context — không nghi thức từng bước — và cái hook chặn không cho dừng chừng nào điều kiện chưa thỏa. Đó là lý do bước tiếp theo là *dán block `/goal`* chứ không phải "chạy một lệnh": chính việc dán nó là cái gắn hook lên. Block autopilot in ra đã gói sẵn các cổng gác của babysit lẫn điều khoản thoát.

Điều khoản thoát là chỗ chịu lực: vòng lặp kết thúc khi cần leo thang, thay vì nghiến răng cày mãi vào một input còn thiếu. Muốn thoát giữa chừng: `/goal clear`, `Ctrl-C`, hoặc touch `~/.babysit/projects/<slug>/tickets/<ticket>/STOP`.

Không có `/goal`, gọi lại `/bbs:autopilot bs-ab123` vẫn nối tiếp từ checkpoint — chỉ là bạn phải tự tay đẩy nó qua ranh giới giữa các session.

## Cách dùng

Babysit là một dây chuyền nhỏ để ship một thay đổi. Bạn thả ý tưởng vào đầu này, nhặt ra một branch sẵn sàng review ở đầu kia. Dây chuyền dừng đúng bốn chỗ mà bạn thật sự thêm được giá trị; khúc giữa nó tự lo.

### Bốn chỗ nó dừng

1. **"Có phải đây là thứ đáng làm không?"** — `requirement.md` sẵn sàng. Bạn đọc và duyệt.
2. **"Có phải đây là cách làm đúng không?"** — `plan.md` sẵn sàng. Bạn đọc, chỉnh, duyệt.
3. **"Nó có chạy thật không?"** — code đã viết, đã review, đã QA, đã push.
4. **"Có nên biến thành PR không?"** — bạn review bản handoff rồi chạy `/bbs:create-pr`; khi reviewer để lại comment, `/bbs:fix-pr` xử lý từng cái.

### Chọn chỗ nó dừng

| Dừng ở | Cách |
|--------|------|
| chặng 1 — `requirement.md` sẵn sàng | `/bbs:autopilot "<idea>" --stop-after=requirement` |
| chặng 2 — `plan.md` sẵn sàng | `/bbs:autopilot "<idea>" --stop-after=plan` |
| chặng 3 — branch đã QA sẵn sàng | `/bbs:autopilot "<idea>"` *(đầu-tới-cuối, mặc định)* |
| chặng 4 — bàn giao PR | chạy `/bbs:create-pr` sau khi người review |

Mỗi khi một stage xong, ticket có thêm một dòng `Next:` — đúng nghĩa đen là làm gì tiếp. Gọi lại `/bbs:autopilot bs-<id>` luôn chọn đúng stage kế tiếp từ state dò được, nên bạn không bao giờ phải nhớ gọi workflow nào.

### Ba kiểu input

```
/bbs:autopilot "<ý tưởng một dòng>"   # feature mới — tạo ticket + branch, chạy đầu-tới-cuối
/bbs:autopilot bs-ab123              # ticket có sẵn — state-route tới stage kế tiếp
/bbs:autopilot                       # resume — nối lại từ checkpoint của branch hiện tại
```

Cả bề mặt chỉ có vậy. Các flag (`--stop-after=`, `--replan`, `--dry-run`, `--workflow=<name> --force`) mở rộng thêm; không có token động từ nào cả.

### Làm nhiều ticket song song (mode `worktree`)

`/bbs:foreman` chạy giùm bạn nguyên mục này — dispatch, chốt design, kiểm verdict, và bề mặt gộp cuối cùng. Các lệnh bên dưới là tầng bên dưới, cho lúc bạn tự lái hoặc muốn hiểu foreman đang làm gì.

Mỗi repo có một checkout nặng chạy dev server; mỗi ticket sống trong worktree nhẹ riêng của nó. Nhờ vậy mọi thứ chạy song song được hết — *trừ* cái khoảnh khắc có người cần thấy một ticket đang chạy thật — và khoảnh khắc đó có đúng ba lệnh:

```bash
bbs-ticket board            # toàn bộ ticket trong một cái nhìn: status, verdict, session đang sống, PR, ai đang giữ bề mặt
bbs-ticket serve bs-ab123   # đưa ticket này lên dev server đang chạy cho người review
bbs-ticket serve            # để trống: gộp mọi ticket đã xong (qa + review DONE) lên server
/bbs:fix-pr                 # khi reviewer để lại comment: kéo các thread chưa resolve, sửa, trả lời, resolve
```

**Vòng lặp review.** Review feature đang chạy thật trong browser là bước bắt-buộc tốn thời gian nhất, nên babysit làm nó thành bước rẻ nhất để lặp lại:

1. Một ticket chạm chặng 3 — dòng `Next:` trong bản handoff đưa tận tay lệnh cần gõ: `bbs-ticket serve bs-ab123`.
2. `serve` giữ bề mặt test trong 4 tiếng (QA của các agent lịch sự xếp hàng sau bạn) và chuyển server đang chạy sang base + đúng ticket này — ở repo này **và** ở repo FE/BE anh em khi ticket trải qua cả hai.
3. Review trong browser. Nhờ session của ticket sửa; nó commit trong worktree của riêng nó; chạy lại `serve` (reentrant — làm mới thời gian giữ, cắt lại bề mặt) rồi refresh browser. Lặp tới khi ưng.
4. Ưng rồi → `bbs-ticket serve --release`, rồi `/bbs:create-pr` cho từng repo. Reviewer comment sau đó → `/bbs:fix-pr`.
5. `bbs-ticket board --pr` chỉ ra các PR đã merge và in đúng các lệnh dọn dẹp (`reset-base`, `set-status done`).

**Một ticket, hai repo** (feature trải cả frontend + backend): `/bbs:setup-project` ghi lại các repo anh em một lần; builder của autopilot tự băng qua — tạo ticket anh em đã liên kết, code và QA cả hai bên — và `serve` bày cả cặp ra trước mặt bạn bằng một lệnh. Trong lúc đó session của các ticket khác vẫn code và review trong worktree riêng của chúng; `board` cho cả nhà thấy ai đang giữ bề mặt và giữ bao lâu nữa. Công thức đầy đủ: [`references/git-flow.md` § Attended parallel review](.claude/skills/references/git-flow.md).

## Đào sâu hơn

- **Ruột routing & debug** — Parse → Probe → Assign → Dispatch, `bbs-autopilot explain`, `--dry-run`, các lối thoát `--replan` / `--force`: [`.claude/skills/autopilot/SKILL.md`](.claude/skills/autopilot/SKILL.md).
- **Schema config** — [`.claude/skills/references/git-flow.md`](.claude/skills/references/git-flow.md) và [`docs/qa-config.md`](docs/qa-config.md) để tự viết tay `.babysit/`.

## Danh mục skill

`/bbs:autopilot` ghép các skill dưới đây thành workflow đầy đủ. Gọi thẳng một cái khi bạn chỉ cần đúng mảnh đó — mấy bản hit:

| Tôi muốn… | Skill |
|-----------|-------|
| Giao nhiều yêu cầu chạy song song mà vẫn nhìn thấy được | `/bbs:foreman "<ý tưởng>"` |
| Vặn thử một ý tưởng trước khi quyết định làm | `/bbs:office-hours` |
| Thiết kế một feature trong hệ UI có sẵn | `/bbs:design-ui` |
| Ship một feature đầu-tới-cuối từ một ý tưởng một dòng | `/bbs:autopilot "<idea>"` |
| Biến một requirement thành `plan.md` (chưa code) | `/bbs:plan-draft` |
| Dựng từ một plan đã được duyệt | `/bbs:implement` |
| Cải thiện copy marketing hoặc conversion | `/bbs:copy-rewrite`, `/bbs:conversion-fix` |
| Đề xuất experiment tăng trưởng hoặc kịch bản video ngắn | `/bbs:growth-experiment`, `/bbs:social-content` |
| Kiểm một URL hoặc một flow frontend trong browser | `/bbs:browse` |
| Chạy full vòng lặp test/fix trên browser | `/bbs:qa` |
| Review một branch trước khi merge | `/bbs:review-pr` |
| Truy nguyên gốc một bug | `/bbs:investigate` |
| Cấu hình repo này cho autopilot | `/bbs:setup-project` |
| Tạo một pull request để review | `/bbs:create-pr` |
| Xử lý comment review trên PR (sửa, trả lời, resolve) | `/bbs:fix-pr` |

Bảng skill đầy đủ (kèm phân loại autonomous-ready / interactive-only) ở [`docs/skills.md`](docs/skills.md).

## CLI đi kèm

`setup-skills` symlink một nắm bin `bbs-*` vào `~/.claude/` — `bbs-autopilot` (bộ chạy), `bbs-slug` (resolver lấy branch làm mỏ neo), cộng các trợ giúp cho env, config, snapshot db, và kiểm tra upgrade. Bảng đầy đủ và mục đích ở [`docs/companion-cli.md`](docs/companion-cli.md). Chạy `<bin> --help` để xem cách dùng bất kỳ cái nào.

## Vận hành

Config ngày-2 (`bbs-config`), telemetry (JSONL đổ vào `~/.babysit/analytics/`, mặc định chỉ ở local), và xử lý upgrade (`bbs-update-check` + `bbs-upgrade`) nằm trong [`docs/operations.md`](docs/operations.md).

**Upgrade.** `cd ~/.claude/skills/babysit && git pull && ./bin/setup-skills`, rồi `/plugin marketplace update babysit` + `/reload-plugins` trong Claude Code.

## Gỡ cài

```
/plugin uninstall bbs@babysit
/plugin marketplace remove babysit
```

```bash
./bin/setup-skills --uninstall
rm -rf ~/.claude/skills/babysit ~/.babysit
```

Dọn tay nếu còn sót symlink cũ từ bản cài tiền-plugin:

```bash
find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete
rm -f ~/.claude/babysit ~/.claude/bbs-*
```

## Xử lý sự cố

| Vấn đề | Cách sửa |
|--------|----------|
| Skill biến mất sau khi upgrade | `cd ~/.claude/skills/babysit && git pull`, rồi `/reload-plugins` |
| `/bbs:*` không tìm thấy | `/plugin marketplace add ~/.claude/skills/babysit` + `/plugin install bbs@babysit`; hoặc `/reload-plugins` |
| Skill hiện ra mà thiếu tiền tố `bbs:` | Bản cài cũ — chạy `./bin/setup-skills`, rồi `/plugin install ~/.claude/skills/babysit` |
| `env resolve` trả về rỗng | Kiểm xem đúng file `.env.base` có nằm dưới `config/<app>/` không |

## Giấy phép

MIT.
