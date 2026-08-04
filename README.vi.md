# babysit

[English](README.md) | Tiếng Việt

**Giao một dòng. Nó plan, code, review, QA trong lúc bạn đi vắng. Bạn chỉ review một branch.**

```
/bbs:autopilot add a settings page with dark mode toggle
```

~40 phút tự chạy mỗi ticket — mà vẫn xong xuôi, dù chẳng session Claude nào ôm nổi ngần ấy việc trong một hơi. Bạn ngó lại branch, ưng thì tự bấm mở PR.

**Bắt đầu bằng `autopilot`.** Nó là cả sản phẩm gói trong một lệnh, chạy được trên terminal bất kỳ và repo kiểu gì cũng được, và cũng chính là thứ mà mỗi worker song song chạy — nên chẳng có gì bạn học ở đây là học phí bỏ đi.

**Rồi khi làm từng ticket một không còn đủ — `foreman`:**

```
/bbs:foreman product-wide search on the home page
/bbs:foreman rebuild the novel request flow
```

Mỗi yêu cầu một worker nhìn thấy được, nằm trong workspace cmux riêng ở sidebar (mỗi worker chạy autopilot trọn gói — plan, code, review, QA, push), design được duyệt trước khi viết dòng code nào, và mọi ticket xong đều được merge lên base local để bạn review cả lô đang chạy trong một trình duyệt — rồi mới tạo PR. Đây là luồng nâng cao: nó cần [cmux](https://cmux.com), và chỉ đáng công khi bạn có vài ticket độc lập để giao cùng lúc.

*babysit là việc bạn làm khi khỏi cần ai trông.* Nó chuộng mấy quyết định Claude tự làm tự kiểm được, hơn là mấy quyết định phải có người ngồi kè kè — đẻ ra cho các lần chạy theo lịch, pipeline được điều phối, và bất cứ thứ gì bạn muốn giao rồi đi chơi.

## Cách dễ nhất để thử

Nếu bạn là dev chính của một team nhỏ, đây là trọn vòng lặp — nó cày, bạn giữ chốt. Đọc từ trên xuống như một chương trình:

```bash
/bbs:setup-project                        # một lần mỗi repo — branch + mặc định QA
/bbs:autopilot "thêm nút bật dark-mode"   # việc gì cũng được: nó plan → code → review → QA
#   → đọc tickets/<id>/plan.md, rồi dán block /goal nó in ra và đi chơi
#   → autopilot viết code, review, chạy QA, push branch
#   → bạn review bằng chứng, rồi:
/bbs:create-pr                            # bạn tự mở PR — autopilot không bao giờ tự mở
```

Từng bước:

- **`/bbs:setup-project`** một lần — dạy autopilot cách đặt tên branch + mặc định QA của bạn, để mọi thứ phía sau đều tất định.
- **`/bbs:autopilot "<yêu cầu một dòng>"`** cho mọi việc nhiều bước. Nó checkpoint xuống disk (sống sót qua crash và context bị nén), rồi plan → code → review → QA, và dừng ở chốt PR để bạn review trước khi có gì merge.

**Các chốt của con người — nơi bạn giữ quyền.** Autopilot chỉ dừng ở những khoảnh khắc thật sự thuộc về bạn; chọn khoảnh khắc nào bằng flag:

- `--stop-after=plan` — duyệt hướng đi trước khi viết một dòng code.
- *mặc định* — dừng ở trạng thái QA-xong, bạn review bằng chứng.
- **`/bbs:create-pr`** — bạn tự gọi; autopilot không bao giờ tự mở PR.

**Thêm khi cần:**

- **`/bbs:review-pr`** (tức `/code-review`) — một chốt trước khi merge, vì team nhỏ không có người review thứ hai. Đây là lưới an toàn của bạn.
- **`/bbs:foreman`** — luồng nâng cao: một worker hiện hình cho mỗi ticket (workspace cmux), nhiều ticket độc lập cùng lúc. Đụng tới khi vòng lặp trên đã quen tay và bạn có nguyên một mẻ để giao; thừa thãi với việc solo, tuần tự.

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

**Thẳng từ GitHub — không có gì rơi vào workspace của bạn.** Claude Code tự
clone cái marketplace:

```bash
brew install lohi-ai/babysit/bbs        # phần CLI — bắt buộc, xem bên dưới
claude plugin marketplace add lohi-ai/babysit
claude plugin install bbs@babysit
```

Khởi động lại Claude Code là có `/bbs:autopilot`. Sau này nâng cấp bằng một
lệnh duy nhất — `bbs upgrade` làm mới cả hai nửa, rồi khởi động lại Claude Code:

```bash
bbs upgrade
```

**`brew install bbs` không phải tùy chọn.** `bin/bbs` là sản phẩm build, không
được commit, nên plugin cài từ GitHub không kèm binary nào cả. Thiếu `bbs` trên
`PATH` thì cổng gác push/PR không đọc được verdict của ticket, và nó fail đóng —
mọi `git push` đều bị chặn. Người dùng Linux lấy bản tarball:
[docs/install.md](docs/install.md).

<details>
<summary><b>Hoặc cài từ một checkout</b> — nếu bạn muốn đọc hoặc sửa chính babysit</summary>

Checkout là hình dạng duy nhất mà bạn sửa skill xong là có hiệu lực ngay, khỏi
cần publish. `setup-skills` build binary và nối dây mọi thứ:

```bash
git clone https://github.com/lohi-ai/babysit.git ~/src/babysit
cd ~/src/babysit
./bin/setup-skills --full
```

Rồi trong Claude Code:

```
/plugin marketplace add ~/src/babysit
/plugin install bbs@babysit
```

Cách này đã đặt `bbs` lên `PATH` tại `~/.local/bin/bbs` → checkout của bạn, nên
khỏi cần cài thêm bản Homebrew. Nâng cấp bằng `git pull && ./bin/setup-skills`.

Lưu ý: plugin từ marketplace được *copy* vào `~/.claude/plugins/cache/`, còn một
thư mục nằm dưới `~/.claude/skills/<tên>/` thì được nạp **tại chỗ** — chính cái
thứ hai mới làm cho sửa-là-thấy. Đừng chạy cả hai hình dạng cùng lúc: plugin
marketplace đã cài sẽ thắng khi trùng tên.

</details>

Yêu cầu: Claude Code có hỗ trợ plugin, Git.

Khuyến nghị, chưa cần ngay lúc đầu: **[cmux](https://cmux.com)**, một terminal sinh ra cho lối phát triển bằng agent. `/bbs:autopilot` và mọi thứ khác chạy trên terminal bất kỳ — cmux là **phụ thuộc cứng chỉ với `foreman`** (cũng như nút spawn trên dashboard), vì nó không còn backend nào khác: cmux cho mỗi worker song song một workspace riêng trên sidebar kèm status pill và badge thông báo, diff mở trong trình xem thật thay vì trôi qua trong pane, và app đang chạy nằm ngay ở split browser bên cạnh. Thiếu cmux thì `foreman` dừng ngay với thông báo cách cài.

#### `bbs` CLI là cái gì

Một binary Go duy nhất, mọi subcommand gọi dạng `bbs <sub>` — `ticket`,
`config`, `secrets`, `upgrade`, `design`, `dashboard`, `autopilot`, `foreman`.
Không còn dòng bash production nào.
Formula còn thả thêm hai alias argv0 là `bbs-config` và `bbs-env` — đúng hai
cái đó thôi, nên skill luôn gọi dạng có dấu cách.

Nó là nửa phần mà các skill gọi thay bạn: danh tính ticket, verdict, các nước cờ
git-flow, telemetry. Nó **không** chứa bộ skill — skill, workflow, và dữ liệu
DESIGN.md/CSV đến từ plugin. Vì vậy bản cài mới gồm hai lệnh, và thiếu nửa nào
cũng không chạy.

Ma trận nền tảng đầy đủ và đường tarball cho Linux: [docs/install.md](docs/install.md).

### 2. Cấu hình project của bạn

Trong bất kỳ repo nào bạn muốn autopilot ship từ đó:

```
/bbs:setup-project
```

Wizard viết ra config `.babysit/` nhỏ nhất mà vẫn đủ xài: `git-flow.yaml` với `profile` và `base_branch`, và `qa.yaml` với `url`, `start`, `check`, `flows`. Chạy lại thì idempotent — với repo đã cấu hình, chạy lại chính là cách đổi profile.

#### Git flow: chọn một profile

Chỉ có một cái núm. Wizard hỏi đúng một câu — **ở repo này, sai một lần thì trả giá bao nhiêu?** — vì đó mới là thứ một git flow thật sự trả lời, chứ không phải team đông bao nhiêu hay bạn ngồi canh sát tới đâu. Câu trả lời của bạn là `profile:`, mọi thứ còn lại suy ra từ đó:

| | `pet` | `startup` | `enterprise` |
|---|---|---|---|
| ai dùng | solo, dự án nghiệp dư | solo freelance / team nhỏ | team, codebase doanh nghiệp |
| ưu tiên | ship ngay | tốc độ release > chất lượng code | chất lượng code > tốc độ release |
| việc của ticket nằm đâu | không cắt branch — đi thẳng trên base | `feat/<id>_<slug>` | `feat/<id>_<slug>` |
| review diễn ra ở đâu | không ở đâu cả — push chính là release | **tại máy**, trong browser, tác giả tự merge | **trên GitHub**, người khác merge |
| độ gắt QA | `smoke` — 3–5 ca | `standard` — 5–10 | `strict` — 8–12 |
| effort `review-pr` | `low` | `medium` | `high` |

Mỗi profile cũng đòi hỏi vài thứ ở *bạn* — khi bắt đầu một ticket thì phải đang đứng ở branch nào, và base local dùng để làm gì. Bản giao kèo đó, cùng với cách chạy ticket song song trong từng profile, nằm ở **[docs/profiles.md](docs/profiles.md)**.

**Lần đầu dùng, hoặc chưa chắc? Chọn `startup`.** Đây vốn đã là thứ mà một repo không có key `profile:` tự suy ra, và là chỗ bắt đầu an toàn với một repo bạn còn quý: có branch và có PR nghĩa là không thứ gì lọt lên base branch mà chưa ai xem, còn QA thì giao lại cho bạn app đang chạy ngay trong browser. Đổi profile sau này chỉ tốn một dòng.

Độ gắt chỉ nới **bề rộng, không hạ cái ngưỡng**. Một `PASS` mang đúng một nghĩa ở cả ba mức: mọi chiều rubric áp dụng được phải từ B trở lên, và phải có một lượt chạy end-to-end tươi trên đúng code cuối cùng. Dự án nghiệp dư chạy ít ca hơn — chứ không phải không chạy ca nào, và cũng không bao giờ pass với một chiều điểm C.

Tự tay đặt `mode:`, `land:`, hay `push:` sẽ đè lên preset của profile. Đó là cửa thoát hiểm, không phải hình dạng thường ngày — một cái núm viết tay là một cái núm thôi bám theo profile của nó.

#### Chạy nhiều ticket song song là một trục *khác*

Để ý cái bảng trên thiếu gì: không có profile nào tên "song song". **Không profile nào phải trả cái giá của worktree.** Worktree tốn thêm một commit cộng một lần `merge-base` cho mỗi vòng test, và đổi lại đúng một thứ — cho phép nhiều ticket dùng chung một dev server. Nó không làm việc test sâu hơn. Nên cả ba profile đều giữ nguyên vòng lặp trong 0 bước (sửa → nhìn browser), còn `mode: worktree` là thứ **`/bbs:foreman` tự xin theo từng lần giao việc**, không phải thứ bạn cấu hình.

Nghĩa là: đừng chạy lại `/bbs:setup-project` để "chuyển sang song song". Hãy giao cả mẻ cho **`/bbs:foreman`**. Nó mở một worker hiện hình cho mỗi ticket, chốt review design trước khi viết một dòng code, và — với `land: local` — merge hết các ticket xong lên base local để bạn review cả mẻ trong một browser, rồi tự tay mở các PR.

Khi foreman đưa bạn vào mode worktree, QA đưa một ticket lên bề mặt dùng chung bằng `bbs ticket merge-base` (chạy từ worktree), hoặc nhảy bề mặt qua lại giữa các ticket bằng `bbs ticket switch <ticket>...` (chạy từ checkout chính — reset về base, rồi merge đúng các ticket được gọi tên). Sau khi các PR merge lên upstream, `bbs ticket reset-base` kéo base local về lại origin. Cả ba đều từ chối lớn tiếng thay vì làm mất việc. Lớp dành cho người ngồi trên cùng — `board`, `serve`, `/bbs:fix-pr` — nằm ở mục [Làm nhiều ticket song song](#làm-nhiều-ticket-song-song-mode-worktree). Chi tiết: [`references/git-flow.md`](.claude/skills/references/git-flow.md).

### 3. Chạy

**Bắt đầu ở đây — `autopilot`, luồng một ticket lẻ:**

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
> /goal bs-ab123 is done: qa verdict PASS/FIXED persisted via bbs ticket set-verdict,
> review-pr verdict persisted, branch pushed, handoff note written — or a
> NEEDS_CONTEXT / BLOCKED status block printed verbatim.
> Work it: /bbs:autopilot builder bs-ab123
> ```

#### Vì sao `/goal` nắm phần việc

`/goal <condition>` (có sẵn, Claude Code 2.1.139+) gắn một Stop hook theo session: model làm việc thoải mái với đầy đủ context — không nghi thức từng bước — và cái hook chặn không cho dừng chừng nào điều kiện chưa thỏa. Đó là lý do bước tiếp theo là *dán block `/goal`* chứ không phải "chạy một lệnh": chính việc dán nó là cái gắn hook lên. Block autopilot in ra đã gói sẵn các cổng gác của babysit lẫn điều khoản thoát.

Điều khoản thoát là chỗ chịu lực: vòng lặp kết thúc khi cần leo thang, thay vì nghiến răng cày mãi vào một input còn thiếu. Muốn thoát giữa chừng: `/goal clear`, `Ctrl-C`, hoặc touch `~/.babysit/projects/<slug>/tickets/<ticket>/STOP`.

Không có `/goal`, gọi lại `/bbs:autopilot bs-ab123` vẫn nối tiếp từ checkpoint — chỉ là bạn phải tự tay đẩy nó qua ranh giới giữa các session.

#### Nâng cao — `foreman`, chạy song song có người trông

Khi vòng lặp một-ticket đã quen tay, `foreman` chạy cả một mẻ. Từng ticket vẫn y hệt — mỗi worker chỉ là con autopilot bạn đã biết:

```
/bbs:foreman <yêu cầu một dòng>     # mỗi yêu cầu một worker; lặp lại để giao thêm
/bbs:foreman                        # attach/resume: điểm danh worker đang sống + board
```

Foreman mở một worker cho mỗi ticket — một workspace cmux bạn bấm ở sidebar để xem hoặc tự lái — theo dõi các pane, và giữ chốt chặn giữa design và build: khi một worker dừng ở bản bàn giao plan/prototype, foreman review design, góp ý, rồi hoặc bật đèn xanh cho build hoặc hỏi bạn khi tiếng nói của bạn có thể đổi hướng kết quả. Nó tự trả lời các câu hỏi máy móc của worker, chuyển cho bạn những câu cần bạn, kiểm chứng mọi verdict QA/review trên đĩa, và — với `land: local` (mặc định ở mode worktree) — merge hết các ticket xong lên base local để bạn review sản phẩm gộp trên dev server trước khi quyết: PR từng ticket hay một compose PR.

**Một thứ phải có trước, nên nó mới là bài học thứ hai:** [cmux](https://cmux.com) — thiếu là foreman dừng ngay. Bạn không cần cấu hình lại repo: foreman tự xin worktree theo từng lần giao việc, để các ticket song song không giành nhau một checkout, còn profile của bạn vẫn là thứ quyết định độ gắt. Với một ticket lẻ chạy tuần tự, nó chẳng hơn `/bbs:autopilot` chỗ nào.

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

Cả bề mặt chỉ có vậy. Hai flag mở rộng thêm — `--stop-after=requirement|plan` để dừng ở checkpoint sớm hơn, và `--mode=worktree` để chạy riêng ticket này trong worktree của nó. Không có token động từ nào cả.

### Làm nhiều ticket song song (mode `worktree`)

`/bbs:foreman` chạy giùm bạn nguyên mục này — dispatch, chốt design, kiểm verdict, và bề mặt gộp cuối cùng. Các lệnh bên dưới là tầng bên dưới, cho lúc bạn tự lái hoặc muốn hiểu foreman đang làm gì.

Mỗi repo có một checkout nặng chạy dev server; mỗi ticket sống trong worktree nhẹ riêng của nó. Nhờ vậy mọi thứ chạy song song được hết — *trừ* cái khoảnh khắc có người cần thấy một ticket đang chạy thật — và khoảnh khắc đó có đúng ba lệnh:

```bash
bbs ticket board            # toàn bộ ticket trong một cái nhìn: status, verdict, session đang sống, PR, ai đang giữ bề mặt
bbs ticket serve bs-ab123   # đưa ticket này lên dev server đang chạy cho người review
bbs ticket serve            # để trống: gộp mọi ticket đã xong (qa + review DONE) lên server
/bbs:fix-pr                 # khi reviewer để lại comment: kéo các thread chưa resolve, sửa, trả lời, resolve
```

**Vòng lặp review.** Review feature đang chạy thật trong browser là bước bắt-buộc tốn thời gian nhất, nên babysit làm nó thành bước rẻ nhất để lặp lại:

1. Một ticket chạm chặng 3 — dòng `Next:` trong bản handoff đưa tận tay lệnh cần gõ: `bbs ticket serve bs-ab123`.
2. `serve` giữ bề mặt test trong 4 tiếng (QA của các agent lịch sự xếp hàng sau bạn) và chuyển server đang chạy sang base + đúng ticket này — ở repo này **và** ở repo FE/BE anh em khi ticket trải qua cả hai.
3. Review trong browser. Nhờ session của ticket sửa; nó commit trong worktree của riêng nó; chạy lại `serve` (reentrant — làm mới thời gian giữ, cắt lại bề mặt) rồi refresh browser. Lặp tới khi ưng.
   Với cmux, cả vòng lặp này gói gọn trong một workspace: `cmux browser open <qa url>` đưa app đang chạy vào một split, `cmux diff --branch --repo <worktree> --base origin/main` mở diff đầy đủ của ticket bên cạnh, còn `cmux diff --last-turn` chỉ hiện những gì session vừa đổi ở lượt cuối.
4. Ưng rồi → `bbs ticket serve --release`, rồi `/bbs:create-pr` cho từng repo. Reviewer comment sau đó → `/bbs:fix-pr`.
5. `bbs ticket board --pr` chỉ ra các PR đã merge và in đúng các lệnh dọn dẹp (`reset-base`, `set-status done`).

**Một ticket, hai repo** (feature trải cả frontend + backend): `/bbs:setup-project` ghi lại các repo anh em một lần; builder của autopilot tự băng qua — tạo ticket anh em đã liên kết, code và QA cả hai bên — và `serve` bày cả cặp ra trước mặt bạn bằng một lệnh. Trong lúc đó session của các ticket khác vẫn code và review trong worktree riêng của chúng; `board` cho cả nhà thấy ai đang giữ bề mặt và giữ bao lâu nữa. Công thức đầy đủ: [`references/git-flow.md` § Attended parallel review](.claude/skills/references/git-flow.md).

## Đào sâu hơn

- **Ruột routing & debug** — init gieo những gì, vòng lặp `/goal` khôi phục từ checkpoint ra sao, và skill nào giữ cửa nào: [`.claude/skills/autopilot/SKILL.md`](.claude/skills/autopilot/SKILL.md). Muốn xem trạng thái mà một lần chạy sẽ route theo mà không chạy thật: `bbs autopilot explain` (thêm `--details` để ra ma trận prereq của workflow).
- **Profile** — [`docs/profiles.md`](docs/profiles.md): mỗi profile đòi hỏi gì ở base branch của bạn, và cách chạy ticket song song trong từng profile.
- **Schema config** — [`.claude/skills/references/git-flow.md`](.claude/skills/references/git-flow.md) và [`docs/qa-config.md`](docs/qa-config.md) để tự viết tay `.babysit/`.

## Danh mục skill

`/bbs:autopilot` ghép các skill dưới đây thành workflow đầy đủ. Gọi thẳng một cái khi bạn chỉ cần đúng mảnh đó — mấy bản hit:

| Tôi muốn… | Skill |
|-----------|-------|
| Ship một feature đầu-tới-cuối từ một ý tưởng một dòng | `/bbs:autopilot "<idea>"` |
| Giao nhiều yêu cầu chạy song song mà vẫn nhìn thấy được | `/bbs:foreman "<ý tưởng>"` |
| Vặn thử một ý tưởng trước khi quyết định làm | `/bbs:office-hours` |
| Thiết kế một feature trong hệ UI có sẵn | `/bbs:design-ui` |
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

Tất cả là một binary duy nhất, gọi dạng `bbs <sub>` — `bbs autopilot` (bộ chạy), `bbs ticket env` (resolver lấy branch làm mỏ neo), cộng các trợ giúp cho env, config, snapshot db, và kiểm tra upgrade. `brew install lohi-ai/babysit/bbs` đặt nó lên `PATH`; nếu cài từ checkout thì `setup-skills` build nó rồi symlink `~/.local/bin/bbs`, kèm các alias argv0 `bbs-*` vào `~/.claude/` cho các caller cũ. Bảng đầy đủ và mục đích ở [`docs/companion-cli.md`](docs/companion-cli.md). Chạy `bbs <sub> --help` để xem cách dùng bất kỳ cái nào.

## Vận hành

Config ngày-2 (`bbs config`), telemetry (JSONL đổ vào `~/.babysit/analytics/`, mặc định chỉ ở local), và xử lý upgrade (`bbs upgrade check` + `bbs upgrade`) nằm trong [`docs/operations.md`](docs/operations.md).

**Upgrade.** Một lệnh, rồi khởi động lại Claude Code — thay đổi plugin chỉ có hiệu lực sau khi khởi động lại:

```bash
bbs upgrade
```

babysit gồm hai nửa do hai công cụ khác nhau quản — CLI qua brew và plugin của Claude Code — và `bbs upgrade` chạy nửa nào máy này có, đồng thời nêu tên nửa nào nó không với tới được. Nếu cài từ checkout thì nó pull rồi chạy lại `setup-skills` cho nửa CLI, sau đó vẫn cập nhật plugin marketplace nếu máy có cài: checkout nằm trên `PATH` và plugin trong `~/.claude/plugins/cache/` là hai bản sao khác nhau, và bản Claude Code nạp chính là plugin đã cài.

## Gỡ cài

```
/plugin uninstall bbs@babysit
/plugin marketplace remove babysit
```

```bash
brew uninstall bbs
rm -rf ~/.babysit          # ticket và analytics của bạn — bỏ qua nếu muốn giữ
```

Nếu cài từ checkout, chạy thêm `./bin/setup-skills --uninstall` trước khi xóa nó. Dọn tay nếu còn sót symlink cũ từ bản cài tiền-plugin:

```bash
find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete
rm -f ~/.claude/babysit ~/.claude/bbs-*
```

## Xử lý sự cố

| Vấn đề | Cách sửa |
|--------|----------|
| Mọi `git push` đều bị chặn, báo "GATE OFFLINE" | Chưa có `bbs` trên `PATH` — `brew install lohi-ai/babysit/bbs`. Plugin không kèm binary, và cổng gác cố tình fail đóng |
| Skill biến mất hoặc cũ mèm sau khi upgrade | Khởi động lại Claude Code; vẫn cũ thì chạy lại `bbs upgrade` và đọc xem nó báo không với tới được nửa nào |
| `/bbs:*` không tìm thấy | `claude plugin install bbs@babysit`, rồi khởi động lại; hoặc `/reload-plugins` |
| Skill hiện ra mà thiếu tiền tố `bbs:` | Bản cài cũ — `find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete`, rồi cài lại plugin |
| `env resolve` trả về rỗng | Kiểm xem đúng file `.env.base` có nằm dưới `config/<app>/` không |

## Giấy phép

MIT.
