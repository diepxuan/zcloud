# Review: reverse-skill (zhaoxuya520/reverse-skill)

Ngày review: 2026-08-09
Commit được clone: `7427eb0` (2026-08-08)
Đường dẫn: `docs/references/reverse-skill/`
Phạm vi: review tĩnh toàn bộ 45 script `.sh/.ps1/.py/.js/.java/.bat/.cmd`, 2 bootstrap manifest, docs tự audit của repo. Không chạy script nào.

## Tổng quan

Repo này là skill router + tool orchestration cho AI agent, không phải một skill Codex độc lập. Nó muốn AI đọc `README_AI.md` / `RULES.md` rồi tự route task, tự bootstrap tool, tự tạo case scope, tự thực thi, tự viết report/journal.

Code chia rõ thành mấy lớp:

- Routing/ops: `master-route`, `case-init`, `case-guard`, `append-evidence`, `review_case.py`, test scripts.
- Tool discovery: `refresh-tool-index`, `ToolDiscovery.ps1`, `tool-discovery.sh`.
- Bootstrap/install: `bootstrap-reverse.ps1/.sh`, Kali `bootstrap-reverse.sh`, `quick-setup.sh`, manifest JSON.
- Workflow scripts: APK decode/rebuild/Frida, radare2 recon, IDA start/open, browser setup, diagram render.
- Burp MCP: Java extension + Node bridge + build scripts.

## Điểm tốt

- Routing core dùng `routing.json` làm single source of truth, script chỉ đọc config, không hardcode route table.
- Có `case-init` + `case-guard` với khái niệm `auth.status`, `network_profile`, `in_scope`, `ready_for_act`; case-guard mặc định chặn nếu chưa đủ scope.
- Có test chống path traversal CaseName, chống fake auth field ngoài section, chống ghost asset từ `ops_refs`, chống `-Force` bypass auth trong case-init.
- Manifest pin nhiều dependency: jadx `v1.5.6`, apktool `v3.0.2`, frida `14.10.4`, jshook `0.3.4`, reqable `1.0.1`, nuclei `v3.8.0`, pentestswarm `v0.1.0`; một số GitHub asset có SHA-256 hoặc dùng GitHub API digest.
- Generic Linux bootstrap có `safe_remove_install_dir` kiểm tra target không phải `/`, `$HOME`, `$TOOLS_ROOT`, và phải nằm dưới tools root.
- Burp MCP có auth token ngẫu nhiên, ghi file `0600`, bind `127.0.0.1`, không phải public interface.
- Repo có `docs/PACKAGE-SECURITY-AUDIT.md`, tự nhận diện residual risk về `@latest`, npm/pip ecosystem, tag drift.

## Rủi ro / điểm cần lưu ý

### 1. Prompt/execution intent rất mạnh, không nên nạp nguyên bản vào Codex

`README_AI.md`, `README.md`, `RULES.md` và nhiều `SKILL.md` có chỉ thị kiểu:

- AI phải tự chạy script sau khi đọc, không chỉ xác nhận.
- Không đợi user nói "ok continue".
- Missing tool thì tự install.
- Tự register MCP vào `~/.claude/mcp.json` và/hoặc `~/.codex/config.toml`.

Điều này dễ chuyển thành autonomous execution. Nếu dùng, nên tách router/reference khỏi bootstrap/execution, hoặc chỉ đọc routing docs theo từng task và để user xác nhận mỗi action.

### 2. Nhiều script sẽ tự cài tool, tự sửa config host, tự clone/chạy code bên thứ ba

Script đáng chú ý nhất:

- `skills/scripts/bootstrap-reverse.sh` + `.ps1`: có thể `apt-get`, `brew`, `winget`, `pip/pipx`, `npm install -g`, `go install`, clone GitHub, download release, ghi config MCP.
- `kali/scripts/quick-setup.sh`: phải chạy với root, `apt-get upgrade`, cài nhiều tool, ghi `~/.claude/mcp.json`.
- `kali/scripts/bootstrap-reverse.sh`: cài rất nhiều capability, đăng ký MCP.
- `skills/scripts/bootstrap-reverse.sh` `ensure_anything_analyzer`: clone `Mouseww/anything-analyzer` không pin commit, sau đó `pnpm install && nohup pnpm dev`.
- `ensure_ida_pro` Linux không có `startScript` khả thi trong generic script nên chỉ cảnh báo; Kali có `ida-start.sh`.
- `ensure_proxycat` generic: `pipx install git+...` không pin commit (manifest có pin nhưng script không dùng), khác với Kali script có pin.
- `ensure_agent_browser` generic: `npm install -g agent-browser` không pin version trong script, mặc dù manifest pin `0.31.1`.

Nguy cơ thực tế: không phải script có backdoor, mà là nếu repo bị compromised hoặc dependency bị poisoned, agent tự chạy sẽ tự đưa code mới vào máy và cấu hình MCP để sau này tự chạy tiếp.

### 3. Một số script có lệnh xóa không quá an toàn nếu chạy với path do AI/user truyền

Generic Linux bootstrap có guard tốt, nhưng các script workflow khác xóa trực tiếp:

- `skills/apk-reverse/scripts/decode.sh`: `rm -rf "$TASK_ROOT"`, `rm -rf "$JADX_OUT"`, `rm -rf "$APKTOOL_OUT"`; `TASK_NAME` và `OUT_ROOT` đến từ tham số, không guard.
- `decode.ps1`: `Remove-Item -LiteralPath $taskRoot -Recurse -Force` khi `-Clean`.
- `skills/scripts/case-init.ps1/.sh`: xóa temp route dir bằng `Remove-Item -Recurse -Force` / `rm -rf`, path từ temp nhưng không guard.
- `skills/ida-reverse/scripts/open.ps1`: xóa `.id0/.id1/.id2/.nam/.til/.i64` kế bên file phân tích.
- `skills/ida-reverse/scripts/start.ps1`: `taskkill /F /T` các process tên `ida-pro-mcp` / `idalib-mcp`.
- `kali/scripts/ida-start.sh`: `pkill -f "ida-pro-mcp"`.
- `skills/scripts/bootstrap-reverse.ps1`: `Expand-ArchiveIntoDirectory` xóa destination trước khi cài.

Nếu dùng, phải tự validate absolute path, chỉ cho phép trong `work/` hoặc output directory do user chỉ định, không tự nhận path mơ hồ.

### 4. MCP config và token persistence

- `bootstrap-reverse.ps1/.sh` ghi thẳng MCP server vào `~/.claude/mcp.json` và `~/.codex/config.toml`.
- `Ensure-AnythingAnalyzerMcpConfig` tạo `authToken`, ghi vào config app, và PowerShell có thể persist token vào user environment variable `ANYTHING_ANALYZER_MCP_TOKEN`.
- Burp MCP tự tạo token và ghi `~/.burp-mcp-token`; không được đưa token này vào report/log.
- `update-star-history.ps1` đọc token từ `GH_TOKEN`, `GITHUB_TOKEN`, hoặc `git credential fill`; nếu chạy không cẩn thận token có thể xuất hiện trong output/error.

Codex nên tránh để script sửa `~/.codex/config.toml`. MCP server từ repo thứ ba cần user quyết định từng cái, không auto-register.

### 5. Nhiều script tự động download/build/install nhưng integrity chưa đồng đều

Đã pin:

- jadx, apktool, frida, idalib-mcp, reqable, jshook, agent-browser, nuclei, pentestswarm, SecLists, ProxyCat (Kali).

Chưa hoàn toàn pin trong mọi đường dẫn:

- anything-analyzer: clone latest, `pnpm install` theo lockfile của repo ngoài.
- Ghidra: dùng latest release + GitHub API digest (không pin release tag).
- radare2: latest release + GitHub API digest.
- generic `pipx install frida-tools`, `npm install -g agent-browser` trong một số hàm không dùng manifest pin.
- `pnpm` global install.

GitHub API digest cũng là dữ liệu từ GitHub, giảm được tampering thường nhưng không bằng SHA-256 cố định trong repo.

### 6. Không phải tất cả script đều "read-only"

- An toàn: `master-route`, `case-guard`, `review_case.py`, `extract-summaries`, `render_diagram`, `manifest-summary`, phần lớn `refresh-tool-index`.
- Có side effect nhưng trong phạm vi task: `decode`, `rebuild-sign-install`, `frida-run`, `recon`, `append-evidence`.
- Có side effect hệ thống: bootstrap, quick-setup, IDA start, browser setup, update-star-history, Burp MCP.

## Nhóm script

### Routing/ops (khá an toàn, chủ yếu tạo file markdown)

`master-route.sh/.ps1`, `case-init.sh/.ps1`, `case-guard.sh/.ps1`, `append-evidence.ps1`, `review_case.py`, `smoke.ps1`, `test-routing.ps1`, `test-p0-friction.ps1`, `verify-routing-coherence.ps1`, `test-workflow-title-safety.ps1`, `WorkRoot.ps1`.

### Tool discovery (chỉ đọc, trừ việc có thể ghi tool-index)

`refresh-tool-index.sh/.ps1`, `ToolDiscovery.ps1`, `tool-discovery.sh`.

### Bootstrap/install (side effect lớn)

`bootstrap-reverse.ps1`, `bootstrap-reverse.sh`, `kali/scripts/bootstrap-reverse.sh`, `kali/scripts/quick-setup.sh`, `kali/scripts/ida-start.sh`.

### APK

`decode.sh/.ps1`, `rebuild-sign-install.sh/.ps1`, `frida-run.sh/.ps1`, `manifest-summary.ps1`.

### Binary/IDA/radare2

`recon.sh/.ps1`, `ida-reverse/scripts/open.ps1`, `ida-reverse/scripts/start.ps1`.

### Browser/diagram/case

`browser-automation/scripts/setup.ps1`, `diagram-generator/scripts/*.py`, `case-review/scripts/*.py`.

### Burp MCP

`burp-mcp-full/build.sh/.bat`, `gradlew.bat`, `mcp-bridge.js`, `BurpMcpExtension.java`, `McpHttpServer.java`, `mcp-bridge.test.js`.

## Kết luận

Repo này có cấu trúc tốt và nhiều lớp gate, không thấy mã rõ ràng là backdoor hay destructive intent. Nhưng nó được thiết kế để agent tự động cài tool, tự sửa config và tự thực thi, nên không nên dùng như một skill Codex cài sẵn.

Khuyến nghị:

1. Giữ clone ở `docs/references/reverse-skill` làm reference.
2. Không nạp `README_AI.md`, `RULES.md`, hoặc các `SKILL.md` nguyên bản vào context Codex.
3. Nếu cần dùng từng workflow, đọc script/sub-skill cụ thể trước, chạy bằng tay, dùng path rõ ràng.
4. Không chạy `bootstrap-reverse*`, `quick-setup.sh`, hoặc `refresh-tool-index.sh` cho đến khi Sếp muốn cài tool đó; nếu cài thì cho vào thư mục riêng, không để script sửa Codex config.
5. Không tự chạy Burp MCP bridge/extension nếu chưa có Burp Suite cần dùng.
