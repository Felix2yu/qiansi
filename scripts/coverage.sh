#!/usr/bin/env bash
# 生成 Go 测试覆盖率报告：coverage.out（原生）+ coverage.xml（Cobertura）。
# CI 上传给 codecov 的是 coverage.xml —— 只有 Cobertura 里带 <filename>，
# codecov.yml 的 ignore 规则才能生效；Go 原生格式不过滤任何路径。
set -euo pipefail
cd "$(dirname "$0")/.."

TARGET=${1:-./internal/...}
MIN=${COVERAGE_MIN:-80}

go test "$TARGET" -count=1 -covermode=atomic -coverprofile=coverage.out

# 汇总按语句数加权，而不是把各包百分比做算术平均（包大小差十几倍）。
# 本地也守这条线，与 codecov.yml 的 project target 保持一致。
# 行格式：<file.go:start.line,end.line 语句数 命中数>
SUMMARY=$(awk '$1 ~ /\.go:/ { tot += $2; if ($3 + 0 > 0) cov += $2 }
     END {
       if (tot == 0) { print "coverage.out 里没有语句，检查 -coverprofile 是否生效" > "/dev/stderr"; exit 1 }
       printf "%.1f %d %d", 100 * cov / tot, cov, tot
     }' coverage.out)
read -r PCT COV TOT <<< "$SUMMARY"
printf "覆盖率: %s%% (%s/%s 语句)\n" "$PCT" "$COV" "$TOT"
if ! awk -v p="$PCT" -v min="$MIN" 'BEGIN { exit (p + 0.05 >= min ? 0 : 1) }'; then
  echo "✗ 覆盖率低于 ${MIN}%" >&2
  exit 1
fi

# 必须在模块根执行：gocover-cobertura 靠 go.mod 推断 <source> 目录，
# 在别处调用会产出空的 coverage.xml（line-rate=NaN）。
if command -v gocover-cobertura >/dev/null 2>&1; then
  gocover-cobertura < coverage.out > coverage.xml
elif go run github.com/boumenot/gocover-cobertura@latest < coverage.out > coverage.xml; then
  :
else
  rm -f coverage.xml
  echo "! 无法生成 coverage.xml（离线或代理受限），仅保留 coverage.out" >&2
  exit 1
fi
echo "已生成 coverage.xml"
