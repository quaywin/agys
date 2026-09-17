# Kế hoạch Tối ưu hóa Toàn diện agys (Performance & Correctness)

## 1. Tổng quan
Kế hoạch này giải quyết triệt để 7 điểm nghẽn và lỗi tiềm ẩn (bottlenecks & defects) đã được thẩm định trong dự án `agys`, bao gồm:
1. Quét session toàn bộ khi chạy `agys resume` dù có limit, cache bỏ qua session Global.
2. Ghi đè tuần tự 7 file token bằng fsync khi chạy `agys run` dù nội dung không đổi.
3. Statusline kích hoạt durable write fsync trên hot path & chained command chặn 5s.
4. Background model refresh bị kill sớm trong hook process ngắn hạn.
5. TTL 4 giờ của quota cache bị bypass hoàn toàn (lỗi logic so sánh).
6. Timeout mặc định 5s của `WithFileLock` có thể treo vô hạn khi caller truyền `context.Background()`.
7. Concurrency query quota tạo goroutine không giới hạn và quét cả profile rỗng không có token.

---

## 2. Phân chia công việc theo Phase

### Phase 1: Nền tảng an toàn & Sửa lỗi logic (Foundation & Correctness)
* **Task 1: Sửa cơ chế Timeout của `WithFileLock` (Issue 6)**
  * **Files**: `pkg/profile/lock.go`, `pkg/profile/lock_test.go`
  * **Hành động**: Trong `WithFileLock`, nếu `lockCtx` chưa có deadline (`_, ok := lockCtx.Deadline(); !ok`), tự động bọc context với timeout mặc định 5 giây.
  * **Verification**: `go test -v ./pkg/profile -run TestWithFileLock`

* **Task 2: Enforce nghiêm ngặt Quota Cache TTL (Issue 5)**
  * **Files**: `pkg/profile/quota.go`, `pkg/profile/quota_test.go`
  * **Hành động**: Sửa tất cả các caller gọi `GetCachedQuota(profileName, 4*time.Hour)` tại `pkg/profile/quota.go:441, 458, 501, 717` phải kiểm tra boolean `ok`:
    `if stale, ok := GetCachedQuota(profileName, 4*time.Hour); ok && stale != nil`
    Sửa `GetCachedQuota` để khi `maxAge > 0` và cache quá hạn, không trả về data ngầm nếu caller không yêu cầu rõ ràng.
  * **Verification**: `go test -v ./pkg/profile -run TestQuota`

---

### Phase 2: Tối ưu I/O & Hot Path (Startup & Statusline Latency)
* **Task 3: Loại bỏ Durable Write dư thừa khi đồng bộ Token (Issue 2)**
  * **Files**: `pkg/profile/profile.go`, `pkg/profile/profile_test.go`
  * **Hành động**: Trong `WriteTokenToProfile`, kiểm tra file đích trước khi ghi:
    * Nếu file đã tồn tại và nội dung khớp với `trimmed`, bỏ qua không gọi `WriteFileAtomic`.
    * Chỉ ghi các file bị thiếu hoặc có token mới.
  * **Verification**: Kiểm tra số lần ghi đĩa khi gọi `SyncAllTokenLocations` lặp lại.

* **Task 4: Tối ưu Hot Path Statusline & Chained Command (Issue 3)**
  * **Files**: `pkg/profile/statusline.go`, `pkg/profile/statusline_test.go`
  * **Hành động**:
    * Loại bỏ `InputTokens`, `CacheReadTokens`, `CacheCreationTokens` khỏi điều kiện dirty check `isSessionContextStateEqual`.
    * Tạo hàm ghi atomic nhẹ không ép `fsync` (`WriteFileAtomicFast` hoặc không sync) cho `SaveSessionContextForPane`.
    * Giảm timeout của `chainPreviousStatusLine` từ 5s xuống 1.5s để tránh chặn giao diện CLI.
  * **Verification**: `go test -v ./pkg/profile -run TestStatusLine`

---

### Phase 3: Tối ưu Session Scanning, Model Refresh & Concurrency
* **Task 5: Tối ưu hóa toàn diện `agys resume` & Session Cache (Issue 1)**
  * **Files**: `pkg/profile/session.go`, `pkg/profile/session_cache.go`, `pkg/profile/session_test.go`, `pkg/profile/session_cache_test.go`
  * **Hành động**:
    * Cache cả session `(Global)` hoặc rỗng project path (dùng mtime + size để check cache hit).
    * Áp dụng sớm `filter.Limit`: sắp xếp candidates theo file modtime trước, chỉ parse tối đa N candidates cần thiết thay vì scan & parse toàn bộ transcript.
    * Thêm cơ chế Prune Cache khi lưu để dọn dẹp các session đã bị xoá.
    * Dùng `WithFileLock` bảo vệ khi đọc/ghi `session_cache.json`.
  * **Verification**: `go test -v ./pkg/profile -run TestSession`

* **Task 6: Ổn định hóa Model Refresh trong Hook Process (Issue 4)**
  * **Files**: `pkg/profile/models.go`, `pkg/profile/models_test.go`
  * **Hành động**:
    * Nhận diện khi đang chạy trong hook ngắn hạn (như `herdr-hook`, `statusline-hook` hoặc qua cờ môi trường `AGYS_INTERNAL_EXEC` / caller type).
    * Trong hook process ngắn hạn: chỉ đọc cache stale, không bao giờ spawn goroutine `agy models` bị kill giữa chừng.
    * Chỉ refresh trong các lệnh tương tác dài (`agys run`, `agys models`).
  * **Verification**: `go test -v ./pkg/profile -run TestModel`

* **Task 7: Giới hạn Concurrency & Lọc Profile Chưa Cấu Hình (Issue 7)**
  * **Files**: `pkg/profile/auto.go`, `cmd/quota.go`, `cmd/list.go`
  * **Hành động**:
    * Thêm hàm kiểm tra nhanh `HasProfileToken(name)` dựa trên kiểm tra file token tồn tại.
    * Trong `AutoSelectProfile`: lọc bỏ các profile không có token trước khi chạy quota check.
    * Áp dụng Worker Pool / Semaphore (giới hạn 6-8 worker đồng thời) thay vì spawn 1 goroutine cho mỗi profile.
  * **Verification**: `go test -v ./cmd ./pkg/profile -run TestAuto`

---

### Phase 4: Kiểm tra toàn diện & Đo lường
* **Task 8: Kiểm tra Regression & Đánh giá Performance**
  * Chạy toàn bộ test: `go test -v ./...`
  * Kiểm tra linter: `go vet ./...`
  * Đo lường thời gian thực thi của `agys resume --limit 20` và `agys run` so với trước khi tối ưu.
