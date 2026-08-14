# Làm việc với profile

[English](profiles.md) | Tiếng Việt

Đúng một cái núm trong `.babysit/git-flow.yaml` quyết định việc đã xong thì đi
đâu, và QA test gắt tới mức nào:

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
| việc về đích kiểu gì | push **chính là** release | PR vào `develop`, tác giả tự merge | PR vào `develop`, người khác merge |
| review diễn ra ở đâu | không ở đâu cả | tại máy, trong browser | tại máy **và** trên GitHub |
| độ gắt QA | smoke, 3–5 ca | standard, 5–10 | strict, 8–12 |

Độ gắt chỉ nới *bề rộng*. `PASS` mang đúng một nghĩa ở cả ba: mọi chiều rubric
áp dụng được phải từ B trở lên, và phải có một lượt chạy end-to-end mới tinh
trên đúng code cuối cùng. Dự án nghiệp dư chạy ít ca hơn — chứ không bao giờ
chạy không ca nào.

**Một repo không có key `profile:` sẽ tự suy ra `pet`.** Chưa ai cấu hình nó,
nên nó xử sự y như git trần. Key `mode:`/`land:`/`push:`/`ticket_branch:` viết
tay vẫn thắng preset, nên một config viết từ hồi chưa có profile vẫn giữ nguyên
hình dạng nó đã ghi ra.

---

## Thứ không profile nào đụng tới: bạn làm việc ở đâu

**Babysit làm việc ngay trên cái branch bạn đang đứng.** Không profile nào cắt
branch, không profile nào lôi bạn vào worktree — `bbs autopilot git-flow` in ra
`BBS_MODE='trunk'` ở cả ba. Đây là chủ ý: một công cụ lặng lẽ dời việc của bạn
đi chỗ khác là công cụ bạn không quản được. Nên sự cô lập là thứ *từng lần chạy*
phải xin, không bao giờ là thứ file config tự bật sau lưng bạn:

```bash
/bbs:autopilot --mode=worktree "<yêu cầu>"          # ticket này có checkout riêng
bbs ticket ensure --slug-hint thing --mode=branch   # …hoặc cắt branch feat/… tại chỗ
/bbs:foreman                                        # chạy lô: mỗi ticket một worktree
```

Repo nào lúc nào cũng muốn một trong hai hình dạng đó thì viết tay
`mode: worktree` (hoặc `mode: branch`) — xem [Chạy nhiều ticket song
song](#chạy-nhiều-ticket-song-song). Phần còn lại của tài liệu này nói về những
thứ profile *thật sự* quyết định.

### Cái bẫy: danh tính ticket nằm trong một biến môi trường

Không có branch cho ticket thì trong git chẳng có gì nhận diện nó. Danh tính
chuyển sang `BABYSIT_TICKET`, do `ensure` in ra cho bạn:

```
export BABYSIT_TICKET=bs-xxxxxxxx
```

Mất biến đó — shell mới, pane mới, một session crash rồi quay lại — thì hook
pre-push **không resolve ra ticket nào và bỏ qua**. Verdict qa và review-pr thôi
không còn chặn gì nữa, một cách âm thầm. Ở `pet`, nghĩa là một cú push không ai
gác, mà push chính là release. Khôi phục bằng:

```bash
bbs ticket session list
bbs ticket session attach <id>     # in lại dòng export
```

---

## Cấu trúc branch mà mỗi profile giả định

`base_branch` là thứ duy nhất profile không suy ra hộ bạn được — nó phụ thuộc
vào cách bạn release. Đặt nó khớp với hình dưới đây.

### `pet` — một branch

```
main ────────●────●────●──────►   push chính là release
```

```yaml
profile: pet
base_branch: main
```

`create-pr` cố tình BLOCK ở đây (`land: none`); không có bước PR nào cả, nên
**verdict qa + review-pr là thứ duy nhất đứng giữa một session và code đã phát
hành của bạn.**

### `startup` — `develop` để tích hợp, `main` để release

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

Sao không dùng thẳng `main`: với `base_branch: main`, merge PR **chính là** ship,
nên QA ở máy bạn là cửa duy nhất từng chạy. `develop` tách *đã tích hợp* khỏi
*đã ship* thành hai sự kiện — một merge tồi bị giữ lại trên nhánh tích hợp, và
`main` luôn ở trạng thái release được.

Nếu bạn deploy mỗi lần merge và không có thời điểm release riêng thì `develop`
chỉ là thủ tục — đặt `base_branch: main` và biết rằng bạn vừa biến QA ở máy
thành cửa cuối cùng.

### `enterprise` — thêm một môi trường tích hợp được deploy ở giữa

```
feat/bs-a ──┐
feat/bs-b ──┼──► develop ──► staging ──► main
feat/bs-c ──┘     (CI)      (QA đã deploy)  (release có tag)
```

```yaml
profile: enterprise
base_branch: develop
```

Hai chỗ review trả lời hai câu khác nhau: lượt xem trong browser ở máy là QA
**sản phẩm** (nó có chạy không?), PR trên GitHub vào `develop` là review **code**
do người khác làm. `staging` là nơi kết quả đã tích hợp được QA như một bản
deploy chứ không phải như một checkout. QA `strict` còn *trả* luôn surface trước
khi bàn giao — bằng chứng đi trong body của PR, nên đừng mong app còn chạy sau
một lượt QA (`smoke`/`standard` để app chạy và đưa bạn URL).

### Việc đẩy lên bậc trên không bao giờ là việc của babysit

`create-pr` nhắm vào `base_branch` rồi dừng — cùng lý do nó không bao giờ merge.
Mọi bước đẩy lên phía trên là của bạn hoặc của CI:

```bash
git switch main && git merge --ff-only develop && git tag v1.2.0 && git push --follow-tags
```

Ngoại lệ duy nhất là hotfix, fork thẳng từ production thay vì xếp hàng sau mọi
thứ đang nằm trên `develop`:

```bash
BBS_BASE_BRANCH=main bbs ticket ensure --slug-hint hotfix-thing --type hotfix --mode=branch
# …fix, QA, PR vào main, rồi merge ngược main về develop
```

---

## Khi đã có branch: base ở máy là *bề mặt test*, không phải base của git

Luật này chỉ có tác dụng khi đã thật sự cắt cái gì đó — một ticket
`--mode=branch`, hoặc một lô chạy worktree.

Mọi thao tác ghi lên branch ticket đều tham chiếu `origin/<base>`, không bao giờ
là `<base>` ở máy. Merge base ở máy vào branch ticket sẽ kéo mọi thứ đang dang
dở khác vào PR của ticket đó. Muốn lấy thay đổi mới từ upstream thì dùng:

```bash
bbs ticket refresh          # fetch + merge origin/<base>; BLOCK nếu cây bẩn hoặc conflict
```

Chỉ bốn lệnh động vào base ở máy — `merge-base`, `switch`, `reset-base`,
`serve` — và không lệnh nào ghi lên branch ticket.

`ensure` fetch `origin/<base>` trước khi cắt, rồi fork từ ref remote-tracking
với `--no-track`. Hai đường lui, theo thứ tự:

1. **Không có ref `origin/<base>` nào cả** (không có remote, hoặc remote không có
   branch đó) → fork từ `<base>` ở máy.
2. **Fetch lỗi nhưng ref có sẵn** → fork từ `origin/<base>` *biết-lần-cuối* và
   nói rõ: `ensure: warning — fetch failed, using the last-known origin/<base>`.

`bbs ticket board` khép phần chân bằng vị trí thật của base ở máy, để độ lệch
hiện ra trước khi nó cắn:

```
BASE: main — 3 ahead / 12 behind origin/main (last fetch 2h ago)
  ↑ 3 commit(s) on local 'main' are not on origin/main — mode: trunk cuts no branch, so tickets build on them; they are only unpushed
  ↓ origin/main moved on — bbs ticket refresh (in a ticket) or bbs ticket reset-base (on the primary)
```

Nó không bao giờ chặn — base lệch giữa chừng là chuyện bình thường. Board không
fetch, nên các con số mới tới mức tuổi ghi trong ngoặc. Dòng `↑` tự đổi lời theo
mode: khi không cắt gì, ticket dựng *trên* mấy commit đó và điều duy nhất chưa
ổn là chúng chưa được push; còn khi ticket đã cắt từ `origin/<base>` thì chúng
vô hình với mọi ticket cắt sau đó.

---

## Chạy nhiều ticket song song

Nhiều session trong một checkout nghĩa là mọi thay đổi đan vào nhau trên một
branch: bạn **không thể review riêng một ticket, không thể bỏ một cái tồi, không
thể quy trách nhiệm cho một regression**. Worktree mua đúng sự tách bạch đó, và
trả giá bằng một commit + `bbs ticket merge-base` cho mỗi vòng test thay vì
sửa-rồi-refresh. Chính vì cái giá đó mà không ai tự bật nó cho bạn.

### Luật duy nhất

> **Checkout chính ở nguyên trên `base_branch`.** Đó là bề mặt test dùng chung —
> `node_modules` và dev server sống ở đó. Nếu một branch ticket chiếm chỗ đó thì
> không gộp được gì ở đó nữa.

`serve` thực thi luật này chứ không đoán:

```
STATUS: BLOCKED
REASON: primary checkout … is on 'feat/…', not base 'main'.
```

### Cách A — để `foreman` giao việc hộ

Cần [cmux](https://cmux.com), phụ thuộc cứng. Đưa cho nó vài yêu cầu độc lập; nó
mở mỗi ticket một worker nhìn thấy được, mỗi worker trong cmux workspace riêng
chạy autopilot đầu-tới-cuối, và tự xin `--mode=worktree` theo từng lần giao — nên
nó chạy y hệt nhau trên mọi repo, đã cấu hình hay chưa:

```
/bbs:foreman
```

Nó cũng gác cửa thiết kế trước khi có dòng code nào, và có thể merge các ticket
đã xong lên base ở máy để bạn review cả lô cùng lúc.

### Cách B — tự giao việc

```bash
/bbs:autopilot --mode=worktree "<yêu cầu>"             # mỗi ticket một lần
bbs ticket ensure --slug-hint <slug> --mode=worktree   # …hoặc chỉ tạo worktree
```

Repo nào *lần nào cũng* làm kiểu này thì khỏi gõ flag nữa:

```yaml
profile: startup
base_branch: develop
mode: worktree        # mỗi ticket có checkout riêng
land: local           # …và lô đã xong được gộp ở máy trước khi mở PR
```

`land: local` là thứ biến điểm-chốt QA-người-gộp thành bàn giao mặc định. Nó chỉ
có nghĩa khi đi kèm `mode: worktree`: một branch cắt tại chỗ kéo checkout chính
rời `base_branch`, và mọi lần gộp sau đó sẽ BLOCK.

### Rồi: gộp, review, ship

```bash
bbs ticket board                  # trạng thái: ticket, verdict, PR, ai đang giữ lease
bbs ticket serve                  # gộp TẤT CẢ ticket đã xong (qa + review-pr DONE) lên base ở máy
bbs ticket serve <t1> <t2>        # …hoặc đúng những cái bạn gọi tên
bbs ticket serve --release        # trả lại surface
```

`serve <t1> <t2>` gộp đúng những ticket bạn gọi tên — phần thưởng là bạn có thể
bỏ một cái tồi ra ngoài mà mỗi cái còn lại vẫn về đích bằng PR sạch của nó.

| lệnh | làm gì |
|---|---|
| `bbs ticket merge-base` | chạy **từ worktree** — đưa ticket đó lên bề mặt dùng chung để QA |
| `bbs ticket switch <t…>` | chạy **từ checkout chính** — reset về base rồi merge đúng các ticket được gọi tên |
| `bbs ticket reset-base` | sau khi PR merge trên remote, kéo base ở máy về đúng `origin/<base>` |
| `bbs ticket qa-lease` | mỗi lúc một phiên QA trên bề mặt dùng chung; người khác BLOCK kèm tên chủ lease |
| `bbs ticket land <t…>` | merge các ticket đã xong vào base ở máy và **giữ** merge đó (xem dưới) |

Tất cả đều từ chối lớn tiếng chứ không làm mất việc.

### Vòng QA khi ticket sống trong worktree

1. Code và **commit trong worktree**.
2. `bbs ticket merge-base` từ worktree.
3. QA thấy lỗi → sửa **trong worktree**, commit, chạy lại `merge-base`. Đừng bao
   giờ sửa trong checkout base — QA phải test một trạng thái ticket đã commit.
4. Push branch ticket; `create-pr` nhắm vào `base_branch`.
5. Sau khi PR merge: `bbs ticket reset-base` từ checkout chính; các worktree còn
   dang dở chạy lại `merge-base`.

### `auto_land` — để ticket đã xong tự merge vào base

`serve` là một lượt *nhìn*, không phải một lượt hạ cánh: nó reset base rồi gộp
lại từ đầu mỗi lần, nên không gì nó đặt ở đó sống sót qua lần `reset-base` kế
tiếp. `land` thì ngược lại — một merge `--no-ff` vào base ở máy và ở lại đó.

Repo có thể xin cho việc đó tự xảy ra:

```yaml
profile: startup
auto_land: true
```

Giờ foreman merge từng ticket vào base ở máy ngay khi worker của nó xong, thay vì
để lại một đống branch cho bạn tự gộp. Bạn review branch base — vốn đã chạy sẵn
trên dev server — rồi push.

Mặc định nó tắt và phải bật thủ công theo từng repo, vì đây là key duy nhất dịch
chuyển branch base của bạn trong lúc bạn ngủ. Những chốt chặn khiến điều đó sống
được:

- **qa và review-pr đều phải `DONE`**, theo từng ticket, không có `--force` nào
  để với tới. Việc chưa được kiểm chứng không bao giờ hạ cánh.
- **Cả lô được kiểm trước khi bất kỳ cái nào merge** — một ticket thứ ba bị chặn
  không để lại hai cái đầu trên một base bạn không hề yêu cầu.
- **Nó giữ QA lease**, nên không thể dịch base dưới chân một phiên QA đang chạy.
- **Nó không bao giờ push.** Base kết thúc ở phía trước origin và cú push vẫn là
  của bạn.
- **Chạy lại là no-op** (`LANDED=0 … already on main`).

Đừng chạy `serve` sau khi đã land: `serve` gọi `reset-base`, kéo base về origin và
vứt hết các merge — branch ticket vẫn giữ việc, nhưng bề mặt review của bạn biến
mất.

---

## Đổi profile

Chạy lại `/bbs:setup-project` — trên repo đã cấu hình, nó mời bạn đổi và chỉ ghi
lại `profile:`. Đổi profile là đổi chỗ review và độ gắt QA; nó không bao giờ dời
việc đang dang dở.

Đổi `base_branch` sang `develop` trên repo chưa có nhánh đó: tạo và push nó từ
`main` **trước** khi đổi, không thì lần cắt đầu tiên không thấy `origin/develop`
và sẽ fork từ base ở máy.

```bash
git switch -c develop main && git push -u origin develop
```

Viết tay `mode:`, `land:`, `push:` hay `auto_land:` sẽ đè lên preset của profile.
Đó là cửa thoát hiểm, không phải hình dạng bình thường — một cái núm viết ra tay
là một cái núm thôi bám theo profile của nó. `mode:` được đọc tại thời điểm cắt
branch, nên thêm hay bỏ nó chỉ ảnh hưởng ticket mới; trước khi chuyển *vào* lối
làm việc worktree thì checkout chính phải sạch và đứng trên `base_branch`, còn
trước khi chuyển ra thì phải xong hoặc gác các worktree dang dở (`bbs ticket
board`) và trả mọi qa-lease.

Schema đầy đủ và bảng suy dẫn: [`.claude/skills/references/git-flow.md`](../.claude/skills/references/git-flow.md);
phần máy móc worktree: [`references/worktrees.md`](../.claude/skills/references/worktrees.md).
