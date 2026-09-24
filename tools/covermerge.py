#!/usr/bin/env python3
"""covermerge：把 main() 子进程口径的覆盖数据合并进门禁 profile。

Herald 的三个进程入口（cmd/heraldd、examples/quickstart、worker-sdk/go/example）
的 main() 只能由 OS 启动进程调用，`go test` profile 里永远是零块。三个包的
TestMainProcessSuccessPath 用 `go build -cover` 子进程真实运行了 main 成功路径，
并把子进程的 textfmt 转储持久化到 $HERALD_MAIN_COVERDIR。本工具把这些实测
计数合并回主 profile：入口块从「登记豁免」升级为「真实非零」，不是算出来的。

防假绿（防空报告）约定，全部硬失败而不是警告：

1. 每个 --expect 入口（`<import路径>`）必须在合并结果中存在，且文件 basename
   必须是 main.go —— 入口文件按 basename 识别，不按行号硬编码；
2. 每个 child 转储至少含一个非零计数块 —— 空 dump / GOCOVERDIR 没写出文件
   不能被静默跳过；
3. 每个 --expect 入口在 child 转储中至少有一个非零块 —— 子进程真的执行到了
   入口文件的代码；
4. child 转储中出现的块必须存在于主 profile —— 两边工具链/代码不一致时块集
   漂移，宁可失败也不能拼出一份口径不明的 profile。

用法（仓库根目录）：

    export HERALD_MAIN_COVERDIR=build/maincover
    go test -count=1 -coverpkg=./... -coverprofile=coverage.out ./...
    python3 tools/covermerge.py coverage.out build/maincover/*.cov \\
        --expect github.com/cuihairu/herald/cmd/heraldd/main.go \\
        --expect github.com/cuihairu/herald/examples/quickstart/main.go \\
        --expect github.com/cuihairu/herald/worker-sdk/go/example/main.go \\
        -o coverage.merged.out
"""
import argparse
import posixpath
import re
import sys
from pathlib import Path

# profile 行：path:startLine.startCol,endLine.endCol numStmts count
BLOCK = r"^(?P<path>.+?):(?P<sl>\d+)\.(?P<sc>\d+),(?P<el>\d+)\.(?P<ec>\d+) (?P<stmts>\d+) (?P<count>\d+)$"


def key(m):
    return (m["path"], m["sl"], m["sc"], m["el"], m["ec"])


def parse(profile: Path):
    """返回 (mode, {key: (numStmts, count)})；同键重复行取 max count
    （同一块会被多个包的 -coverpkg 测试二进制各报告一次）。"""
    mode = None
    blocks: dict[tuple, tuple[int, int]] = {}
    for line in profile.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        if line.startswith("mode:"):
            mode = line.split(":", 1)[1]
            continue
        m = re.match(BLOCK, line)
        if not m:
            raise SystemExit(f"covermerge: unparseable profile line in {profile}: {line!r}")
        k = key(m)
        stmts, count = int(m["stmts"]), int(m["count"])
        if k in blocks:
            stmts = blocks[k][0]
            if stmts != int(m["stmts"]):
                raise SystemExit(f"covermerge: inconsistent stmts for {k}")
            count = max(count, blocks[k][1])
        blocks[k] = (stmts, count)
    if mode is None:
        raise SystemExit(f"covermerge: {profile} has no mode line")
    return mode, blocks


def main() -> int:
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument("main_profile")
    ap.add_argument("child_profiles", nargs="+")
    ap.add_argument("--expect", action="append", default=[],
                    help="必须被子进程数据覆盖到的入口文件 import 路径（可多次）")
    ap.add_argument("-o", "--out", required=True)
    args = ap.parse_args()

    mode, main_blocks = parse(Path(args.main_profile))
    merged = dict(main_blocks)

    for cp in args.child_profiles:
        cmode, child = parse(Path(cp))
        nonzero = {k for k, (_, c) in child.items() if c > 0}
        # 防假绿 2：空 dump（GOCOVERDIR 没写出数据）不能静默通过。
        if not nonzero:
            raise SystemExit(f"covermerge: {cp} has no non-zero blocks — "
                             "child process data missing or empty; refusing to merge")
        # 防假绿 4：块集漂移说明两边工具链/代码不一致。
        unknown = set(child) - set(merged)
        if unknown:
            sample = sorted(unknown)[0]
            raise SystemExit(f"covermerge: {cp} contains {len(unknown)} block(s) absent "
                             f"from the main profile (e.g. {sample}); toolchain or code "
                             "mismatch — refusing to merge")
        for k, (stmts, count) in child.items():
            _, old = merged[k]
            merged[k] = (stmts, max(old, count))
        print(f"covermerge: merged {cp} (mode {cmode}): {len(child)} blocks, "
              f"{len(nonzero)} non-zero")

    # 防假绿 1 + 3：入口文件按 basename 识别，且必须被子进程数据真实覆盖到。
    for expect in args.expect:
        if posixpath.basename(expect) != "main.go":
            raise SystemExit(f"covermerge: --expect {expect} is not a main.go by basename")
        entry_nonzero = nonzero_union(args.child_profiles, expect)
        if not entry_nonzero:
            raise SystemExit(f"covermerge: entry {expect} has no non-zero block in any "
                             "child dump; the subprocess never reached it")
        print(f"covermerge: entry {expect} covered by child run ({len(entry_nonzero)} blocks)")

    out = Path(args.out)
    with out.open("w", encoding="utf-8") as f:
        f.write(f"mode: {mode}\n")
        for k in sorted(merged):
            path, sl, sc, el, ec = k
            stmts, count = merged[k]
            f.write(f"{path}:{sl}.{sc},{el}.{ec} {stmts} {count}\n")

    gained = sum(1 for k in merged if main_blocks.get(k, (0, 0))[1] == 0 and merged[k][1] > 0)
    print(f"covermerge: wrote {out} — {len(merged)} blocks, {gained} block(s) "
          "rescued from zero by subprocess runs")
    return 0


def nonzero_union(child_paths, expect):
    """所有 child 转储中，属于 expect 入口文件的非零块集合。"""
    result = set()
    for cp in child_paths:
        for line in Path(cp).read_text(encoding="utf-8").splitlines():
            m = re.match(BLOCK, line.strip())
            if not m:
                continue
            if m["path"] == expect and int(m["count"]) > 0:
                result.add(key(m))
    return result


if __name__ == "__main__":
    sys.exit(main())
