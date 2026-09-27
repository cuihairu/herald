module github.com/cuihairu/herald

// go 1.26.6: minimum toolchain fixing all govulncheck-reported stdlib
// vulnerabilities affecting this code (GO-2026-6218/6090/6089/5972/5856/
// 5039/5037/5026/4971/4918; fixed in 1.26.3–1.26.6). Do not lower.
go 1.26.6

require (
	github.com/alicebob/miniredis/v2 v2.39.0
	github.com/expr-lang/expr v1.17.8
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/redis/go-redis/v9 v9.20.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)
