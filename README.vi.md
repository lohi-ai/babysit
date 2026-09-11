# babysit

[English](README.md) | Tiếng Việt

**Ném cho nó một dòng. Nó plan, code, review, QA trong lúc bạn đi vắng. Bạn chỉ review một branch.**

```
/bbs:autopilot add a settings page with dark mode toggle
```

Trong Codex, cùng skill đó được gọi là `$bbs:autopilot`; README này dùng cách
viết `/bbs:` của Claude Code trừ khi lệnh dành riêng cho Codex.

~40 phút tự chạy mỗi ticket — mà vẫn xong xuôi, dù chẳng session Claude nào ôm nổi ngần ấy việc trong một hơi. Bạn ngó lại branch, ưng thì tự bấm mở PR.

**Bắt đầu bằng `autopilot`.** Nó là cả sản phẩm gói trong một lệnh, chạy được trên terminal bất kỳ và repo kiểu gì cũng được, và cũng chính là thứ mà mỗi worker song song chạy — nên mọi thứ bạn học ở đây đều xài lại được.

**Rồi khi công việc lớn hơn một ticket — `foreman`:**

```
/bbs:foreman rebuild the novel request flow across web, API, and migrations
```

Foreman tách dự án thành đồ thị phụ thuộc, tạo branch và worktree cho từng ticket, rồi giám sát mỗi autopilot assistant bằng vòng đời Run/Task/Dispatch của Orca. Nó duyệt plan trước khi build, điều phối QA dùng chung và QA tích hợp, phục hồi từ state trên đĩa cộng với Orca, rồi áp dụng finish policy theo thứ tự phụ thuộc. Luồng này cần [Orca](https://www.onorca.dev); một ticket tuần tự thì gọi autopilot thẳng.

*babysit là việc bạn làm khi khỏi cần ai trông.* Nó chuộng mấy quyết định Claude tự làm tự kiểm được, hơn là mấy quyết định phải có người ngồi kè kè — đẻ ra cho các lần chạy theo lịch, pipeline được điều phối, và bất cứ thứ gì bạn muốn giao rồi đi chơi.

## Cách dễ nhất để thử

Nếu bạn là dev chính của một team nhỏ, đây là trọn vòng lặp — nó cày, bạn giữ chốt. Đọc từ trên xuống như một chương trình:

```bash
/bbs:setup-project                        # một lần mỗi repo — profile + mặc định QA
/bbs:autopilot "thêm nút bật dark-mode"   # việc gì cũng được: nó plan → code → review → QA
#   → đọc tickets/<id>/plan.md, rồi dán block /goal nó in ra và đi chơi
#   → autopilot viết code, review, chạy QA, commit lên branch bạn đang đứng
#   → bạn review bằng chứng, rồi:
bbs ticket serve bs-ab123                 # chỉ khi ticket chạy trong worktree riêng (foreman / --mode=worktree)
#   → xem nó chạy trong browser; muốn sửa thì bảo session của ticket, rồi chạy lại serve
/bbs:create-pr                            # bạn tự mở PR — autopilot không bao giờ tự mở
```

Code của ticket nằm ngay trên branch bạn đang đứng, nên dev server reload là thấy. `bbs ticket serve` dành cho hình dạng kia — ticket chạy trong worktree riêng (`bbs ticket ensure --mode=worktree`, hoặc một lô `/bbs:foreman`), code không nằm trong checkout này.

Từng bước:

- **`/bbs:setup-project`** một lần — dạy autopilot cách đặt tên branch + mặc định QA của bạn, để mọi thứ phía sau đều deterministic.
- **`/bbs:autopilot "<yêu cầu một dòng>"`** cho mọi việc nhiều bước. Nó checkpoint xuống disk (sống sót qua crash và compaction — context bị nén), rồi plan → code → review → QA, và dừng ở chốt PR để bạn review trước khi có gì merge.

**Các chốt của con người — nơi bạn giữ quyền.** Autopilot chỉ dừng ở những khoảnh khắc thật sự thuộc về bạn; chọn khoảnh khắc nào bằng flag:

- `--stop-after=plan` — duyệt hướng đi trước khi viết một dòng code.
- *mặc định* — dừng ở trạng thái QA-xong, bạn review bằng chứng.
- **`/bbs:create-pr`** — bạn tự gọi; autopilot không bao giờ tự mở PR.

**Thêm khi cần:**

- **`/bbs:review-pr`** (tức `/code-review`) — một chốt trước khi merge, vì team nhỏ không có người review thứ hai. Đây là lưới an toàn của bạn.
- **`/bbs:foreman`** — luồng dự án tự vận hành: tách một requirement lớn, xếp lịch các ticket phụ thuộc trong Orca, sở hữu worktree, rồi gác QA và finish của cả dự án. Thừa thãi với một ticket tuần tự.

## Vì sao nó chạy được

- **Nó chạy tới cùng.** `/bbs:autopilot` là một **goal proxy**: phần init gieo state bền — ticket, requirement, plan, checkpoint — rồi giao phần việc cho [`/goal`](#3-chạy), cái Stop hook theo session của Claude Code chặn không cho session dừng chừng nào nó chưa ghi xong verdict QA và review. Trong vòng lặp, model làm việc thoải mái với đầy đủ context, y như khi bạn hỏi thẳng nó; checkpoint trên disk giúp một session mới nối tiếp đúng chỗ con cũ dừng.
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
từng byte; perf tự nhiên có thêm), và cái châm ngòi là đống cruft tích lại.
Maintainer tối ưu *hệ thống đang chạy production* (giữ nó sống dưới tải thật;
đổi caching/indexing/data-model có thể đổi timing), và cái châm ngòi là scale,
bảo mật, hoặc chi phí. "Một feature ổn định, nhiều người xài" là trigger của
Maintainer; nếu nó còn cần dọn cấu trúc, chạy Sweeper thành một pass riêng giữ
nguyên hành vi.

**Những bất biến mọi archetype đều giữ** — chúng chỉ khác nhau ở *nhiệm vụ và
tiêu chí thành công*, không bao giờ khác về độ chặt: quyết định đi qua
[Auto-Decision Framework](.claude/skills/references/auto-decision-framework.md)
(quyết định taste phải ghi log, không bao giờ lẳng lặng đoán bừa); tự kiểm
trước khi "xong"; bán kính sát thương có giới hạn (không force-push, không mất
dữ liệu, không nhắn ra ngoài khi chưa được cấp phép hẳn hoi và có lưu lại);
fail lớn tiếng và tại chỗ
(`BLOCKED`/`NEEDS_CONTEXT` còn hơn một giả định sai).

Chi tiết và các skill mỗi archetype ghép lại:
[`.claude/skills/references/archetypes.md`](.claude/skills/references/archetypes.md).

## Bắt đầu nhanh

Ba bước. Cài một lần cho toàn máy, cấu hình mỗi repo một lần, rồi chạy.

### 1. Cài plugin

**Thẳng từ GitHub — không có gì rơi vào workspace của bạn.** Cài CLI, rồi đăng
ký cùng một marketplace với agent bạn dùng:

```bash
brew install lohi-ai/babysit/bbs        # phần CLI — bắt buộc, xem bên dưới

# Claude Code
claude plugin marketplace add lohi-ai/babysit
claude plugin install bbs@babysit

# Codex CLI
codex plugin marketplace add lohi-ai/babysit
codex plugin add bbs@babysit
```

Khởi động lại agent. Claude Code có `/bbs:autopilot`; Codex có
`$bbs:autopilot`. `bbs upgrade` cập nhật CLI và plugin Claude Code; plugin
Codex được cập nhật bằng CLI của Codex:

```bash
bbs upgrade
codex plugin marketplace upgrade babysit
codex plugin add bbs@babysit
```

**`brew install bbs` không phải tùy chọn.** `bin/bbs` là sản phẩm build, không
được commit, nên plugin cài từ GitHub không kèm binary nào cả. Thiếu `bbs` trên
`PATH` thì cổng gác push/PR không đọc được verdict của ticket, và nó fail closed —
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

Rồi đăng ký checkout trong agent bạn dùng:

```
# Claude Code
/plugin marketplace add ~/src/babysit
/plugin install bbs@babysit

# Codex CLI (chạy trong shell)
codex plugin marketplace add ~/src/babysit
codex plugin add bbs@babysit
```

Cách này đã đặt `bbs` lên `PATH` tại `~/.local/bin/bbs` → checkout của bạn, nên
khỏi cần cài thêm bản Homebrew. Nâng cấp bằng `git pull && ./bin/setup-skills`.

Lưu ý: plugin từ marketplace được *copy* vào `~/.claude/plugins/cache/`, còn một
thư mục nằm dưới `~/.claude/skills/<tên>/` thì được nạp **tại chỗ** — chính cái
thứ hai mới làm cho sửa-là-thấy. Đừng chạy cả hai hình dạng cùng lúc: plugin
marketplace đã cài sẽ thắng khi trùng tên.

</details>

Yêu cầu: Claude Code hoặc Codex CLI có hỗ trợ plugin, cùng với Git.

Khuyến nghị, chưa cần ngay lúc đầu: **[Orca](https://www.onorca.dev)**, ADE để chạy nhiều coding agent cạnh nhau. `/bbs:autopilot` và mọi thứ khác chạy trên terminal bất kỳ — Orca là **phụ thuộc cứng chỉ với `foreman`**, vì nó không còn backend nào khác: Orca cho mỗi worker song song một tab terminal riêng, diff mở trong editor, và app đang chạy nằm trong browser của Orca. Thiếu Orca thì `foreman` dừng ngay với thông báo cách cài.

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
| việc của ticket nằm đâu | trên branch bạn đang đứng | trên branch bạn đang đứng | trên branch bạn đang đứng |
| review diễn ra ở đâu | không ở đâu cả — push chính là release | **tại máy**, trong browser, tác giả tự merge | **trên GitHub**, người khác merge |
| độ gắt QA | `smoke` — 3–5 ca | `standard` — 5–10 | `strict` — 8–12 |
| effort `review-pr` | `low` | `medium` | `high` |

Mỗi profile cũng đòi hỏi vài thứ ở *bạn* — khi bắt đầu một ticket thì phải đang đứng ở branch nào, và base local dùng để làm gì. Bản giao kèo đó, cùng với cách chạy ticket song song trong từng profile, nằm ở **[docs/profiles.vi.md](docs/profiles.vi.md)**.

**Một repo bạn chưa hề cấu hình sẽ tự suy ra `pet`** — không cắt branch, không có chỗ review, việc đi thẳng trên cái branch bạn đang đứng, y như chạy git trần. Lễ nghi là thứ repo phải tự xin, không phải thứ được phát sẵn.

**Lần đầu dùng, hoặc chưa chắc? Chọn `startup`.** Đây là chỗ bắt đầu an toàn với một repo bạn còn quý: không gì lên được remote mà thiếu một PR do chính bạn mở, và QA chạy ở độ gắt `standard`. Đổi profile sau này chỉ tốn một dòng.

Độ gắt chỉ nới **bề rộng, không hạ cái ngưỡng**. Một `PASS` mang đúng một nghĩa ở cả ba mức: mọi chiều rubric áp dụng được phải từ B trở lên, và phải có một lượt chạy end-to-end mới tinh trên đúng code cuối cùng. Dự án nghiệp dư chạy ít ca hơn — chứ không phải không chạy ca nào, và cũng không bao giờ pass với một chiều điểm C.

Tự tay đặt `mode:`, `land:`, hay `push:` sẽ đè lên preset của profile. Đó là cửa thoát hiểm, không phải hình dạng thường ngày — cái núm nào bạn tự tay vặn ra thì cái núm đó thôi bám theo profile.

#### Ticket song song là thứ phải xin, không bao giờ tự cấu hình

**Không profile nào cắt branch hay lôi bạn vào worktree.** Cả ba đều làm việc trên branch bạn đang đứng, y như không có babysit — profile chỉ quyết chỗ review và độ gắt QA, không quyết việc nằm ở đâu. Cố ý vậy: một công cụ lặng lẽ dời việc của bạn là công cụ bạn không quản được.

Cô lập là thứ xin theo từng lần chạy, khi bạn thật sự muốn:

- `/bbs:foreman` — một mẻ: foreman tự tạo một worktree cho mỗi ticket, repo nào cũng được, config nói gì cũng vậy.
- `bbs ticket ensure --slug-hint <slug> --mode=worktree` — riêng ticket này một worktree; rồi chạy autopilot trong đó.
- `bbs ticket ensure --slug-hint <slug> --mode=branch` — cắt `feat/<id>_<slug>` tại chỗ.

**Worktree có giá, nên biết mình mua gì.** Vòng lặp trong không còn 0 bước: vì code nằm trong worktree còn dev server phục vụ checkout chính, mỗi vòng test là một commit cộng `bbs ticket merge-base` thay vì sửa-rồi-refresh. Cái nó mua là ticket tách rời — review từng cái một, bỏ cái hỏng, và land từng cái thành một PR sạch. Repo nào lúc nào cũng muốn hình dạng đó thì viết tay `mode: worktree` + `land: local`; không gì tự viết nó cho bạn.

Các lệnh chuyển việc giữa worktree và bề mặt dùng chung — `merge-base`, `switch`, `reset-base`, cùng lớp cho người dùng `board`, `serve`, `/bbs:fix-pr` — nằm ở mục [Làm nhiều ticket song song](#làm-nhiều-ticket-song-song-mode-worktree). Chi tiết: [`references/git-flow.md`](.claude/skills/references/git-flow.md).

### 3. Chạy

**Bắt đầu ở đây — `autopilot`, luồng một ticket lẻ:**

```
/bbs:autopilot "add a settings page with dark mode toggle"
```

Autopilot init ticket — requirement, plan — rồi dừng lại và in ra một block `/goal` làm **tin nhắn cuối cùng**. Block đó chính là việc duy nhất bạn làm tiếp theo: **copy nó, dán lại vào Claude Code, rồi đi chơi.** Session goal sẽ viết code, review, chạy QA, và commit lên branch bạn đang đứng — autopilot không bao giờ cắt branch, push, hay mở PR. Review xong thì tự mở PR.

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
> /goal bs-ab123 is done: work committed locally, qa verdict PASS/FIXED persisted
> via bbs ticket set-verdict, review-pr verdict persisted, handoff note written — or a
> NEEDS_CONTEXT / BLOCKED status block printed verbatim.
> Work it: /bbs:autopilot builder bs-ab123
> ```

#### Vì sao `/goal` nắm phần việc

`/goal <condition>` (có sẵn, Claude Code 2.1.139+) gắn một Stop hook theo session: model làm việc thoải mái với đầy đủ context — không nghi thức từng bước — và cái hook chặn không cho dừng chừng nào điều kiện chưa thỏa. Đó là lý do bước tiếp theo là *dán block `/goal`* chứ không phải "chạy một lệnh": chính việc dán nó là cái gắn hook lên. Block autopilot in ra đã gói sẵn các cổng gác của babysit lẫn điều khoản thoát.

Nhờ điều khoản thoát, vòng lặp kết thúc khi cần leo thang, thay vì nghiến răng cày mãi vào một input còn thiếu. Muốn thoát giữa chừng: `/goal clear`, `Ctrl-C`, hoặc touch `~/.babysit/projects/<slug>/tickets/<ticket>/STOP`.

Không có `/goal`, gọi lại `/bbs:autopilot bs-ab123` vẫn nối tiếp từ checkpoint — chỉ là bạn phải tự tay đẩy nó qua ranh giới giữa các session.

#### Nâng cao — `foreman`, orchestrator tự vận hành cho cả dự án

Khi một requirement trải qua nhiều ticket, `foreman` sở hữu cả dự án. Từng worker vẫn chạy autopilot quen thuộc, còn foreman cấp checkout và điều phối DAG:

```
/bbs:foreman <requirement dự án lớn>  # tách việc, xếp lịch, kiểm chứng, finish
/bbs:foreman                          # attach/resume từ ticket + state Orca
```

Foreman tạo một parent project và các child ticket có biên rõ, ghi cạnh phụ thuộc, rồi khởi động worker được giám sát qua Orca orchestration. Mỗi child có một Dispatch chỉ-plan, một design gate tự xử lý, rồi một Dispatch build/QA. Foreman kiểm verdict trên đĩa, tuần tự hóa test surface dùng chung, chạy QA tích hợp khi các ticket tương tác, sau đó mới áp dụng `finish: land|pr|review` theo thứ tự phụ thuộc. Message và Dispatch id của Orca thay cho việc dò chữ trong pane, nên coordinator khởi động lại vẫn resume được mà không cần nhớ hội thoại.

**Một thứ phải có trước, nên nó mới là bài học thứ hai:** [Orca](https://www.onorca.dev) với orchestration được bật. Foreman nạp guide orchestration đúng phiên bản đang cài, dừng rõ ràng nếu runtime thiếu, và tạo worktree cho từng child bất kể profile. Profile vẫn quyết định độ gắt và finish policy. Với một ticket lẻ chạy tuần tự, nó chẳng hơn `/bbs:autopilot` chỗ nào.

## Cách dùng

Babysit là một dây chuyền nhỏ để ship một thay đổi. Bạn thả ý tưởng vào đầu này, nhặt ra một branch sẵn sàng review ở đầu kia. Dây chuyền dừng đúng bốn chỗ mà bạn thật sự thêm được giá trị; khúc giữa nó tự lo.

### Bốn chỗ nó dừng

1. **"Có phải đây là thứ đáng làm không?"** — `requirement.md` sẵn sàng. Bạn đọc và duyệt.
2. **"Có phải đây là cách làm đúng không?"** — `plan.md` sẵn sàng. Bạn đọc, chỉnh, duyệt.
3. **"Nó có chạy thật không?"** — code đã viết, đã review, đã QA, đã commit.
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
/bbs:autopilot "<ý tưởng một dòng>"   # feature mới — tạo ticket, chạy đầu-tới-cuối
/bbs:autopilot bs-ab123              # ticket có sẵn — state-route tới stage kế tiếp
/bbs:autopilot                       # resume — nối lại từ checkpoint của ticket đã resolve
```

Cả bề mặt chỉ có vậy. Ba flag mở rộng thêm — `--stop-after=requirement|plan` để dừng ở checkpoint sớm hơn, và `--planner <model>` / `--planner-effort <effort>` để chọn model tạo plan/prototype. Autopilot luôn chạy trên checkout bạn khởi động nó; cô lập là `bbs ticket ensure --mode=…` hoặc `/bbs:foreman`, không bao giờ là flag của autopilot. Không có token động từ nào cả.

### Làm nhiều ticket song song (mode `worktree`)

**Đây là hình dạng mà một lô foreman (hoặc `bbs ticket ensure --mode=worktree` chạy tay) cho bạn** — các lệnh mà một lần chạy như vậy giao lại. `/bbs:foreman` lái đúng mục này cho cả một mẻ — dispatch, chốt design, kiểm verdict, bề mặt gộp — nhưng bạn không cần nó thì mọi thứ ở đây vẫn chạy.

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
   Với Orca, cả vòng lặp này gói gọn trong một worktree: `orca tab create --url <qa url>` đưa app đang chạy vào browser tích hợp, còn `orca file open-changed --mode diff --worktree path:<ticket-worktree>` mở diff của ticket bên cạnh.
4. Ưng rồi → `bbs ticket serve --release`, rồi `/bbs:create-pr` cho từng repo. Reviewer comment sau đó → `/bbs:fix-pr`.
5. `bbs ticket board --pr` chỉ ra các PR đã merge và in đúng các lệnh dọn dẹp (`reset-base`, `set-status done`).

**Một ticket, hai repo** (feature trải cả frontend + backend): `/bbs:setup-project` ghi lại các repo anh em một lần; builder của autopilot tự băng qua — tạo ticket anh em đã liên kết, code và QA cả hai bên — và `serve` bày cả cặp ra trước mặt bạn bằng một lệnh. Trong lúc đó session của các ticket khác vẫn code và review trong worktree riêng của chúng; `board` chỉ mặt từng người đang giữ bề mặt và còn giữ bao lâu. Công thức đầy đủ: [`references/git-flow.md` § Attended parallel review](.claude/skills/references/git-flow.md).

## Đào sâu hơn

- **Ruột routing & debug** — init gieo những gì, vòng lặp `/goal` khôi phục từ checkpoint ra sao, và skill nào giữ cửa nào: [`.claude/skills/autopilot/SKILL.md`](.claude/skills/autopilot/SKILL.md). Muốn xem trạng thái mà một lần chạy sẽ route theo mà không chạy thật: `bbs autopilot explain` (thêm `--details` để ra ma trận prereq của workflow).
- **Profile** — [`docs/profiles.vi.md`](docs/profiles.vi.md): mỗi profile đòi hỏi gì ở base branch của bạn, và cách chạy ticket song song trong từng profile.
- **Schema config** — [`.claude/skills/references/git-flow.md`](.claude/skills/references/git-flow.md) và [`docs/qa-config.md`](docs/qa-config.md) để tự viết tay `.babysit/`.

## Danh mục skill

`/bbs:autopilot` ghép các skill dưới đây thành workflow đầy đủ. Gọi thẳng một cái khi bạn chỉ cần đúng mảnh đó — mấy bản hit:

| Tôi muốn… | Skill |
|-----------|-------|
| Ship một feature đầu-tới-cuối từ một ý tưởng một dòng | `/bbs:autopilot "<idea>"` |
| Hoàn thành dự án lớn gồm nhiều ticket hoặc feature | `/bbs:foreman "<dự án>"` |
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

Tất cả là một binary duy nhất, gọi dạng `bbs <sub>` — `bbs autopilot` (bộ chạy), `bbs ticket env` (resolver danh tính: `BABYSIT_TICKET` → manifest → branch), cộng các trợ giúp cho env, config, snapshot db, và kiểm tra upgrade. `brew install lohi-ai/babysit/bbs` đặt nó lên `PATH`; nếu cài từ checkout thì `setup-skills` build nó rồi symlink `~/.local/bin/bbs`, kèm các alias argv0 `bbs-*` vào `~/.claude/` cho các caller cũ. Bảng đầy đủ và mục đích ở [`docs/companion-cli.md`](docs/companion-cli.md). Chạy `bbs <sub> --help` để xem cách dùng bất kỳ cái nào.

## Vận hành

Config ngày-2 (`bbs config`), telemetry (JSONL đổ vào `~/.babysit/analytics/`, mặc định chỉ ở local), và xử lý upgrade (`bbs upgrade check` + `bbs upgrade`) nằm trong [`docs/operations.md`](docs/operations.md).

**Upgrade.** Cập nhật CLI và bản plugin của agent bạn dùng, rồi khởi động lại agent:

```bash
bbs upgrade
codex plugin marketplace upgrade babysit
codex plugin add bbs@babysit
```

babysit gồm hai nửa do hai công cụ khác nhau quản — CLI qua brew và plugin của Claude Code — và `bbs upgrade` chạy nửa nào máy này có, đồng thời nêu tên nửa nào nó không với tới được. Nếu cài từ checkout thì nó pull rồi chạy lại `setup-skills` cho nửa CLI, sau đó vẫn cập nhật plugin marketplace nếu máy có cài: checkout nằm trên `PATH` và plugin trong `~/.claude/plugins/cache/` là hai bản sao khác nhau, và bản Claude Code nạp chính là plugin đã cài.

## Gỡ cài

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
| Mọi `git push` đều bị chặn, báo "GATE OFFLINE" | Chưa có `bbs` trên `PATH` — `brew install lohi-ai/babysit/bbs`. Plugin không kèm binary, và cổng gác cố tình fail closed |
| Skill biến mất hoặc cũ mèm sau khi upgrade | Khởi động lại Claude Code; vẫn cũ thì chạy lại `bbs upgrade` và đọc xem nó báo không với tới được nửa nào |
| `/bbs:*` không tìm thấy | `claude plugin install bbs@babysit`, rồi khởi động lại; hoặc `/reload-plugins` |
| `$bbs:*` không tìm thấy trong Codex | `codex plugin add bbs@babysit`, rồi mở session mới |
| Skill hiện ra mà thiếu tiền tố `bbs:` | Bản cài cũ — `find ~/.claude/skills -maxdepth 1 -type l -name 'bbs:*' -delete`, rồi cài lại plugin |
| `env resolve` trả về rỗng | Kiểm xem đúng file `.env.base` có nằm dưới `config/<app>/` không |

## Giấy phép

MIT.
