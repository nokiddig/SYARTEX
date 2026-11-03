# Giải thích README.md của ARTEX

> File này giải thích ý nghĩa từng mục trong `README.md` gốc (viết song ngữ Anh/Trung),
> theo đúng thứ tự xuất hiện trong file. Đây là phần **giải thích/diễn giải**, không phải
> bản dịch nguyên văn — muốn xem câu lệnh/cấu hình chính xác, luôn đối chiếu lại
> `README.md` hoặc `HUONG_DAN_SU_DUNG.md` đã có trong repo.
>
> **Lưu ý quan trọng**: mục cuối cùng của README ("Giấy phép & Miễn trừ trách nhiệm")
> ghi rõ ARTEX **chỉ dùng cho học tập/nghiên cứu trong môi trường lab cục bộ, cô lập** —
> **cấm dùng để test bất kỳ hệ thống/website trực tuyến thật nào**, dù có uỷ quyền hay
> là tài sản của chính mình. Đọc kỹ mục 11 trước khi dùng.

## 1. Khối giới thiệu đầu trang

Dòng mô tả ngắn: ARTEX là **hệ thống pentest tự động đa agent (multi-agent)**, backend
viết bằng Go, frontend Next.js. Badge hiển thị phiên bản release mới nhất và giấy phép
(AGPL-3.0). Câu giới thiệu súc tích nhất của cả README:

> "planner và worker cùng chia sẻ 1 đồ thị tài sản (asset graph) và 1 đồ thị khám phá
> (exploration graph), có phê duyệt người-trong-vòng-lặp (human-in-the-loop) và ghi lại
> toàn bộ traffic. Đóng gói thành 1 binary hoặc 1 cụm Docker Compose."

## 2. Mục "截图预览" (Ảnh chụp màn hình xem trước)

Bảng ảnh minh hoạ các màn hình chính của UI: Dashboard, danh sách Task, chi tiết thực thi
task (session/tool call), Đồ thị khám phá (Exploration Graph), Findings, Assets, Đồ thị
độ phủ tài sản (asset coverage — layout lực hấp dẫn, highlight phần đã test), Traffic
(ghi lại request/response), Chat (hội thoại người-trong-vòng-lặp), Agent management, LLM
config, Intercept/Approval, System logs. Đây chỉ là ảnh demo UI, không có nội dung kỹ
thuật cần làm theo.

## 3. "审批记录详情" (Chi tiết bản ghi phê duyệt)

Giải thích rằng cả 3 nơi hiển thị phê duyệt (trang "Approval records" toàn cục, tab
"Intercept approvals" trong 1 task, và card phê duyệt trong khung chat) đều cho **mở
rộng xem chi tiết**. README ghi nhận bố cục UI này được tham khảo từ component chi tiết
phê duyệt của dự án mã nguồn mở **AegisHook**, nhưng dùng lại component/theme riêng của
ARTEX (không copy nguyên code).

## 4. "资产同步（ScopeSentry）" — Đồng bộ tài sản

ARTEX có thể kéo dữ liệu tài sản (domain/subdomain/IP/port/site/endpoint) thẳng từ công
cụ recon **[ScopeSentry](https://github.com/Autumn-27/ScopeSentry)** mà không cần quét
lại từ đầu:
1. Vào trang "资产同步" (Asset Sync), điền địa chỉ + API Key của ScopeSentry.
2. Chọn phạm vi đồng bộ theo **dự án (project)** hoặc theo **task**, cùng loại tài sản
   muốn lấy.
3. Nhập 1 lần — dữ liệu được gộp vào đúng company/scope tương ứng trong Asset Graph của
   ARTEX, agent dùng được ngay.

## 5. "安装" (Cài đặt) — 5 cách cài

| Cách | Tóm tắt |
|---|---|
| **① Script tự động `./install.sh`** (khuyến nghị) | Tự kiểm tra/cài Docker, hỏi bạn chọn chạy toàn bộ bằng Docker hay build Go chạy local, tự sinh `.env`/`config.json`. |
| **② Docker Compose thủ công** | `cp .env.example .env` (điền `POSTGRES_PASSWORD`, tuỳ chọn `ANTHROPIC_API_KEY`) → `docker compose up -d`. Image có sẵn ripgrep/curl/vim/npm/nmap…; `./skills` và `./data` bind-mount để giữ dữ liệu. |
| **③ Build từ source ra 1 binary** | Build frontend tĩnh (`npm run build:static`) → copy vào `server/webui/dist` → `go build -tags embedui` → chạy `./start.sh`. |
| **⑤ Build release đa nền tảng** | `./build.sh --release` sinh sẵn file zip cho Linux/macOS/Windows nhiều kiến trúc; có cờ `--upx` để nén thêm (có thể không tương thích mọi môi trường nên không bật mặc định). |

Dù cách nào, sau khi cài xong mở `http://localhost:8787`, lần đầu vào sẽ được đưa tới
`/setup` để đặt mật khẩu admin.

README cũng ghi chú riêng về **Remote MCP kiểu SSE cũ**: server mở stream bằng `GET
/sse`, nhận JSON-RPC qua `/message?sessionId=...` — khi cấu hình trong System Settings
điền URL là `/sse` và header `Authorization=Bearer <token>`.

## 6. "更新升级" (Cập nhật/nâng cấp) — 5 cách

Nguyên tắc chung: **cập nhật chỉ thay phần chương trình, không đụng dữ liệu** (Postgres
volume, `./data` chứa `jwt.key`/SQLite, `./skills` đều giữ nguyên); schema DB tự migrate
idempotent mỗi lần khởi động (không cần chạy migration tay).

- **① 1-click trên UI** (khuyến nghị): vào System Settings → card "Version & Update" →
  bấm Update. Cơ chế: tải bản mới → so khớp `SHA256SUMS` → smoke-test bằng `-h` → thay thế
  → tự restart. Nếu lỗi: giữ bản cũ, không để lại bản hỏng; nếu bản mới lỗi 3 lần liên
  tiếp sau khi thay, **tự rollback**. Có thể bấm "rollback về bản trước" bất kỳ lúc nào.
  **Lưu ý**: update sẽ làm gián đoạn task đang chạy (vì cần restart) — nên làm lúc rảnh.
  Bản `dev`/build từ git không cho update qua đường này (tránh đè code debug cục bộ).
  Docker: update kiểu này chỉ thay binary trong container đang chạy, **không** đổi
  image — muốn đổi cả image (kéo theo playwright/nmap mới) phải dùng `docker compose
  pull` + `up -d` thủ công.
- **② Script `./update.sh`**: hỏi git pull trước, rồi chọn Docker hoặc build local.
- **③ Docker Compose thủ công**: `git pull` (tuỳ chọn) → `docker compose pull artex` →
  `docker compose up -d artex` → `docker image prune -f` dọn image cũ.
- **④ Tải binary dựng sẵn từ Releases**: ghi đè `artex` + `skills/`, giữ `config.json` và
  `data/`.
- **⑤ Build lại từ source**: giống cách cài ③ nhưng chạy sau `git pull`.

## 7. "配置" (Cấu hình)

- **Database**: `config.json` (field `database.host/port/user/password/dbname/sslmode`,
  hoặc 1 dòng DSN duy nhất), có thể override bằng biến môi trường `ARTEX_PG_DSN` (biến
  môi trường luôn thắng file).
- **LLM**: set `ANTHROPIC_API_KEY` hoặc `OPENAI_API_KEY`, hoặc điền trực tiếp trong UI ở
  trang "LLM 配置". Biến môi trường tuỳ chọn thêm: `ARTEX_LLM_PROVIDER` /
  `ARTEX_LLM_MODEL` / `ARTEX_LLM_BASE_URL` / `ARTEX_LLM_PROXY`.
- **Số worker song song**: cấu hình trong "系统设置" (System Settings), mặc định 3
  worker/task.
- **Tham số khởi động**: `./start.sh -addr :8787 -proxy :8788` — `-addr` là cổng
  frontend+API, `-proxy` là cổng của proxy ghi lại traffic; script chỉ truyền nguyên các
  cờ này cho `artex`, không tự xử lý.

### Mục con: triển khai sau reverse proxy (HTTPS)

Vì frontend + API/SSE dùng chung 1 cổng và stream realtime đi **same-origin**, không cần
set `NEXT_PUBLIC_SSE_BASE` trong trường hợp thông thường — chỉ mở 443 ra ngoài, giữ 8787
nội bộ. Điểm bắt buộc: **SSE là long-lived connection, reverse proxy phải tắt buffering**
(`proxy_buffering off`, `proxy_cache off`, `proxy_http_version 1.1`, timeout dài) — nếu
không, biểu hiện lỗi là "activity stream quay vòng (loading) không dừng" dù kết nối vẫn
mở được. `NEXT_PUBLIC_SSE_BASE` chỉ cần khi SSE phải tách sang origin khác, và **biến này
bị đóng băng vào bundle tĩnh lúc `next build`** — đặt lại lúc container chạy sẽ vô tác
dụng, phải build lại.

## 8. "开发" (Phát triển)

### Mục con: Phục hồi/kiểm tra lại lỗ hổng thủ công (复测 / Retest)

Giải thích chi tiết luồng Retest (đã mô tả ở các câu trả lời trước trong hội thoại này):
chọn 1 finding → xem lịch sử kết luận + bằng chứng cũ → bấm "发起复测" (phát động retest),
điền tuỳ chọn phiên bản đã vá/điều kiện/giới hạn test → hệ thống tạo 1 **session Agent
retester độc lập**. Agent `retester` được tạo sẵn từ lần khởi động đầu (sửa được
prompt/LLM/budget/tool trong Agent management), dùng LLM riêng nếu có gán, không thì dùng
config LLM toàn cục đang active. 3 kết luận có thể: **"仍可复现" (vẫn tái hiện được)**,
**"已修复" (đã vá)**, **"无法确认" (không xác định được)** — chỉ kết luận "đã vá" mới
**tự động** đổi trạng thái xử lý (disposition) của finding, các kết luận khác giữ nguyên.
Nếu finding đang có 1 session retest chạy, bấm lại sẽ dùng chung session đó (không tạo
trùng); chỉ tạo mới được khi session cũ đã stop/fail hoặc sau khi service restart. Giới
hạn hiện tại: lịch sử retest chỉ xem được trong chi tiết finding/session, **chưa** được
đưa vào báo cáo xuất ra hay gói lưu trữ (archive) của task, và không tự gắn traffic. Chế
độ demo/mock chỉ sinh record giả có đánh dấu rõ, không gọi request thật.

### Mục con: Chạy và test local

- `./dev.sh`: chạy đồng thời backend (`:8787`) + traffic proxy (`:8788`) + frontend
  `next dev` (`:5173`) → mở `http://localhost:5173` để có hot-reload khi code frontend.
- Chạy backend riêng: `go run ./cmd/artex` (không có `-tags embedui` thì **không** nhúng
  frontend — chỉ dùng được API, không có UI phục vụ kèm).
- Chạy frontend riêng: `cd web && npm run dev` (tự proxy `/api` sang backend).
- Chạy test: `go test ./...`.
- Xem UI không cần backend thật: `cd web && NEXT_PUBLIC_MOCK=1 npm run dev` — toàn bộ data
  là giả, hữu ích để xem giao diện mà không cần dựng Postgres/LLM key.

## 9. "系统技术架构" (Kiến trúc kỹ thuật hệ thống) — phần dài và quan trọng nhất

README gọi đây là hệ thống **đa agent điều khiển bởi LLM**: 1 backend Go duy nhất (nhúng
sẵn frontend Next.js) + PostgreSQL; năng lực agent lấy từ SDK
**[`norma`](https://github.com/Autumn-27/norma)** (các module `agentcore`/`tool`/
`permission`/`harness`/`memory`/`transcript`). Lõi kiến trúc là **2 đồ thị (dual-graph)**
cộng với 2 cơ chế giúp hệ thống "tự chủ" hơn.

### 9.1. Sơ đồ phân lớp tổng thể (mermaid flowchart)

Mô tả luồng: **UI** (Next.js, nhúng bằng `go:embed`) gọi HTTP tới **server** (REST
`/api/*` + JWT auth + SSE) → **Manager** quản lý vòng đời task/engine/store → **engine**
(vòng lặp điều phối) điều khiển 3 loại **agent**: `planner` (người lập kế hoạch — nơi
**duy nhất** sinh ra "intent"), `worker` ×N (người thực thi), `mainagent` (vai trò
người-trong-vòng-lặp cho chat). Cả 3 đều đọc/viết vào **PostgreSQL** — nơi lưu 2 đồ thị
(Asset Graph + Exploration Graph). Worker còn nối ra các hệ thống hỗ trợ: **proxy ghi
traffic** (MITM+CA, ghi lại mọi lệnh Bash/HTTP), **guard/intercept** (cổng phê duyệt tool
nhạy cảm), **enrich** (bổ sung DNS/HTTP bất đồng bộ). Cả 3 loại agent đều có thể gọi ra
**MCP / skills / memory / report** (đường nét đứt — kết nối tuỳ chọn, không bắt buộc).

Bảng phân lớp kèm theo tóm tắt trách nhiệm mỗi lớp: Frontend (hiển thị), server (routing +
auth + SSE, Manager quản lý lifecycle), engine (1 `plannerLoop` + N worker goroutine mỗi
task — nhận/nhả intent, xử lý timeout/pause/drain), agent (goals/planner/worker/mainagent,
`ToolSet` biến 2 đồ thị thành tool cho LLM gọi), db (2 đồ thị lưu bằng pgx, schema tự tạo
idempotent mỗi lần start), hỗ trợ (proxy MITM, cổng phê duyệt, enrich bất đồng bộ,
MCP/skill/memory/report).

### 9.2. Kiến trúc 2 đồ thị: Exploration Graph + Asset Graph

Tách riêng "**mục tiêu là gì**" và "**đã test tới đâu**" thành 2 đồ thị độc lập, nối với
nhau qua **anchor (điểm neo)**:

- **Asset Graph (chia sẻ toàn cục, xuyên mọi task)**: kho sự thật về tài sản. Node:
  `root_domain / subdomain / ip / service / app / endpoint`, gắn với company. Quan hệ
  cha-con (domain → subdomain → service → endpoint) và key chống trùng đều do **chương
  trình tự tính** — agent chỉ nộp thông tin thô, không tự quyết cấu trúc đồ thị.
- **Exploration Graph (riêng từng task)**: quá trình "nghĩ và tiến triển" của 1 task. Node:
  `goal (mục tiêu) / intent (ý định) / fact (sự thật) / finding (lỗ hổng) / hint (gợi ý)`,
  nối bằng các cạnh `spawns / derived_from / yields / proves` tạo thành **chuỗi huyết
  thống (lineage)** — trả lời được câu "hướng nào sinh ra từ sự thật nào, cho ra kết quả
  gì".
- **2 đồ thị nối qua anchor**: bảng `exploration_anchors(node_id, asset_id)` gắn
  intent/fact/finding vào đúng 1 tài sản cụ thể — nên vừa xem được "từ hướng khám phá, nó
  đánh vào tài sản nào", vừa tra ngược được "từ 1 tài sản, nó bị test bởi những intent
  nào, ra fact gì". Đây cũng là cơ sở cho **độ phủ tài sản (asset test coverage)** và
  **đồ thị độ phủ tài sản** (tài sản trong scope + highlight phần đã test) trên UI.

Sơ đồ mermaid minh họa: `goal` --spawns--> `intent A` --yields--> `fact` --derived_from-->
`intent B` --proves--> `finding`; song song đó `root_domain --> subdomain --> service -->
endpoint` ở Asset Graph, với các đường nét đứt "anchor" nối intent/fact/finding sang đúng
node tài sản tương ứng.

> **Phân công rõ ràng**: `planner` đọc tình trạng đồ thị khám phá, đánh giá mục tiêu, **chỉ**
> khi có hướng chưa-được-phủ mới cử **intent** vào frontier; `worker` nhận **1 intent**,
> dùng tool thật thực thi, ghi tài sản/fact/finding mới ngược lại 2 đồ thị rồi dừng. Asset
> Graph là sự thật chung; Exploration Graph là chuỗi tiến triển riêng của từng task.

### 9.3. Vòng đời engine & intent (closed loop của 1 lượt khám phá)

Engine hoạt động theo mô hình **event-driven (hướng sự kiện)**: đồ thị vừa đổi → đánh thức
planner → planner cử intent → worker nhận và thực thi, ghi lại → việc ghi lại lại kích
hoạt vòng kế tiếp — lặp đến khi mục tiêu được chứng minh (`prove_goal`).

Sequence diagram minh hoạ 7 bước:
1. Sự kiện đổi đồ thị (đã debounce) đánh thức `planner`.
2. Planner đọc tình trạng (`graph_overview` prefetch + coverage/scope) từ DB.
3. Planner cử 0..N intent (kèm `asset_ids`) vào hàng đợi `frontier` — *đa số lần đánh thức
   cử ra 0 intent*, vì không còn hướng mới nên coi như xong lượt đó.
4. Worker gọi `claimNext` để nhận 1 intent từ frontier.
5. Worker lấy tài sản thô ứng với `asset_ids` của intent đó làm thông tin khởi đầu.
6. Worker thực thi tool thật (Kali/Bash/HTTP) — đi qua **proxy ghi lại** (full traffic +
   xác thực bằng CA riêng của ARTEX).
7. Worker ghi `fact`/`asset`/`finding` + từng bước `activity` ngược lại DB → đồ thị lại đổi
   → đánh thức planner lần nữa, khép vòng lặp.

### 9.4. Trao đổi thông tin cấp quá trình giữa các worker

Vấn đề: trong một lượt khám phá sâu, nhiều quan sát có giá trị (1 lỗi, 1 đoạn response, 1
tham số ẩn) chỉ xuất hiện trong **quá trình thực thi** của 1 worker, chưa kịp ghi thành
fact chính thức. Để tránh làm lại việc đã làm và cho worker "đứng trên vai nhau", mỗi
worker có khả năng **tìm kiếm xuyên quá trình của worker khác**:

- `search_all_worker_traces(q)`: tìm theo từ khoá trong **toàn bộ quá trình của các work
  khác trong cùng task** (tự loại trừ các bước của chính intent mình đang làm), kết quả
  trả về kèm `intent_id` để tra tiếp.
- `list_worker_traces` / `get_worker_trace(intent_id, step_ids=[…])`: xem có work nào đã
  chạy, rồi lấy chi tiết đầy đủ vài bước cụ thể của 1 work để "trao đổi" thông tin.

Nhờ vậy dù đồ thị khám phá chưa có fact tương ứng, worker sau vẫn tái dùng được quan sát từ
quá trình của worker trước — **thông tin lưu chuyển giữa worker ở cấp "quá trình thực
thi"**, còn ranh giới nhiệm vụ vẫn giữ nguyên (mỗi worker chỉ làm đúng 1 intent mình nhận).
Sơ đồ minh hoạ: worker A/B ghi `activity` từng bước vào kho activity của Exploration Graph;
worker C gọi `search_all_worker_traces` để tìm, nhận lại các bước khớp của A/B (trừ của
chính C), rồi gọi `get_worker_trace` lấy nội dung đầy đủ.

### 9.5. Todolist chia sẻ qua nhiều vòng của planner → chuỗi tấn công ổn định

Vấn đề: 1 chuỗi tấn công thật thường có **phụ thuộc tuần tự** (tìm điểm injection → lấy
credential → di chuyển ngang → leo quyền) — nếu cử tất cả các bước này song song cùng lúc
sẽ loạn. Vì mỗi lần được đánh thức planner chạy **một phiên hoàn toàn mới** (không nhớ gì
phiên trước), nó cần giữ 1 **todolist quy hoạch được lưu theo task, tồn tại xuyên suốt các
lần đánh thức**:

- Todolist giúp planner **ghi lại 1 lần** cả chuỗi khai thác nhiều bước có phụ thuộc, rồi
  **cử intent từng bước theo đúng thứ tự phụ thuộc** ở các vòng sau — không phải mở hết
  toàn bộ chuỗi ngay trong 1 vòng.
- Mỗi vòng chỉ cử intent cho bước kế tiếp **khi bước trước đã có fact xác nhận** (tiền đề
  đã thoả), đồng thời cập nhật lại danh sách (đánh dấu hoàn thành bước đã được fact thoả
  mãn).

Sơ đồ minh hoạ: todolist có 4 bước (1.Tìm injection [đã xong] → 2.Lấy credential [đang làm]
→ 3.Di chuyển ngang [chờ tiền đề] → 4.Leo quyền [chờ tiền đề]); vòng 1 cử intent① cho bước
1; vòng 2 (sau khi ① ra fact) cử intent② cho bước 2; vòng 3 (sau khi ② ra fact) cử intent③
cho bước 3 — tiến triển tuần tự, đúng thứ tự.

Kết luận của README: nhờ 2 cơ chế này (trao đổi quá trình + todolist chia sẻ), hệ thống vẫn
**tiến triển ổn định, không lặp lại, không sai thứ tự** dù chạy trong mô hình "mỗi phiên
agent là stateless" — đây được nêu là **điểm mấu chốt** giúp ARTEX tự chủ đi hết một chuỗi
khai thác nhiều bước.

## 10. "交流群" (Nhóm trao đổi)

Quét QR theo dõi 1 tài khoản WeChat Official Account tên **"SecSentry"**, nhắn tin riêng
trong đó để được thêm vào group trao đổi. (Lưu ý: đây là group tiếng Trung, dành cho người
dùng Trung Quốc; không có kênh tiếng Anh/Việt chính thức được nêu trong README.)

## 11. Ghi công & tham khảo

- Repo gốc đang maintain: **Autumn-27/ARTEX** (AGPL-3.0).
- Fork theo dõi cập nhật mà bạn đang dùng: **trhhubsd/ARTEX**.
- Tham khảo thêm: **oritera/Cairn** (một dự án khác có kiến trúc tương tự, dùng làm nguồn
  tham khảo khi thiết kế ARTEX).

## 12. "许可与免责声明" (Giấy phép & Miễn trừ trách nhiệm) — PHẦN QUAN TRỌNG NHẤT

### Giấy phép mã nguồn mở

AGPL-3.0: ai cũng được dùng/sửa/phân phối lại tự do, **nhưng** nếu sửa đổi và cung cấp qua
mạng (deploy làm dịch vụ online), **bắt buộc công khai toàn bộ mã nguồn** của bản sửa đó
cho người dùng dịch vụ.

### Ràng buộc bổ sung của tác giả (nằm ngoài giấy phép, vẫn phải tuân theo)

> Giấy phép mã nguồn mở không giới hạn mục đích sử dụng — nhưng tác giả đặt thêm ràng buộc
> sau, và yêu cầu người dùng **phải tuân thủ**:

- **ARTEX chỉ dành cho học tập cá nhân, nghiên cứu mã nguồn, xác minh kỹ thuật trong môi
  trường local/lab cô lập — không được dùng để test thực tế lên bất kỳ hệ thống/website
  trực tuyến nào.**
- **Phạm vi được phép**: đọc/học/nghiên cứu source code; xác minh nguyên lý kỹ thuật trong
  môi trường cách ly cục bộ; mục đích học thuật/phi tấn công.
- **Phạm vi bị cấm** (nêu rõ từng điểm):
  - Cấm dùng để scan/dò/khai thác/tấn công **bất kỳ** website/dịch vụ online/hệ thống kết
    nối mạng nào — **dù đã được uỷ quyền hay là tài sản của chính người dùng**;
  - Cấm dùng cho pentest/đối kháng/môi trường production thực tế nói chung;
  - Cấm dùng để xâm nhập trái phép, đánh cắp dữ liệu, tống tiền, DoS, hoặc bất kỳ hành vi
    phá hoại/phạm pháp nào;
  - Cấm dùng để vi phạm pháp luật nơi người dùng sinh sống/hoạt động.
- **Trách nhiệm pháp lý**: người dùng tự chịu trách nhiệm tuân thủ pháp luật địa phương về
  an ninh mạng/bảo vệ dữ liệu (ví dụ ở Trung Quốc: Luật An ninh mạng, Luật Bảo vệ dữ liệu,
  Luật Bảo vệ thông tin cá nhân...).
- **Miễn trừ trách nhiệm**: phần mềm cung cấp "nguyên trạng" (AS IS), không bảo đảm gì;
  tác giả/contributor không chịu trách nhiệm cho bất kỳ thiệt hại trực tiếp/gián tiếp nào
  dù dùng đúng hay sai cách. Việc tải/cài/dùng project đồng nghĩa đã đọc, hiểu và đồng ý
  toàn bộ điều khoản trên.

---

*File này do trợ lý tạo ra để giải thích nội dung `README.md` hiện có trong repo, không
thay thế tài liệu gốc — khi `README.md` thay đổi, file này cần được cập nhật lại tương
ứng.*
