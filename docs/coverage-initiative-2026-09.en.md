> English · [中文](./coverage-initiative-2026-09.md)

# Coverage Drive: Wrap-Up (2026-09)

For the final state of the task chain and the commit chain, see the [Coverage Task Final Report](./coverage-final-report-2026-09.md.md).

This round's task: bring Herald's whole-repo test coverage up to **line coverage and branch coverage both full**, judged by the test tooling, with no padding through fake coverage; every genuinely unreachable branch or defensive path records its reason site by site; CI gates are raised in step to the measured level.

Final result: **statement coverage 99.5% (go-test basis)**; in phase 5, merging the three `main()` subprocess measured counts into the gate brings it to 99.7%, and the gate reads **"equivalent 100% after excluding ledger-registered blocks"** (`tools/zero_check.py --gate 100`, see the [final report](./coverage-final-report-2026-09.md.md)). The remaining zero blocks are only the `os.Exit` failure branches in the three entry packages' `main()` — exit skips the GOCOVERDIR dump, so this is unmeasurable at the toolchain level; the entry blocks have been **really executed and asserted** through `go build -cover` + GOCOVERDIR subprocess guard tests (see the three phases below); the dozens of defensive branches on the original list were all converted to measured tests, eliminated by refactor, or rewritten to be always-nil, with per-site explanations in [Test Coverage Basis](./architecture/coverage.md).

## Basis and Method

- **Block coverage ⟺ full branch coverage**: the Go coverprofile takes the basic block as its smallest unit; every exit of an `if`/`for`/`select` is an independent block. All block counts non-zero is equivalent to full branch coverage; every zero block must either "have a measurement" or "have a qualitative annotation" — there is no third handling.
- **Zero-block checker**: the script parses the coverprofile, aggregates by `(file, block)` taking the max count across test binaries (the same block is reported once by each package's tests; without aggregation it misjudges), then for every genuine zero block checks whether the 7 lines above it or the block body contains a qualitative annotation — `Defensive` / `Unreachable` / `not callable` / `Coverage note`. Both bases (each package's own tests, and the whole-repo cross via `-coverpkg=./...`) must be GREEN. The tool is in the repo: `tools/zero_check.py`.
- **Probe first**: every "suspected unreachable" gets a temporary probe test written before anything is called defensive; only when it genuinely cannot be triggered is a Defensive annotation left behind, and when the probe does trigger it graduates into a permanent test. This round corrected several wrong qualitative calls that way.
- **Deterministic error injection**: real network errors are hard to reproduce stably, so the means used include `SetLinger(0)`+Close to produce RST, a non-responding server to produce read timeouts, bad data injection (non-JSON, non-block-aligned ciphertext, non-serializable fields), and `jsonMarshal` seam injection of encode failures.

## Six Batches of Progress

| commit | batch | content |
| --- | --- | --- |
| `1d912a8` | batch 1 | api package: callback decryption, JWT, user, and so on |
| `7974e86` | batch 2 | small core packages and the rules state store |
| `1d3958b` | batch 3 | rules engine compile-time and silence-window paths |
| `5e28b62` | lint fix | CI caught `errcheck` (unchecked return values of `mr.Set` and `SetReadDeadline`); from then on golangci-lint runs locally before every batch's commit |
| `b33fcd0` | batch 4 | websocket: real upgrade chain, ACK failure path, seam injection |
| `ef34449` | batch 5 | providers constructor defense, worker-sdk control plane (readLoop/heartbeatLoop/dispatch) |
| `9c611f8` | batch 6 | cmd/heraldd startup failures and full-feature lifecycle, examples, CI gate, basis docs |

Every batch shipped to the same bar: `go test -race -count=1 ./...` all green (46 packages) + gofmt/vet/golangci-lint clean + zero-block checker GREEN before committing, and only main is pushed.

## Three-Phase Sprint (98.7% → 99.5%, remainder at the zero-block floor)

After the six batches everything left in the profile was defensive zero blocks and the three `main()` entries. The three phases ate that up:

| phase | content |
| --- | --- |
| phase 1 (`1f7eab9`) | direct-call measurement of private functions: `compileRule` six field error classes (table-driven), `pkcs7Unpad` illegal input, explicit `GroupInterval` compile path |
| phase 2 | defensive branches to zero: direct-call/seam measurement (`writeControl` transport failure, `evalProgram` stubbed timeout); refactor away dead branches (`Deliver` double lock → single-lock `lookupEnabled`, `Put` front-loaded `compileRule`, `runProgram` single-value assertion); explicit acceptance of always-nil returns (gorilla deadline, Marshal of serializable structs, fallback rate-limit factory, base64 password digest, `aes.NewCipher` on a SHA-256 digest), `x, _ :=` annotated in place with its contract |
| phase 3 | `main()` dual-basis measurement: the three entry `main`s changed to `return` on the success path; each package's `main_cover_test.go` compiles a subprocess with `go build -cover -coverpkg=./...`, runs it for real under `GOCOVERDIR` (graceful SIGTERM exit or natural exit), and asserts that the `main()` range in the dump is fully covered except the `os.Exit` lines |

`os.Exit(n)` skipping the GOCOVERDIR dump is Go toolchain behavior, unmeasurable in principle; every other main success path was measured. This also killed two test fragilities: `runProgram`'s select picked randomly when cancellation and completion were ready together (added a parent ctx pre-check, so the cancellation path now fails deterministically), and `evalTimeout` at 50ms would be pierced by scheduling delay under heavy load (relaxed to 500ms, guard semantics unchanged).

## Key Facts Sedimented

Product facts confirmed during analysis and written into source comments:

- gorilla/websocket (v1.5.3) `SetWriteDeadline` only records the timestamp without touching the network and always returns nil; `SetReadDeadline` delegates to the underlying net.Conn and always succeeds on a live connection.
- `websocket.Server.Start` runs `ListenAndServe` in an internal goroutine; a bind failure is only logged internally, so `Start` always returns nil — the matching branch in serveCmd is unreachable.
- herald's serveCmd exit model waits on SIGINT/SIGTERM signals rather than ctx; component errors only cancel ctx without exiting the process.
- The builtin registry registers only Provider factories, so the `RegisterProvider` duplicate-name guard in `serveCmd`/`workerCmd` is unreachable.
- go-redis v9's `AddHook` immediately calls all three methods of the chain, so a Hook must fully implement `ProcessHook`/`DialHook`/`ProcessPipelineHook`.
- In worker-sdk, `readLoop`/`heartbeatLoop` `wg.Done` pairs with `Run`'s `wg.Add`, so direct-call tests must manually `Add(1)`; `Disconnect`'s `wg.Wait` comes before the state flip.

## Mid-course Correction Log

- **Checker false green**: after a compressed rewrite the checker's path mapping was wrong (import paths concatenated directly onto the repo root, silently skipping every file), which briefly emitted a false "ALL GREEN". After fixing the mapping and introducing block aggregation, 26 unqualified zero blocks surfaced and were verified and handled one by one.
- **Mislabeled branch to measurement**: `compileRule`'s explicit `GroupInterval` compile path was previously thought to be covered (in fact it was only touched incidentally by other packages' integration paths; the in-package basis read zero) — added a table-driven measurement (default value plus explicit value).
- **Test hang triage**: serveCmd waits on signals not ctx (the port-occupied case needs SIGTERM to close); `Disconnect` and `heartbeatLoop` Wait ordering used to deadlock, solved with a separate hbCtx.

## Maintenance Rules

1. New code is tested against "trigger every branch", not "run the function".
2. When a branch is genuinely unreachable, first suspect dead code — delete it if so; if it must be kept, write `// Defensive: ...` in place explaining why.
3. When adding a `main()` or a long-running process entry, add a GOCOVERDIR subprocess guard test in the same change.
4. The coverage gate only moves up with the measured level, with no reserved buffer; CI catches untested code immediately.
5. Assertion-free "pass-through" tests and fabricated call paths are forbidden.
