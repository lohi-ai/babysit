# Đi tìm unknowns của chính mình: bạn không thể prompt cho thứ mình còn chẳng biết là nó tồn tại

*Tóm tắt "A Field Guide to Fable" của Thariq — vì sao nút thắt bây giờ là unknown unknowns của bạn, chứ không phải con model.*

Làm việc với Claude Fable 5 cứ dạy đi dạy lại tôi một bài học cũ: **the map is not the territory** — bản đồ không phải là lãnh thổ.

Bản đồ là thứ bạn đưa cho Claude — prompt, skill, context, tức bản mô tả của công việc cần làm. Còn lãnh thổ là nơi công việc thật sự diễn ra: codebase, thế giới thực, với đống ràng buộc thật của nó. Chênh lệch giữa hai thứ đó chính là **unknowns** của bạn. Mỗi lần đụng một unknown, Claude phải tự đoán xem bạn muốn gì rồi quyết — và việc càng nhiều, số unknowns nó đụng phải càng dày.

Fable là con model đầu tiên mà tôi thấy chất lượng công việc bị nghẽn không phải ở model, mà ở **khả năng của bạn trong việc làm rõ unknowns cho nó**. Bạn không thể prompt cho một thứ mà bạn còn chẳng biết là nó tồn tại.

Và plan trước kỹ cỡ nào cũng chưa chắc đủ. Unknowns có thể lòi ra giữa lúc implement, thậm chí có unknown còn chỉ cho bạn thấy đáng lẽ phải giải một bài toán khác hẳn. Làm việc với Fable là một vòng lặp đi tìm unknowns **trước, trong, và sau** khi implement.

## Biết unknowns của mình

Gặp bài toán nào cũng mổ theo bốn ô:

| Ô | Định nghĩa |
|---|---|
| Known knowns | Những gì nằm sẵn trong prompt — điều bạn nói thẳng với con agent |
| Known unknowns | Điều bạn chưa nghĩ ra, nhưng biết là mình chưa nghĩ ra |
| Unknown knowns | Hiển nhiên tới mức chẳng buồn viết ra — nhưng nhìn phát là nhận ra ngay |
| Unknown unknowns | Điều bạn chưa từng nghĩ tới. Bạn có biết một thứ có thể tốt tới cỡ nào không? |

Mấy người giỏi agentic coding nhất là mấy người có ít unknowns — họ thuộc codebase và nắm nết con model như lòng bàn tay. Nhưng họ cũng luôn *mặc định* là unknowns có tồn tại. Giảm unknowns và tính trước đường cho tụi nó chính **là** kỹ năng của agentic coding — và may là kỹ năng này rèn được, bằng chính việc làm việc với Claude.

## Giúp Claude giúp bạn

Chỉ việc cho Claude là một trò đi dây. Quá cụ thể thì nó bám khư khư chỉ dẫn của bạn ngay cả khi đáng lẽ nên rẽ hướng. Quá mơ hồ thì nó tự lấp chỗ trống bằng "best practice" của ngành — thứ chưa chắc hợp với task của bạn. Không tính đến unknowns thì thua cả hai đường: bạn không biết khi nào đường đầy ổ gà, và cũng không biết khi nào đường quang nhưng bạn vẫn muốn nó rẽ.

Claude tìm unknowns giúp bạn nhanh hơn bạn tự tìm nhiều: nó lục codebase với internet nhanh khủng khiếp, biết nhiều hơn bạn về đa số chủ đề, và fail xong đứng dậy đi tiếp cũng nhanh hơn. Thứ quan trọng nhất bạn phải đưa nó là **context về điểm xuất phát của mình**: bạn đang ở đâu trong dòng suy nghĩ, kinh nghiệm với bài toán và codebase tới đâu. Rồi để nó làm việc với bạn như một thought partner.

## Trước khi implement

**Blindspot pass.** Nhảy vào một vùng code lạ là bạn ôm nguyên một rổ unknown unknowns: không biết phải hỏi gì, không biết "tốt" trông ra sao, không biết trước đó ai làm gì rồi, hố nào cần né. Cứ hỏi thẳng, dùng đúng nguyên văn: *"Do a blindspot pass to help me find my unknown unknowns and help me prompt you better."* Nhớ khai luôn bạn là ai và đã biết những gì.

**Brainstorm và prototype.** Vùng nào đầy unknown knowns — mấy tiêu chí kiểu *thấy mới biết* — thì brainstorm và prototype trước. Để tới lúc implement mới phát hiện thì đắt: spec đổi một tí là code đổi một trời, mà revert thì cực. Visual design là ca kinh điển: bảo nó làm vài hướng thiết kế khác nhau một trời một vực trong đúng một file HTML để bạn *phản ứng*, trước khi đụng vào app thật. Mở màn session nào cũng bằng brainstorm còn giúp căn scope — Claude hay tìm ra mấy hướng ngon mà bạn kiểu gì cũng bỏ sót.

**Interview.** Brainstorm xong, unknowns vẫn còn đó. Bảo Claude phỏng vấn ngược lại bạn: *"mỗi lần một câu — ưu tiên mấy câu mà câu trả lời của tôi sẽ làm kiến trúc đổi hướng."*

**Reference.** Có những thứ bạn muốn mà tả không nổi — thiếu chữ, hoặc nói ra thì dài dòng quá. Reference tốt nhất là source code. Trỏ Fable vào cái folder có đúng behavior bạn muốn — viết bằng ngôn ngữ khác cũng được — và nói nó cần soi cái gì: *"Crate Rust trong vendor/rate-limiter có đúng backoff behavior tôi muốn. Đọc nó rồi làm lại y semantics trong TypeScript client bên mình."*

**Implementation plan.** Sẵn sàng build rồi thì xin một bản plan **mở màn bằng mấy quyết định dễ đổi nhất** — data model, type interface, UX flow — còn phần refactor máy móc thì chôn xuống cuối. Những thứ bạn thật sự cần chỉnh sẽ tự nổi lên trên.

## Trong khi implement

**Implementation notes.** Plan kỹ cỡ nào thì unknown unknowns vẫn rình sẵn đâu đó. Con agent kiểu gì cũng đụng edge case buộc phải lệch plan. Bắt nó giữ một file `implementation-notes.md`: *"Edge case nào ép phải lệch plan — chọn phương án bảo thủ, log vào mục 'Deviations', rồi đi tiếp."* Chính đống notes đó là thứ bạn học được cho lần thử sau.

## Sau khi implement

**Pitch và explainer.** Ship là phải có buy-in. Gói prototype, spec, và implementation notes vào đúng một doc: reviewer khởi đầu với y chang mấy unknowns bạn từng có, còn expert duyệt nhanh hơn hẳn khi thấy bạn đã tính sẵn mấy điểm hỏng mà họ định vặn.

**Quiz.** Sau một session dài, con model có khi đã làm nhiều hơn bạn tưởng cả một khúc. Đọc diff chỉ cho cái hiểu bề mặt — phần lớn behavior nằm ở đống code path cũ mà diff không thèm hiện ra. Xin một bản report có context, có intuition, kèm bài quiz ở cuối. **Quiz chưa đạt điểm tuyệt đối thì chưa merge.**

## Case study: video launch của Fable

Video launch của Fable được edit hoàn toàn bằng Claude Code — một lĩnh vực Thariq mù tịt. Quy trình chính là cả cái field guide này thu nhỏ: xuất phát từ known knowns (Claude edit và transcribe video bằng code được), xin explainer cho khúc chưa chắc (Whisper có đủ chính xác để cắt mấy tiếng "ừm" bằng ffmpeg không?), prototype cái ý tưởng rủi ro nhất (một UI Remotion chạy khớp từng chữ với transcript). Rồi tới lúc video trông cứ xỉn xỉn, anh đâm sầm vào bức tường unknown unknowns: color grading. Lần đầu anh thử kiểu quen tay — sinh vài variation rồi chọn — và fail, vì có biết grading *đẹp* trông thế nào đâu mà chọn. Cách sửa: bảo Claude **dạy** mình color grading trước đã, biến unknown unknowns thành known rồi mới quay lại chọn.

## Khớp bản đồ với lãnh thổ

Model càng giỏi, cách tiếp cận đúng càng đưa bạn đi xa. Một task dài hơi mà trả về kết quả sai thì thủ phạm nhiều khả năng không phải con model — mà là bạn cần ngồi thêm với đống unknowns, hoặc viết plan kiểu chừa đường cho Claude ứng biến xuyên qua tụi nó.

Mỗi explainer, brainstorm, interview, prototype, reference đều là **một cách rẻ để biết được thứ mình chưa biết, trước khi việc sửa thành ra đắt.** Dự án tới, mở màn bằng cách nhờ Claude đi tìm unknowns của chính bạn.

---

*Tóm tắt "A Field Guide to Fable: Finding Your Unknowns" của Thariq (@trq212), x.com/trq212/status/2073100352921215386.*
