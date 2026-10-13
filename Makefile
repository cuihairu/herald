.PHONY: build clean run docs-dev docs-build test test-integration docker-build docker-up docker-down docker-logs docker-ps dashboard-dev dashboard-build

build:
	@echo "Building herald..."
	@mkdir -p bin
	@go build -o bin/heraldd ./cmd/heraldd

clean:
	@echo "Cleaning..."
	@rm -rf bin

run: build
	@echo "Running herald..."
	@./bin/heraldd serve --config config.yaml

docs-dev:
	@echo "Starting docs dev server..."
	@cd docs && pnpm dev

docs-build:
	@echo "Building docs..."
	@cd docs && pnpm build

# ================= 本地测试口径 (local testing scope) =================
# CI 与本地开发按改动面三路分流——改一行文档不该全量编译：
#   1) 文档改动（*.md、docs/**、图片）→ 不编译。主 CI workflow 的 paths-ignore
#      已跳过这些路径；本地无需全量跑。
#   2) 代码改动 → 只跑改动子集：
#      - `go test ./<改动包>/...`
#      - `go test ./internal/<改动包>/...`
#   3) 全量（make test）→ 仅以下情形触发：
#      go.mod / go.sum、.github/**、Docker/Compose 配置、发布前。
# ==========================================================================

test:
	@echo "Running tests..."
	@go test ./...

# 需要本地 Redis（默认 localhost:6379，可用 TEST_REDIS_ADDR 覆盖）
test-integration:
	@echo "Running integration tests..."
	@go test -count=1 -tags=integration ./core/queue

# Dashboard commands
dashboard-dev:
	@echo "Starting dashboard dev server..."
	@cd dashboard && pnpm dev

dashboard-build:
	@echo "Building dashboard..."
	@cd dashboard && pnpm build

# Docker commands
docker-build:
	@echo "Building Docker image..."
	@docker build -t herald:latest .

docker-up:
	@echo "Starting Herald with Docker..."
	@docker-compose up -d

docker-down:
	@echo "Stopping Herald..."
	@docker-compose down

docker-logs:
	@docker-compose logs -f herald

docker-ps:
	@docker-compose ps
