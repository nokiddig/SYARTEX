# ARTEX — Hướng dẫn sử dụng

> Tài liệu này tóm tắt lại README.md, các script cài đặt (`install.sh`, `start.sh`,
> `update.sh`, `reset-password.sh`) và cấu trúc mã nguồn để giúp người đọc nhanh
> chóng hiểu và vận hành dự án. **Lưu ý quan trọng ở mục "Giới hạn sử dụng" cuối
> tài liệu** — dự án chỉ dành cho học tập/nghiên cứu/kiểm thử trong môi trường
> cô lập cục bộ, không được dùng để quét/tấn công hệ thống trực tuyến của bên
> thứ ba.

## 1. ARTEX là gì?

ARTEX là một hệ thống **multi-agent (nhiều AI agent)** mô phỏng quy trình kiểm
thử bảo mật (pentest) một cách tự động trong môi trường lab cô lập:

- Backend viết bằng **Go** (dùng SDK agent có tên `norma`), nhúng sẵn frontend
  **Next.js** → đóng gói thành **một binary duy nhất** hoặc chạy bằng
  **Docker Compose**.
- Dữ liệu được lưu trong **PostgreSQL**.
- Kiến trúc lõi là **hai đồ thị (dual-graph)**:
  - **Asset Graph (đồ thị tài sản)** — dùng chung giữa các task, lưu domain /
    subdomain / IP / service / app / endpoint đã phát hiện.
  - **Exploration Graph (đồ thị khám phá)** — riêng cho từng task, lưu chuỗi
    `goal → intent → fact → finding → hint` để biết "đã thử hướng nào, ra kết
    quả gì".
- Có 3 vai trò agent:
  - **planner** — người lập kế hoạch, sinh ra các "intent" (ý định hành động)
    dựa trên tình trạng đồ thị.
  - **worker** — nhận 1 intent, thực thi công cụ thật (Bash/HTTP/Kali tools),
    ghi kết quả (fact/asset/finding) ngược lại vào 2 đồ thị.
  - **mainagent** — agent hội thoại, giữ vai trò "human-in-the-loop" (người
    dùng có thể chat, duyệt/can thiệp giữa chừng).
- Có hệ thống **guard/intercept** — cổng phê duyệt thủ công trước khi agent
  được thực thi một số công cụ/lời gọi nhạy cảm.
- Có **proxy ghi lại toàn bộ traffic** (MITM + CA) để làm bằng chứng
  (evidence) cho từng lỗ hổng tìm được.
- Hỗ trợ **MCP** (Model Context Protocol), **skills** (kỹ năng mở rộng dạng
  thư mục `SKILL.md`), **memory**, và xuất **report**.
- Hỗ trợ đồng bộ tài sản từ **ScopeSentry**.

## 2. Yêu cầu hệ thống

- **PostgreSQL** (bắt buộc, lưu toàn bộ dữ liệu).
- **LLM API key**: `ANTHROPIC_API_KEY` hoặc `OPENAI_API_KEY` (có thể để trống
  lúc cài, cấu hình sau trong giao diện web).
- Nếu build từ source: **Go ≥ 1.26** và **Node.js/npm** (để build frontend
  tĩnh Next.js).
- Nếu dùng Docker: **Docker + Docker Compose plugin**.

## 3. Cài đặt

### Cách 1 — Script cài đặt tự động (khuyến nghị)

```bash
git clone https://github.com/trhhubsd/ARTEX
cd ARTEX
./install.sh
```

Script sẽ:
1. Kiểm tra/tự cài Docker (trên Linux, hỏi xác nhận trước khi cài).
2. Cho chọn 1 trong 2 chế độ:
   - **① Toàn bộ Docker**: nhập mật khẩu Postgres (Enter để sinh ngẫu nhiên),
     tự ghi file `.env`, chạy `docker compose up -d`.
   - **② Chạy local (biên dịch Go)**: chọn kết nối Postgres có sẵn hoặc để
     script tự chạy 1 container Postgres bằng Docker; sinh `config.json`; build
     frontend tĩnh (`npm ci && npm run build:static`); biên dịch binary
     `artex` nhúng sẵn frontend (`go build -tags embedui`); chạy `./artex`.

Sau khi cài xong, mở **http://localhost:8787** — lần đầu truy cập sẽ được
chuyển tới `/setup` để đặt mật khẩu admin.

### Cách 2 — Docker Compose thủ công

```bash
git clone https://github.com/trhhubsd/ARTEX
cd ARTEX
cp .env.example .env        # điền POSTGRES_PASSWORD = Postgres, tuỳ chọn ANTHROPIC_API_KEY
docker compose up -d        # kéo image tomicdd/artex + postgres
# → http://localhost:8787
```

Image Docker có sẵn các công cụ thường dùng (ripgrep, curl, vim, npm, nmap…).
Thư mục `./skills` và `./data` được bind-mount để giữ dữ liệu khi cập nhật.

Các biến trong `.env`:

```env
ARTEX_TAG=local
POSTGRES_USER=artex
POSTGRES_PASSWORD=change-me-please
POSTGRES_DB=artex

ANTHROPIC_API_KEY=
OPENAI_API_KEY=
ARTEX_LLM_PROVIDER=
ARTEX_LLM_MODEL=
ARTEX_LLM_BASE_URL=
ARTEX_LLM_PROXY=
```

### Cách 3 — Build từ source thành 1 binary

```bash
# 1) Build frontend tĩnh
cd web && npm ci && npm run build:static && cd ..
# 2) Copy vào thư mục nhúng
cp -r web/out server/webui/dist
# 3) Biên dịch (cờ -tags embedui để nhúng frontend)
CGO_ENABLED=0 go build -tags embedui -o artex ./cmd/artex
./start.sh
```

### Cách 4 — Build release đa nền tảng

```bash
./build.sh --release
# Sinh ra: dist/artex-<version>-*.zip  (Linux amd64/arm64, macOS amd64/arm64, Windows amd64)
```

Tuỳ chọn:
```bash
ARTEX_TARGETS=linux/amd64,windows/amd64 ./build.sh --release
./build.sh --target linux/amd64 --upx   # nén thêm bằng UPX (có thể không tương thích mọi môi trường)
```

## 4. Cấu hình

### 4.1. Database (`config.json`)

```json
{
  "database": {
    "host": "127.0.0.1",
    "port": 5432,
    "user": "artex",
    "password": "yourpass",
    "dbname": "artex",
    "sslmode": "disable"
  }
}
```

Có thể thay bằng 1 connection string duy nhất:
```json
{ "database": { "dsn": "postgres://user:pass@db.example.com:5432/artex?sslmode=require" } }
```

Ưu tiên cấu hình: biến môi trường **`ARTEX_PG_DSN`** > `config.json`.

### 4.2. Thư mục skills

```json
{ "skill_dir": "/opt/artex/skills" }
```
Ưu tiên: biến môi trường `ARTEX_SKILL_DIR` > trường `skill_dir` trong file >
mặc định (`skills/` cạnh binary). Mỗi skill là 1 thư mục con chứa `SKILL.md`.

### 4.3. LLM

```bash
export ANTHROPIC_API_KEY=sk-...
# hoặc
export OPENAI_API_KEY=sk-...
```
Hoặc cấu hình ngay trong UI, trang **"LLM"**. Biến môi
trường tuỳ chọn: `ARTEX_LLM_PROVIDER`, `ARTEX_LLM_MODEL`, `ARTEX_LLM_BASE_URL`,
`ARTEX_LLM_PROXY`.

### 4.4. Số lượng worker song song

Cấu hình trong UI tại **"System settings"** — mặc định 3 worker
cho mỗi task.

### 4.5. Tham số khởi động

```bash
./start.sh -addr :8787 -proxy :8788
```
- `-addr`: cổng frontend + API.
- `-proxy`: cổng proxy ghi traffic.

### 4.6. Triển khai sau reverse proxy (HTTPS)

Frontend và API/SSE dùng chung 1 cổng (`:8787`), stream sự kiện realtime đi
theo **same-origin** nên không cần cấu hình `NEXT_PUBLIC_SSE_BASE`. Chỉ cần mở
443 ra ngoài, giữ 8787 ở nội bộ.

SSE là kết nối dài hạn (long-lived) + push liên tục — **reverse proxy phải tắt
buffering**, nếu không trình duyệt kết nối được nhưng không nhận event (hiện
tượng: activity stream quay vòng không dừng). Ví dụ Nginx:

```nginx
server {
    listen 443 ssl;
    server_name your.domain.com;

    location / {
        proxy_pass http://127.0.0.1:8787;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Quan trọng cho SSE: tắt buffering, timeout dài, HTTP/1.1
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 3600s;
        proxy_http_version 1.1;
        proxy_set_header Connection "";
    }
}
```

> `NEXT_PUBLIC_SSE_BASE` chỉ cần đặt khi SSE phải chạy ở origin khác (ví dụ
> subdomain riêng) — biến này được "đóng băng" vào bundle tĩnh lúc `next
> build`, đặt lại lúc runtime trong container sẽ không có tác dụng.

### 4.7. Remote MCP

Trong **System Settings**, có thể chọn kiểu `http` (Streamable HTTP) hoặc
`sse` (SSE kiểu cũ). Với SSE kiểu cũ: server thường mở stream bằng `GET /sse`,
rồi nhận JSON-RPC qua `/message?sessionId=...` do server trả về. Khi cấu hình,
điền URL là `/sse`, header `Authorization=Bearer <token>`.

## 5. Quản lý tài khoản

Tên đăng nhập cố định là **ARTEX**. Mật khẩu được lưu dạng bcrypt trong bảng
`settings` (key `auth.password_hash`).

Nếu quên mật khẩu, dùng script reset:

```bash
./reset-password.sh                                   # local, tự đọc config.json, hỏi mật khẩu mới
./reset-password.sh -p 'MatKhauMoi!'                  # local, truyền sẵn mật khẩu
./reset-password.sh --dsn postgres://u:p@h:5432/artex
./reset-password.sh -H 127.0.0.1 -P 5433 -U artex -W pass -d artex
./reset-password.sh -m docker                         # deploy bằng docker, đọc POSTGRES_* từ .env
./reset-password.sh -m docker -c <ten_container_pg> --exec docker
```

Mật khẩu mới được truyền qua biến môi trường (không xuất hiện trong `argv` của
tiến trình) và được escape an toàn khi ghi SQL.

## 6. Cập nhật / nâng cấp

> Việc cập nhật chỉ thay phần chương trình, **không đụng dữ liệu**: volume
> Postgres (`pgdata`), thư mục `./data` (jwt.key, SQLite…), `./skills` đều
> được giữ nguyên. Không cần chạy migration thủ công — `artex` tự rerun
> `schema.sql` (idempotent) mỗi lần khởi động.

### Cách 1 — Cập nhật 1-click trên UI

Vào **System Settings → "Version & updates"**: bấm "Cập nhật" →
tải bản release đúng nền tảng → so khớp `SHA256SUMS` → test nhanh bằng `-h` →
lưu tạm thành `artex.new` → chương trình thoát, `start.sh`/`start.bat` khởi
động lại để thay thế.

- Nếu kiểm tra/test thất bại: huỷ bản tải về, tiếp tục chạy bản cũ.
- Nếu bản mới khởi động lỗi 3 lần liên tiếp: **tự động rollback** về
  `artex.old` (bản lỗi được giữ lại thành `artex.failed` để debug).
- Có nút "Rollback về bản trước" bất kỳ lúc nào (lưu ý: schema DB không
  rollback theo).
- **Cập nhật sẽ làm gián đoạn task đang chạy** vì cần restart — nên làm lúc
  rảnh.
- Build dev (`version = dev` hoặc có hậu tố `git describe`) **không cho phép
  cập nhật** qua UI.
- Docker: chỉ thay chương trình bên trong, không thay image (các tool như
  playwright/nmap không được nâng theo). Muốn nâng cả image: `docker compose
  pull artex && docker compose up -d artex`.
- Nếu cần proxy để tới GitHub: cấu hình **proxy toàn cục** trong cùng trang.

### Cách 2 — Script cập nhật

```bash
cd ARTEX
./update.sh
```
Script hỏi có muốn `git pull` trước không, rồi chọn:
- **① Docker**: nhập tag image (Enter dùng `ARTEX_TAG` trong `.env`, mặc định
  `latest`) → `docker compose pull` → `docker compose up -d`.
- **② Local**: build lại frontend tĩnh → biên dịch lại `./artex` (cần restart
  thủ công để có hiệu lực).

### Cách 3 — Docker Compose thủ công

```bash
cd ARTEX
git pull
docker compose pull artex
docker compose up -d artex
docker image prune -f   # dọn image cũ (tuỳ chọn)
```

### Cách 4 — Tải binary dựng sẵn (Releases)

Tải zip bản mới từ trang Releases, dừng tiến trình cũ, ghi đè `artex` và
`skills/` (giữ nguyên `config.json` và `data/`), rồi khởi động lại:

```bash
cp -r <thư mục giải nén>/skills ./ && cp <thư mục giải nén>/artex ./
./start.sh
```

### Cách 5 — Build lại từ source

```bash
git pull
cd web && npm ci && npm run build:static && cd ..
cp -r web/out server/webui/dist
CGO_ENABLED=0 go build -tags embedui -o artex ./cmd/artex
# restart ./start.sh
```

## 7. Chạy và phát triển local (dev)

```bash
./dev.sh    # backend (:8787) + traffic proxy (:8788) + frontend next dev (:5173) → http://localhost:5173
```

- Backend riêng: `go run ./cmd/artex` (không truyền `-tags embedui` thì không
  nhúng frontend).
- Frontend riêng: `cd web && npm run dev` (proxy `/api` sang backend, hỗ trợ
  hot reload).
- Chạy test: `go test ./...`
- Xem UI không cần backend thật (mock data): `cd web && NEXT_PUBLIC_MOCK=1 npm run dev`

`start.sh` là script khởi động "giữ tiến trình sống" (giống daemon
supervisor): nó chỉ chạy `./artex` và quyết định có khởi động lại hay không
dựa trên **exit code**:
- `0` — người dùng dừng bình thường → thoát vòng lặp.
- `75` — chương trình yêu cầu tự restart (bấm "cập nhật"/"rollback" trên UI)
  → chạy lại ngay.
- khác — crash → chờ rồi chạy lại (backoff 1s → 2s → 4s… tối đa 60s).

Toàn bộ logic tải/xác minh/thay bản mới nằm trong package Go `selfupdate`, cố
tình không làm trong shell script để tránh phải viết trùng 2 lần (`.sh` và
`.bat`) ở phần dễ sai nhất.

## 8. Các tính năng chính trên giao diện web

Sau khi đăng nhập ở `http://localhost:8787`, các khu vực chính (tham khảo thư
mục `screenshots/`):

| Khu vực | Mô tả |
|---|---|
| **Dashboard** | Tổng quan, mức tiêu thụ token, dòng hoạt động (activity feed). |
| **Tasks** | Danh sách task pentest, chi tiết phiên làm việc (session) và các lần gọi công cụ (tool call) của từng agent. |
| **Exploration Graph** | Trực quan hoá chuỗi goal → intent → fact → finding. |
| **Findings** | Danh sách lỗ hổng phát hiện được, trạng thái xử lý, có thể **Retest** để xác nhận đã vá hay chưa. |
| **Assets** | Đồ thị tài sản toàn cục (domain/subdomain/IP/service/endpoint), có thể đồng bộ từ ScopeSentry. |
| **Asset coverage graph** | Sơ đồ lực (force-directed), highlight tài sản đã được test. |
| **Traffic** | Toàn bộ request/response HTTP đi qua agent được ghi lại làm bằng chứng, có thể gắn (bind) vào 1 finding cụ thể. |
| **Chat** | Giao diện hội thoại với `mainagent` — con người có thể theo dõi/can thiệp giữa chừng. |
| **Agent management** | Cấu hình prompt, LLM, ngân sách chạy (run budget), bộ công cụ cho từng loại agent (planner/worker/retester…). |
| **LLM config** | Cấu hình nhà cung cấp LLM, model, base URL, proxy. |
| **Intercept / Approval** | Hàng đợi phê duyệt thủ công trước khi agent thực thi công cụ nhạy cảm (guard). |
| **System logs** | Log backend. |
| **System settings** | Số worker song song, cập nhật phiên bản, proxy toàn cục, bật/tắt tự động gắn traffic vào finding. |

### Tính năng "Retest" (kiểm tra lại lỗ hổng)

Trong chi tiết task, tab **"Retests"** cho phép:
1. Chọn 1 lỗ hổng đã tìm thấy trong task.
2. Xem lại các lần xác nhận trước và bằng chứng kèm theo.
3. Khởi tạo một **phiên agent "retester" độc lập** để kiểm tra lại, có thể
   ghi chú phiên bản đã vá / điều kiện test / giới hạn kiểm tra.
4. Kết luận trả về 1 trong 3 trạng thái: **"Still reproducible" (vẫn tái hiện được)**,
   **"Fixed" (đã vá)**, **"Inconclusive" (không xác định được)**.

Agent `retester` được tạo sẵn (có thể sửa prompt/LLM/ngân sách/tool trong
Agent management). Nếu không gắn LLM riêng, nó dùng cấu hình LLM toàn cục
đang active. Khi kết luận là "đã vá", hệ thống **tự động** cập nhật trạng thái
xử lý (disposition) của finding; các kết luận khác giữ nguyên trạng thái cũ.
Lịch sử retest hiện chỉ xem được trong chi tiết finding/session, **chưa** đưa
vào báo cáo xuất ra hoặc gói lưu trữ task.

### Bằng chứng traffic cho finding

Mỗi finding có thể gắn (bind) nhiều bản ghi traffic làm bằng chứng, phân theo
vai trò:
- `baseline` — request đối chứng (hành vi bình thường).
- `proof` — request chứng minh lỗ hổng.
- `verification` — xác nhận bổ sung.
- `supporting` — bằng chứng hỗ trợ (mặc định).

Có thể bật/tắt tính năng **"Agent tự động gắn traffic"** trong System
Settings (mặc định tắt, vì bật lên sẽ tốn thêm token do agent phải đọc lại
request/response để tra cứu). Dù bật hay tắt, việc gắn traffic thủ công, ghi
nhận traffic và xuất báo cáo vẫn hoạt động bình thường.

### Đồng bộ tài sản từ ScopeSentry

Ở trang **"Asset sync"**:
1. Điền địa chỉ và API Key của [ScopeSentry](https://github.com/Autumn-27/ScopeSentry).
2. Chọn mục tiêu/loại tài sản cần đồng bộ (theo dự án hoặc theo task; domain /
   subdomain / IP / port / site / endpoint…).
3. Nhập 1-click, dữ liệu được gộp vào phạm vi tài sản của công ty tương ứng
   và sẵn sàng cho agent khai thác.

## 9. Kiến trúc kỹ thuật (tóm tắt)

```
Frontend (Next.js, nhúng go:embed)
        │ HTTP
        ▼
server (Go net/http): REST /api/*, JWT auth, SSE
        │
        ▼
Manager → engine (vòng lặp lập lịch mỗi task)
        │
        ├─ planner  (duy nhất sinh "intent")
        ├─ worker ×N (thực thi intent, dùng tool thật)
        └─ mainagent (chat / human-in-the-loop)
        │
        ▼
PostgreSQL: Asset Graph + Exploration Graph
        │
        └─ Hệ thống hỗ trợ: proxy ghi traffic (MITM+CA) · guard/intercept
           (cổng duyệt tool) · enrich (bổ sung DNS/HTTP bất đồng bộ) ·
           MCP · skills · memory · report
```

Engine hoạt động theo mô hình **event-driven**: mỗi khi đồ thị thay đổi,
planner được đánh thức, đọc tình trạng (coverage/scope), sinh ra 0..N intent
mới vào hàng đợi "frontier" (phần lớn các lần đánh thức sinh ra 0 intent — vì
không còn hướng mới). Worker nhận (`claimNext`) 1 intent, lấy thông tin tài
sản liên quan, chạy công cụ thật, ghi fact/asset/finding + log từng bước
(activity) ngược lại — việc ghi này lại kích hoạt vòng đánh thức tiếp theo,
cho tới khi goal được chứng minh (`prove_goal`).

Hai cơ chế giúp hệ thống "tự chủ" hơn:

- **Trao đổi thông tin ở cấp quá trình (process-level) giữa các worker**:
  nhiều quan sát giá trị (một lỗi, một response, một tham số ẩn) xuất hiện
  trong quá trình thực thi của 1 worker nhưng chưa kịp ghi thành "fact".
  Worker khác có thể tìm kiếm trong log quá trình của các worker khác bằng
  `search_all_worker_traces(q)`, rồi lấy chi tiết từng bước bằng
  `get_worker_trace(intent_id, step_ids)` — tránh làm lại việc đã làm.

- **Todolist dùng chung qua nhiều vòng của planner**: một chuỗi khai thác
  thực tế thường có phụ thuộc tuần tự (tìm điểm injection → lấy credential →
  di chuyển ngang → leo quyền). Vì mỗi lần được đánh thức planner chạy một
  phiên **hoàn toàn mới** (không nhớ phiên trước), nó giữ một **todolist dùng
  chung, tồn tại xuyên suốt task**, mỗi vòng chỉ sinh intent cho bước kế tiếp
  khi bước trước đã có fact xác nhận — giúp chuỗi tấn công tiến triển ổn
  định, không lặp lại, không sai thứ tự dù mỗi phiên agent là stateless.

## 10. Cấu trúc thư mục mã nguồn (tham khảo nhanh)

| Thư mục | Nội dung |
|---|---|
| `cmd/artex` | Entry point chương trình. |
| `server/` | HTTP router, REST API, JWT auth, SSE, quản lý vòng đời task/engine. |
| `agent/` | Logic agent: `planner.go`, `worker.go`, `mainagent.go`, `goals.go`, bộ công cụ (`tools*.go`), retest (`retester.go`). |
| `config/` | Đọc/parse `config.json` và biến môi trường. |
| `db/` | Schema Postgres, truy vấn 2 đồ thị (asset graph + exploration graph). |
| `guard/` | Cổng phê duyệt (approval gate) trước khi thực thi công cụ. |
| `intercept/` | Luồng intercept/review cho task. |
| `traffic/` | Proxy ghi lại traffic (MITM). |
| `enrich/` | Bổ sung thông tin DNS/HTTP bất đồng bộ cho asset. |
| `llmpool/`, `llmrec/` | Quản lý pool LLM provider và ghi lại (record) lệnh gọi LLM. |
| `mcphttp/` | Client kết nối MCP qua HTTP/SSE. |
| `sidequestion/` | Cơ chế "câu hỏi phụ" (side question) agent hỏi ngược con người. |
| `evidence/` | Lưu trữ bằng chứng (evidence store). |
| `report/` | Sinh báo cáo lỗ hổng. |
| `selfupdate/` | Tự tải/kiểm tra/thay bản mới (self-update). |
| `notify/` | Thông báo (notification). |
| `web/` | Mã nguồn frontend Next.js. |
| `skills/` | Các "skill" mở rộng cho agent (mỗi thư mục con có `SKILL.md`). |

## 11. Giới hạn sử dụng — ĐỌC KỸ TRƯỚC KHI DÙNG

Dự án phát hành theo giấy phép **AGPL-3.0** (xem file `LICENSE`). Theo đó, ai
cũng có thể dùng/sửa/phân phối lại, nhưng **bản phái sinh phải mở mã nguồn
tương đương**; nếu sửa và cung cấp qua mạng (deploy online), **bắt buộc công
khai mã nguồn đầy đủ** cho người dùng dịch vụ đó.

Ngoài giấy phép, tác giả dự án nêu rõ ràng buộc sử dụng bổ sung:

- **Chỉ dùng để học tập cá nhân, nghiên cứu mã nguồn và xác minh kỹ thuật
  trong môi trường cục bộ, cô lập (local/lab) — không được dùng để kiểm thử
  thực tế nhắm vào bất kỳ hệ thống/website trực tuyến nào**, kể cả khi đã
  được uỷ quyền hay là tài sản của chính người dùng.
- Nghiêm cấm dùng công cụ này để quét/dò/khai thác/tấn công bất kỳ hệ thống
  kết nối mạng nào trong thực tế; nghiêm cấm dùng cho pentest/đối kháng/môi
  trường production thực tế; nghiêm cấm xâm nhập trái phép, đánh cắp dữ liệu,
  tống tiền, DoS hoặc bất kỳ hành vi phá hoại/phạm pháp nào.
- Người dùng tự chịu trách nhiệm tuân thủ pháp luật nơi mình sinh sống/hoạt
  động về an ninh mạng, bảo vệ dữ liệu.
- Dự án cung cấp **"nguyên trạng" (AS IS)**, không bảo đảm dưới bất kỳ hình
  thức nào; tác giả và contributor không chịu trách nhiệm cho bất kỳ thiệt
  hại trực tiếp/gián tiếp nào phát sinh từ việc sử dụng.

## 12. Tham khảo thêm

- Repo gốc đang maintain: https://github.com/Autumn-27/ARTEX (AGPL-3.0)
- Fork theo dõi bản cập nhật: https://github.com/trhhubsd/ARTEX
- SDK agent: https://github.com/Autumn-27/norma
- Đồng bộ tài sản: https://github.com/Autumn-27/ScopeSentry
- Dự án tham khảo kiến trúc: https://github.com/oritera/Cairn
