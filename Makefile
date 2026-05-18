.PHONY: build clean run docs docs-dev test docker-build docker-up docker-down docker-logs dashboard-dev dashboard-build

build:
	@echo "Building herald..."
	@mkdir -p bin
	@go build -o bin/heraldd ./cmd/heraldd

clean:
	@echo "Cleaning..."
	@rm -rf bin

run: build
	@echo "Running herald..."
	@./bin/heraldd --config config.yaml

docs-dev:
	@echo "Starting docs dev server..."
	@cd docs && pnpm dev

docs-build:
	@echo "Building docs..."
	@cd docs && pnpm build

test:
	@echo "Running tests..."
	@go test ./...

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
