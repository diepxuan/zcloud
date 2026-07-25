# Task 00: Setup môi trường + Go project

## Liên kết
- **Master plan:** [master-plan.md](../master-plan.md)
- **Task list:** [tasks.md](../tasks.md)
- **Kế tiếp:** [01-reverse-web-api.md](01-reverse-web-api.md)

## Mục tiêu
Kiểm tra toolchain, tạo cấu trúc thư mục, init Go module.

## Các bước

### 00.1 — Kiểm tra toolchain
- `which go` — nếu chưa có, cài Go 1.22+
- `node --version` — phải ≥ v26.5.0
- `python3 --version` — phải ≥ 3.11
- `java -version` — optional (cho jadx sau)
- `which adb` — optional (cho Android sau)

### 00.2 — Init Go module
- `go mod init github.com/diepxuan/zcloud` tại `src/zcloud/`
- `go mod tidy`

### 00.3 — Tạo thư mục
```
src/zcloud/
├── cmd/zcloudd/
├── internal/core/
├── internal/api/
├── internal/store/
├── web/
└── examples/
```

### 00.4 — Tạo .gitignore
```gitignore
# Go
bin/
*.exe
*.test
*.out
coverage.out

# Node
node_modules/
scripts/re/node_modules/

# OS
.DS_Store
Thumbs.db

# IDE
.idea/
.vscode/
*.swp

# Env
.env
.env.local

# Runtime
*.log
tmp/
```

### 00.5 — Clone references
```bash
cd docs/references
git clone https://github.com/RFS-ADRENO/zca-js.git --depth 1
git clone https://github.com/Amrakk/zcago.git --depth 1
git clone https://github.com/tranhaonguyendev/Za-go.git --depth 1
```

## Output
- `src/zcloud/go.mod`
- `.gitignore` (cập nhật)
- Cấu trúc thư mục hoàn chỉnh
- `docs/references/` có source tham khảo

## Verification
- [ ] `go version` → 1.22+
- [ ] `go build ./...` không lỗi
- [ ] `go test ./...` pass (empty)
- [ ] Cấu trúc thư mục đúng master plan
- [ ] References cloned

## Ghi chú
- Chưa add Go dependencies — sẽ thêm khi implement từng module
- Thư mục `scripts/re/` để ở root project (không trong src/)
- `docs/references/` chứa source clone — thêm vào .gitignore
