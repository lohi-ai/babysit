# Làm việc với profile

[English](profiles.md) | Tiếng Việt

Đúng một cái núm trong `.babysit/git-flow.yaml` quyết định cả cơ chế branch *lẫn*
độ gắt QA:

```yaml
profile: startup      # pet | startup | enterprise
base_branch: main
```

`setup-project` hỏi đúng một câu — **ở repo này, sai một lần thì trả giá bao
nhiêu?** — rồi ghi lại câu trả lời. Mọi thứ còn lại suy ra từ đó. Xem mình đang
có gì:

```bash
bbs autopilot git-flow      # in ra BBS_PROFILE / BBS_MODE / BBS_LAND / BBS_RIGOR / …
```

| | `pet` | `startup` | `enterprise` |
|---|---|---|---|
| branch sống lâu | `main` | `develop` + `main` | `develop` + `staging` + `main` |
| branch cho mỗi ticket | không | `feat/<id>_<slug>`, trong worktree | `feat/<id>_<slug>`, trong worktree |
| việc về đích kiểu gì | push **chính là** release | PR vào `develop`, tác giả tự merge | PR vào `develop`, người khác merge |
| người QA trước khi mở PR | gộp trên `main` ở máy | gộp trên `develop` ở máy | gộp trên `develop` ở máy |
| review diễn ra ở đâu | không ở đâu cả | tại máy, trong browser | tại máy **và** trên GitHub |
| độ gắt QA | smoke, 3–5 ca | standard, 5–10 | strict, 8–12 |

Độ gắt chỉ nới *bề rộng*. `PASS` mang đúng một nghĩa ở cả ba: mọi chiều rubric
áp dụng được phải từ B trở lên, và phải có một lượt chạy end-to-end mới tinh
trên đúng code cuối cùng. Dự án nghiệp dư chạy ít ca hơn — chứ không bao giờ
chạy không ca nào.

**Một repo không có key `profile:` sẽ tự suy ra `pet`.** Chưa ai cấu hình nó,
nên nó xử sự y như git trần: việc đi thẳng trên cái branch bạn đang đứng, không
cắt gì, không gộp gì. Mọi branch, worktree và chỗ review trong bảng trên đều là
thứ repo phải tự xin bằng cách chạy `setup-project`. Key
`mode:`/`land:`/`push:`/`ticket_branch:` viết tay vẫn thắng preset, nên một
config viết từ hồi chưa có profile vẫn giữ nguyên hình dạng nó đã ghi ra.

**Phần còn lại của tài liệu này nói về thứ không có trong bảng: mỗi profile
trông đợi *bạn* làm gì với base branch của mình.** Làm sai chỗ đó là mấy cái
cổng gác lặng lẽ ngừng gác.

---

## Hình dạng branch mà mỗi profile giả định

`base_branch` là thứ duy nhất profile không suy ra giùm bạn được — nó tùy vào
cách bạn release. Đặt nó khớp với hình dạng bên dưới.

### `pet` — một branch duy nhất

```
main ────────●────●────●──────►   push chính là release
```

```yaml
profile: pet
base_branch: main
```

### `startup` — `develop` gom việc, `main` release

```
feat/bs-a ──┐
feat/bs-b ──┼──► develop ───────────────►   PR về đích ở đây
feat/bs-c ──┘        │
                     └──► main ──►  release có tag
```

```yaml
profile: startup
base_branch: develop
```

Sao không để thẳng `main`: với `base_branch: main`, merge PR **chính là** ship,
nên QA ở máy bạn là cổng gác duy nhất từng chạy. `develop` tách *đã gom* khỏi
*đã ship* thành hai sự kiện — một cú merge hỏng bị nhốt lại trên branch gom
việc, còn `main` lúc nào cũng release được. Chặng đi vòng thêm đó đổi lại đúng
chừng ấy.

Nếu bạn deploy mỗi lần merge và chẳng có khoảnh khắc release riêng nào, thì
`develop` chỉ là lễ nghi — cứ đặt `base_branch: main`, và biết rằng bạn vừa đẩy
QA ở máy thành cổng gác cuối cùng.

### `enterprise` — thêm một môi trường gom việc đã deploy ở giữa

```
feat/bs-a ──┐
feat/bs-b ──┼──► develop ──► staging ──► main
feat/bs-c ──┘     (CI)     (QA bản deploy)  (release có tag)
```

```yaml
profile: enterprise
base_branch: develop
```

Hai chỗ review trả lời hai câu khác nhau: bản gộp ở máy là QA **sản phẩm** (nó
có chạy không?), còn PR trên GitHub vào `develop` là review **code** do người
khác làm. `staging` là chỗ người ta QA kết quả đã gom như một bản deploy, chứ
không phải như một checkout.

### Đẩy lên tầng trên không bao giờ là việc của babysit

`create-pr` nhắm vào `base_branch` rồi dừng — cũng chính là lý do nó không bao
giờ merge. Mọi bước đẩy lên trên nữa là việc của bạn hoặc của CI:

```bash
git switch main && git merge --ff-only develop && git tag v1.2.0 && git push --follow-tags
```

Ngoại lệ duy nhất là hotfix — nó fork thẳng từ production thay vì xếp hàng sau
mọi thứ đang nằm trên `develop`:

```bash
BBS_BASE_BRANCH=main bbs ticket ensure --slug-hint hotfix-thing --type hotfix
# …sửa, QA, PR vào main, rồi back-merge main về develop
```

---

## Cái luật đứng trên mọi profile

**`main` (hay `develop`) ở máy bạn là một *bề mặt test*, không phải một base của
git.**

Mọi thao tác ghi lên branch ticket đều tham chiếu `origin/<base>`, không bao giờ
`<base>` ở máy. Merge base local vào một branch ticket là lôi luôn mọi thứ khác
bạn đang làm dở vào PR của ticket đó. Muốn kéo thay đổi từ upstream về thì dùng:

```bash
bbs ticket refresh          # fetch + merge origin/<base>; BLOCK nếu tree bẩn hoặc dính conflict
```

Chỉ bốn lệnh đụng vào base ở máy — `merge-base`, `switch`, `reset-base`,
`serve` — và không lệnh nào trong đó ghi lên branch ticket.

`bbs ticket board` khép phần chân bằng vị trí thật của base ở máy bạn, để thấy
nó trôi trước khi nó cắn:

```
BASE: main — 3 ahead / 12 behind origin/main (last fetch 2h ago)
  ↑ 3 commit(s) exist only on local 'main' — tickets are cut from origin/main, so new ones will not have them
  ↓ origin/main moved on — bbs ticket refresh (in a ticket) or bbs ticket reset-base (on the primary)
```

Nó không bao giờ chặn — base trôi lệch giữa chừng là chuyện thường. Board không
fetch, nên mấy con số tươi đúng bằng cái tuổi ghi trong ngoặc.

Với `startup` và `enterprise`, dòng `↑` mới là dòng đáng ra tay: mấy commit đó
vô hình với mọi ticket cắt sau chúng. Với `pet` thì ngược lại — chẳng cắt gì cả,
nên ticket dựng *trên* đám việc đó, và cái sai duy nhất là nó chưa được push.
Dòng này tự đổi lời theo mode.

### Vậy branch ticket cắt ra từ đâu?

**Từ `origin/<base>`, không bao giờ từ bản ở máy bạn** — còn `pet` thì chẳng cắt
gì hết:

| profile | cắt từ đâu |
|---|---|
| `pet` | **không cắt gì cả.** Ticket đi thẳng trên branch hiện tại của bạn, đang ở trạng thái nào thì kệ nó |
| `startup` | `origin/<base>`, fetch trước đã |
| `enterprise` | `origin/<base>`, fetch trước đã |

`ensure` fetch `origin/<base>` trước khi cắt, rồi fork từ cái ref
remote-tracking với `--no-track`. Kiểu cắt vào worktree và kiểu cắt tại chỗ (cửa
thoát cho vòng lặp solo) dùng chung một nguồn, nên không kiểu nào là con ghẻ:

```
ensure: worktree ready at …/bs-xxxxxxxx_probe (branch feat/bs-xxxxxxxx_probe off origin/main)
```

Hai đường lui, theo thứ tự này:

1. **Không có ref `origin/<base>` nào cả** (không có remote, hoặc remote thiếu
   branch đó) → fork từ `<base>` ở máy bạn.
2. **Fetch hỏng nhưng ref vẫn còn** → fork từ `origin/<base>` *biết được lần
   cuối*, và nói thẳng ra:
   `ensure: warning — fetch failed, using the last-known origin/<base>`.
   Để ý là ở đây nó **không** lẳng lặng lùi về base ở máy — một ref remote cũ
   vẫn sạch hơn một base local đã bị vấy.

**Vì sao chuyện này quan trọng.** Base ở máy cứ mỗi lần bạn chạy `merge-base`
hay `switch` lại gom thêm ticket của người khác — đó đúng là việc của nó với tư
cách một bề mặt test. Nếu ticket cắt ra từ đó, mỗi branch mới sẽ thừa hưởng luôn
bất cứ thứ gì đang được gộp ở đó lúc ấy, và đám đó sẽ đi thẳng vào PR. Cắt từ
`origin/<base>` mới là thứ giữ cho các PR song song độc lập với nhau.

Đó cũng là cách sắc nhất để nói `pet` đánh đổi mất gì: không cắt gì cả, nên việc
của bạn *đúng là* bắt đầu từ base ở máy, kèm luôn mọi thứ khác đang làm dở ở đó.
Ngay từ cách dựng đã không có tách bạch — không phải tách yếu hơn, mà là không
có.

---

## `pet` — bạn làm việc thẳng trên base branch

Mode `trunk`: không bao giờ cắt gì. Session đi thẳng trên cái branch bạn đang
đứng, và việc về đích ngay trên `base_branch`. Git chẳng ép gì cả — sửa gì cũng
được, checkout gì cũng được, không lễ nghi. `create-pr` ở đây cố tình BLOCK
(`land: none`); không có bước PR nào, nên **verdict qa + review-pr là thứ duy
nhất đứng giữa một session và code đã release của bạn.**

### ⚠️ Cái bẫy: danh tính treo trên một biến môi trường

Không có branch ticket thì trong git chẳng có gì để nhận ra ticket. Danh tính
dời sang `BABYSIT_TICKET`, cái mà `ensure` in sẵn cho bạn:

```
export BABYSIT_TICKET=bs-xxxxxxxx
```

Mất cái biến đó — shell mới, pane mới, một session vừa crash rồi sống lại — là
pre-push hook **không resolve ra ticket nào và cho qua luôn**. Verdict qa với
review-pr thôi gác cái gì hết, mà lặng thinh. Với `pet` thì đó là một cú push
không ai gác, và push chính là release.

Đây không phải chuyện lý thuyết; dựng lại y hệt được. Cứu bằng:

```bash
bbs ticket session list
bbs ticket session attach <id>     # in lại dòng export
```

Nên tóm tắt thật lòng về `pet` không phải là "thoải mái không giới hạn" — mà là
**kỷ luật đã dọn ra khỏi git, chui vào đúng một biến môi trường mà bạn phải nuôi
cho sống.** Đổi chác đó ổn với một repo nghiệp dư. Và cũng chính là lý do `pet`
là câu trả lời sai cho bất cứ thứ gì đã có người dùng.

### Chạy song song dưới `pet`

`pet` là profile duy nhất **không** mặc định bật worktree, và đó là chỗ đánh
đổi: nhiều session trong một thư mục, một dev server test hết mọi thứ cùng lúc,
không lễ nghi — nhưng mọi thay đổi đan vào nhau trên base branch, nên bạn
**không review riêng được một ticket, không vứt bỏ được một ticket hỏng, cũng
không truy ra được một regression từ đâu chui ra**. Được ăn cả, ngã về không.

Muốn nhiều ticket song song mà tách rời được thường là dấu hiệu repo đã lớn hơn
`pet` — lúc đó `startup` mới là câu trả lời đúng, và đổi sang chỉ tốn một dòng.
Vẫn muốn thì viết **cả hai** key:

```yaml
profile: pet
base_branch: main
mode: worktree        # ticket có checkout riêng
land: none            # …nhưng vẫn không có bước PR nào
```

Ticket fork `origin/main` vào worktree, `bbs ticket serve` gộp mấy cái đã xong
lên `main` ở máy để QA, còn `main` thì bạn tự push. Viết mỗi `mode: worktree`
thôi thì bạn vẫn giữ `land: none` — không có gì lẳng lặng nâng một repo `pet`
lên thành luồng có PR.

---

## `startup` — checkout chính ở lì trên base, mãi mãi

Mode `worktree`, land `local`. Luật gói trong một dòng:

> **Checkout chính không bao giờ rời `base_branch`. Mọi ticket sống trong một
> worktree.**

Hai cái key đó là một quyết định, không phải hai. Ticket cần chỗ ở không phải
checkout chính; checkout chính cần ở lại trên base vì đó là trạng thái duy nhất
mà một mẻ đã xong gộp được vào đó để review. Bỏ nửa nào đi thì nửa kia cũng chết
theo — nên viết `mode: branch` *nằm cạnh* `land: local` là lỗi ngay lúc resolve,
chứ không phải một config đọc thì xuôi rồi mấy tiếng sau mới BLOCK.

`ensure` nói cho bạn biết ticket đi đâu:

```
ensure: mode=worktree — cutting into a worktree (primary checkout stays on 'develop')
WORKTREE=/…/.babysit/worktrees/bs-xxxxxxxx_thing
```

### Vòng lặp hằng ngày

```bash
/bbs:foreman                      # hoặc: /bbs:autopilot "<yêu cầu>" cho từng ticket
# …các worker code + QA trong worktree của riêng chúng…

bbs ticket board                  # ai xong rồi, ai đang giữ lease
bbs ticket serve                  # gộp mọi ticket đã xong lên develop ở máy
# …xem bản gộp trong browser: đây là chốt QA của con người…
bbs ticket serve --release
/bbs:create-pr <ticket>           # mỗi ticket một PR, nhắm vào develop
```

`serve <t1> <t2>` gộp đúng những ticket bạn gọi tên — cái được so với kiểu song
song của `pet` là bạn bỏ được một ticket hỏng ra ngoài, mà từng cái còn lại vẫn
về đích thành một PR sạch của riêng nó.

### ⚠️ Cái giá phải trả

Vòng lặp trong không còn 0 bước. Mỗi vòng test là **commit + `bbs ticket
merge-base`**, không phải sửa-rồi-refresh, vì code bạn đang test nằm trong
worktree còn dev server thì phục vụ checkout chính.

Nếu bạn thật sự làm từng ticket một, mua lại vòng lặp nhanh bằng đúng một key:

```yaml
profile: startup
base_branch: develop
mode: branch          # cắt feat/… tại chỗ khi tree sạch;
                      # kéo theo land: pr — chẳng có gì để gộp
```

Đổi lại thì luật base-sạch có hiệu lực: lúc một ticket bắt đầu, bạn phải đang
đứng trên `base_branch` với tree sạch, không thì `ensure` vẫn né sang worktree
và nhắc bạn về cái vòng lặp bạn vừa mua.

## `enterprise` — vẫn giao kèo git đó, thêm một môi trường

Cơ chế branch **y hệt `startup`**: mỗi ticket một worktree, checkout chính ghim
trên `develop`, gộp lại để review. Ba thứ đổi khác:

1. **Thêm một chỗ review nữa.** Bản gộp ở máy là QA sản phẩm; PR trên `develop`
   là review code, và người khác mới là người merge nó.
2. **`staging` nằm giữa `develop` và `main`** — người ta QA kết quả đã gom như
   một bản deploy, chứ không chỉ như một checkout.
3. **QA mức strict trả bề mặt lại trước khi bàn giao** — bằng chứng đi theo phần
   thân PR thay vì nằm ở đó. Với `smoke`/`standard`, QA *để app chạy tiếp* và
   đưa URL vào bản bàn giao; với `strict` thì không. Đừng trông chờ app còn sống
   sau một lượt QA.

---

## Chạy nhiều ticket song song

**Với `startup` và `enterprise`, đây là hình dạng mặc định** — profile đã bật
worktree sẵn, nên chẳng có gì phải xin. Với `pet` thì bạn tự chọn vào bằng hai
cái key ở trên, hoặc giao việc kèm `--mode=worktree` cho từng ticket mà không
đụng vào config.

### Đúng một cái luật

> **Checkout chính ở lại trên `base_branch`.** Nó là bề mặt test dùng chung. Nếu
> một branch ticket chiếm chỗ đó thì chẳng gộp được gì lên nữa.

`serve` bắt đúng luật này chứ không đoán mò:

```
STATUS: BLOCKED
REASON: primary checkout … is on 'feat/…', not base 'main'.
```

### Cách A — để `foreman` giao việc giùm bạn

Cần [cmux](https://cmux.com), đây là phụ thuộc cứng. Giao cho nó vài yêu cầu độc
lập; nó mở một worker hiện hình cho mỗi ticket, mỗi con nằm trong workspace cmux
riêng chạy autopilot đầu-tới-cuối, và tự xin `--mode=worktree` theo từng lần
giao việc — nên nó chạy y như vậy trên một repo `pet` chưa hề cấu hình gì:

```
/bbs:foreman
```

Nó cũng chốt duyệt design của từng ticket trước khi viết dòng code nào, rồi
merge các ticket đã xong lên base ở máy để bạn review cả mẻ một lượt.

### Cách B — tự tay giao việc

```bash
/bbs:autopilot "<yêu cầu>"     # mỗi ticket một lần
```

hoặc, muốn tạo worktree mà chưa bắt tay vào việc:

```bash
bbs ticket ensure --slug-hint <slug>
```

Chỉ thêm `--mode=worktree` vào một trong hai lệnh khi config của repo chưa ghi
sẵn điều đó (`pet`, hoặc repo đã chọn cửa thoát cho vòng lặp solo).

### Rồi thì: gộp, review, ship

```bash
bbs ticket board                  # tình hình: ticket, verdict, PR, ai đang giữ lease
bbs ticket serve                  # gộp TẤT CẢ ticket đã xong (qa + review-pr DONE) lên base ở máy
bbs ticket serve <t1> <t2>        # …hoặc đúng những cái bạn gọi tên
bbs ticket serve --release        # trả bề mặt lại
```

Các lệnh phụ trợ:

| lệnh | nó làm gì |
|---|---|
| `bbs ticket merge-base` | chạy **từ trong worktree** — đưa ticket đó lên bề mặt dùng chung để QA |
| `bbs ticket switch <t…>` | chạy **từ checkout chính** — reset về base, rồi merge đúng các ticket được gọi tên |
| `bbs ticket reset-base` | sau khi PR merge lên upstream, kéo base ở máy về lại `origin/<base>` |
| `bbs ticket qa-lease` | mỗi lúc chỉ một session QA trên bề mặt dùng chung; session khác BLOCK và nêu tên chủ đang giữ |

Cả đám đều từ chối lớn tiếng chứ không làm mất việc.

### Vòng lặp QA khi ticket sống trong worktree

1. Code xong thì **commit ngay trong worktree**.
2. Chạy `bbs ticket merge-base` từ trong worktree.
3. QA thấy có vấn đề → sửa **trong worktree**, commit, chạy lại `merge-base`.
   Đừng bao giờ sửa trong checkout base — QA phải test một trạng thái ticket đã
   commit.
4. Push branch ticket; `create-pr` nhắm vào `base_branch`.
5. Sau khi PR merge: chạy `bbs ticket reset-base` từ checkout chính; mấy worktree
   đang làm dở thì chạy lại `merge-base`.

---

## Đổi profile

Chạy lại `/bbs:setup-project` — với repo đã cấu hình, nó mời bạn đổi và chỉ viết
lại đúng dòng `profile:`.

`mode:` chỉ được đọc lúc cắt branch, nên **đổi profile chỉ ảnh hưởng tới ticket
mới**; cái nào đang làm dở vẫn giữ hình dạng lúc nó được cắt ra.

- **Đổi vào kiểu worktree** (`pet` → `startup`/`enterprise`, hoặc từ cửa thoát
  quay về mặc định) — checkout chính phải sạch và đang đứng trên `base_branch`
  trước đã.
- **Đổi ra khỏi nó** — làm nốt hoặc gác lại mấy worktree đang dở
  (`bbs ticket board`), và trả hết qa-lease.

Đổi `base_branch` thành `develop` trên một repo chưa có branch đó: hãy tạo và
push nó từ `main` **trước khi** đổi, không thì lần `ensure` đầu tiên không thấy
`origin/develop` và sẽ fork từ base ở máy bạn.

```bash
git switch -c develop main && git push -u origin develop
```

Tự tay đặt `mode:`, `land:`, hay `push:` sẽ đè lên preset của profile. Đó là cửa
thoát hiểm, không phải hình dạng thường ngày — cái núm nào bạn tự tay vặn ra thì
cái núm đó thôi bám theo profile, và tổ hợp đó chưa chắc đã xuôi. Có đúng một
cặp viết tay bị chặn thẳng:

```
git-flow: incoherent .babysit/git-flow.yaml: mode 'branch' with land 'local' —
a branch cut in place takes the primary checkout off 'develop', so nothing can
compose there. Use 'land: pr' for per-ticket PRs, or drop 'mode: branch' to
keep the profile's worktree default
```

Mấy tổ hợp còn lại chỉ vô dụng thôi, nên nó để yên (`mode: trunk` đi với
`land: pr` chẳng cắt branch nào, nên PR lấy đâu ra chỗ mà mọc).

Schema đầy đủ và bảng suy dẫn: [`.claude/skills/references/git-flow.md`](../.claude/skills/references/git-flow.md).
