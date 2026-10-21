# Deployment

## Run Modes

Herald uses a single binary, `heraldd`, with subcommands distinguishing the run mode:

```bash
heraldd serve    # Scheduler mode (API + Queue + local workers)
heraldd worker   # Remote worker mode (consumes from the shared Queue)
```

## Single-Node Deployment

Single-node mode uses the `memory` queue; all workers run inside the same process.

```bash
# Build
make build

# Start
./bin/heraldd serve --config config.yaml
```

Example configuration:

```yaml
server:
  addr: ":8080"
  timeout: 30s

queue:
  type: memory
  workers: 0    # Automatic: CPU cores * 2 + 1

providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
```

## Distributed Deployment

Distributed mode uses the `redis` queue; the scheduler and workers can be deployed independently.

### Scheduler Node

```bash
./bin/heraldd serve --config scheduler.yaml
```

Example configuration (scheduler.yaml):

```yaml
server:
  addr: ":8080"
  timeout: 30s

queue:
  type: redis
  workers: 4
  redis:
    addr: "redis:6379"
    stream: "herald:tasks"
    group: "herald-workers"

websocket:
  addr: ":8081"

providers:
  telegram:
    type: telegram
    enabled: true
    config:
      token: "$TELEGRAM_BOT_TOKEN"
      chat_id: "$TELEGRAM_CHAT_ID"
```

### Worker Node

```bash
./bin/heraldd worker --config worker.yaml
```

Example configuration (worker.yaml):

```yaml
queue:
  type: redis
  workers: 0
  redis:
    addr: "redis:6379"
    stream: "herald:tasks"
    group: "herald-workers"

worker:
  id: "wechat-worker-01"
  server_url: "ws://scheduler:8081/worker"   # When omitted, auto-assembled from websocket.addr

providers:
  wechatmp:
    type: wechatmp
    enabled: true
    config:
      app_id: "$WECHAT_APP_ID"
      app_secret: "$WECHAT_APP_SECRET"
```

## Docker Deployment

### Using Docker Compose

1. Copy the environment variable template:

```bash
cp .env.example .env
```

2. Edit the `.env` file and fill in your configuration.

3. Start the services:

```bash
docker-compose up -d
```

### Building the Docker Image Manually

```bash
docker build -t herald:latest .
```

Scheduler:

```bash
docker run -d \
  -p 8080:8080 -p 8081:8081 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -e TELEGRAM_BOT_TOKEN=your_token \
  --name herald \
  herald:latest serve --config config.yaml
```

Remote worker:

```bash
docker run -d \
  -v $(pwd)/worker.yaml:/app/worker.yaml:ro \
  --name herald-worker \
  herald:latest worker --config worker.yaml
```

## Binary Deployment

### Build

```bash
make build
```

### Run the Scheduler

```bash
./bin/heraldd serve --config config.yaml
```

### Run a Remote Worker

```bash
./bin/heraldd worker --config worker.yaml
```

## System Services (systemd)

### Scheduler Service

Create `/etc/systemd/system/herald.service`:

```ini
[Unit]
Description=Herald Notification Scheduler
After=network.target

[Service]
Type=simple
User=herald
WorkingDirectory=/opt/herald
ExecStart=/opt/herald/bin/heraldd serve --config /etc/herald/config.yaml
Restart=always
RestartSec=5

EnvironmentFile=/etc/herald/herald.conf

[Install]
WantedBy=multi-user.target
```

### Worker Service

Create `/etc/systemd/system/herald-worker.service`:

```ini
[Unit]
Description=Herald Remote Worker
After=network.target

[Service]
Type=simple
User=herald
WorkingDirectory=/opt/herald
ExecStart=/opt/herald/bin/heraldd worker --config /etc/herald/worker.yaml
Restart=always
RestartSec=5

EnvironmentFile=/etc/herald/worker.conf

[Install]
WantedBy=multi-user.target
```

Start the services:

```bash
sudo systemctl daemon-reload
sudo systemctl enable herald herald-worker
sudo systemctl start herald herald-worker
```

## Health Check

```bash
curl http://localhost:8080/api/v1/status
```

Example response:

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "running",
    "providers": [
      {
        "name": "telegram",
        "type": "telegram",
        "status": "available"
      }
    ]
  }
}
```
