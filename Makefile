# qiansi —— 目标约定见 Felix2yu/.github/docs/makefile.md
#
# 七个必备目标：build / dev / test / lint / fmt / docker / clean
# 其余目标按仓库自身需要添加。
.PHONY: build dev test lint lint-strict fmt docker clean

BIN  := bin/qiansi
MAIN := ./cmd/server

## 产出 $(BIN)。前端产物同时摆进根 dist/（Dockerfile 的 COPY 源）与 embed 位置
build:
	cd web && pnpm install --frozen-lockfile && pnpm run build
	rm -rf dist
	mkdir -p dist
	cp -R web/dist/. dist/
	mkdir -p bin
	go build -trimpath -ldflags "-s -w" -o $(BIN) $(MAIN)

## 前端热更新另开终端：cd web && pnpm run dev
dev:
	go run $(MAIN)

## 集成测试里有自举进程的用例，-count=1 避免缓存掩盖
test:
	go test ./... -count=1

lint:
	go vet ./...
	cd web && pnpm run check

lint-strict: lint
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; else echo "  （未安装 golangci-lint，跳过）"; fi

fmt:
	gofmt -w $$(git ls-files '*.go')

docker: build
	docker build -t qiansi:dev .

clean:
	rm -rf bin dist web/dist
