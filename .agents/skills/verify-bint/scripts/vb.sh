#!/usr/bin/env bash
# Lái bint thật trong môi trường cô lập (một "lượt kiểm" = một thư mục RUN). Chạy từ gốc repo Bint, qua bash (Git Bash trên Windows).
#
#   bash .bint/skills/verify-bint/scripts/vb.sh launch            # build binary, dựng repo nháp + bint init; in RUN=<thư mục>
#   bash .bint/skills/verify-bint/scripts/vb.sh doctor  <RUN>     # kiểm instance: binary đúng bản, repo nháp đã init
#   bash .bint/skills/verify-bint/scripts/vb.sh run     <RUN> <tên-bằng-chứng> -- <đối số bint…>
#   bash .bint/skills/verify-bint/scripts/vb.sh cleanup <RUN>     # xoá repo nháp và trạng thái, GIỮ <RUN>/evidence
#
# Mỗi `run` ghi <RUN>/evidence/<tên>.txt: lệnh, mã thoát, stdout+stderr. Không đụng trạng thái/cấu hình thật của người dùng.
set -euo pipefail

die() { printf 'vb: %s\n' "$*" >&2; exit 2; }

envs() {
  local run="$1"
  export BINT_STATE_DIR="$run/state" BINT_CONFIG_HOME="$run/config" BINT_ISO_HOME="$run/iso"
  export HOME="$run/home" USERPROFILE="$run/home" GIT_CONFIG_NOSYSTEM=1
  export GIT_AUTHOR_NAME=verify GIT_AUTHOR_EMAIL=verify@example.invalid GIT_COMMITTER_NAME=verify GIT_COMMITTER_EMAIL=verify@example.invalid
  # Bỏ biến của lượt workflow/phiên thật để lượt kiểm không gắn vào tác vụ thật. Biến đánh dấu agent (CLAUDECODE…) giữ
  # nguyên: thao tác chỉ-dành-cho-người vẫn bị chặn như thật, kể cả trong repo nháp.
  unset CLAUDE_CODE_SESSION_ID BINT_RUN_ID BINT_TASK_ID BINT_APPROVED_PATHS BINT_DENY_READ BINT_ISO_PROFILE BINT_ISO_REAL_HOME || true
}

exe() { if [ -f "$1/bin/bint.exe" ]; then echo "$1/bin/bint.exe"; else echo "$1/bin/bint"; fi; }

cmd="${1:-}"; shift || true
case "$cmd" in
launch)
  [ -f go.mod ] && grep -q '^module bint' go.mod || die "chạy từ gốc repo Bint"
  base="${TMPDIR:-${TEMP:-/tmp}}"
  run="$(mktemp -d "${base//\\//}/verify-bint-XXXXXX")"
  mkdir -p "$run"/{bin,state,config,iso,home,evidence,work}
  ext=""; [ "$(go env GOOS)" = windows ] && ext=".exe"
  go build -o "$run/bin/bint$ext" ./cmd/bint
  git rev-parse HEAD > "$run/evidence/source-rev.txt"
  envs "$run"
  ( cd "$run/work"
    git init -q -b main .
    printf 'package demo\n\nfunc Add(a, b int) int { return a + b }\n' > add.go
    printf 'module demo\n\ngo 1.22\n' > go.mod
    git add -A && git commit -qm "seed"
    "$(exe "$run")" init --harness claude,codex --no-hooks > "$run/evidence/launch-init.txt" 2>&1 )
  echo "RUN=$run"
  ;;
doctor)
  run="${1:?thiếu RUN}"; envs "$run"
  [ -x "$(exe "$run")" ] || die "không thấy binary trong $run/bin (chạy launch)"
  [ -f "$run/work/.bint/config.yml" ] || die "repo nháp chưa init: $run/work"
  "$(exe "$run")" --version
  ( cd "$run/work" && "$(exe "$run")" doctor ) | grep -E '✗|FAIL' && die "doctor có mục hỏng" || true
  echo "doctor: ok ($run)"
  ;;
run)
  run="${1:?thiếu RUN}"; name="${2:?thiếu tên bằng chứng}"; shift 2
  [ "${1:-}" = "--" ] && shift
  envs "$run"
  out="$run/evidence/$name.txt"
  set +e
  ( cd "$run/work" && { printf '$ bint'; printf ' %q' "$@"; printf '\n'; } && "$(exe "$run")" "$@" ) > "$out" 2>&1
  code=$?
  set -e
  printf '\n[exit %d]\n' "$code" >> "$out"
  cat "$out"
  ;;
cleanup)
  run="${1:?thiếu RUN}"
  case "$run" in *verify-bint-*) ;; *) die "từ chối xoá thư mục không phải lượt kiểm: $run";; esac
  rm -rf "$run/work" "$run/state" "$run/config" "$run/iso" "$run/home" "$run/bin"
  ls "$run/evidence" >/dev/null || die "mất bằng chứng"
  echo "đã dọn; bằng chứng còn ở $run/evidence"
  ;;
*) die "lệnh: launch | doctor <RUN> | run <RUN> <tên> -- <đối số> | cleanup <RUN>";;
esac
