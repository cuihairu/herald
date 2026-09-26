#!/usr/bin/env python3
"""零块核对器：Herald 覆盖率验收工具。

解析 Go coverprofile，按 (文件, 块) 聚合取各测试二进制报告的 max count
（同一块会被多个包的测试各报告一次，不聚合就会误判），列出真实的零计数块，
并按两种豁免通道分类：
1. 源码就地注释：块起始行上方 7 行加块体内 2 行含定性关键词
   （Defensive / Unreachable / not callable / Coverage note，大小写不敏感，
   跨行注释规范化后匹配）；
2. KNOWN_UNCOVERABLE.md 登记：条目格式 `- <import路径>:<行> — 原因`（路径与行写在反引号内）。

两者皆无的零块为未定性，退出码 1。

--gate N：在 GREEN 之上追加门禁——「排除 KNOWN_UNCOVERABLE 登记块后的等效
语句覆盖率 ≥ N」。等效覆盖率 = (总语句 − 全部零块语句) / (总语句 − 登记零块
语句)，即登记块从分子分母同时剔除（与台账「排除登记项后 100%」的声明同一
口径）；就地注释块不剔除，想维持 --gate 100 必须补测或进台账。用法（仓库根目录）：

    go test -count=1 -coverpkg=./... -coverprofile=coverage.out ./...
    python3 tools/zero_check.py coverage.out --gate 100
"""
import argparse
import collections
import re
import sys
from pathlib import Path

KEYS = ("defensive", "unreachable", "not callable", "coverage note")
LEDGER = "KNOWN_UNCOVERABLE.md"


def load_ledger(root: Path) -> set[tuple[str, int]]:
    """Parse `- `import/path.go:LINE` — reason` entries into a set."""
    entries = set()
    path = root / LEDGER
    if not path.exists():
        return entries
    pat = re.compile(r"^\s*-\s+`([^`:]+):(\d+)`")
    for line in path.read_text(encoding="utf-8").splitlines():
        m = pat.match(line)
        if m:
            entries.add((m.group(1), int(m.group(2))))
    return entries


def main() -> int:
    ap = argparse.ArgumentParser(add_help=False)
    ap.add_argument("profile")
    ap.add_argument("--gate", type=float, default=None, metavar="N",
                    help="等效语句覆盖率门禁（排除台账登记块后），如 --gate 100")
    args = ap.parse_args()
    prof = Path(args.profile)
    root = Path.cwd()

    mod = "github.com/cuihairu/herald"
    gomod = root / "go.mod"
    if gomod.exists():
        for line in gomod.read_text(encoding="utf-8").splitlines():
            if line.startswith("module "):
                mod = line.split()[1]
                break

    # (文件, 起.列, 止.列) -> [max count, numStmts]。key 必须是完整块区间：
    # 单行 if 的条件块与分支体块起始行相同（`x.go:71.8,71.48` 条件与
    # `x.go:71.48,74.3` 分支体），只按起始行聚合取 max，恒被走过的条件块
    # 会把同起始行的零块掩盖成非零，把真缺口藏出门禁。
    # 与 tools/covermerge.py 同口径。
    blocks: dict[tuple[str, str, str, str, str], list[int]] = collections.defaultdict(lambda: [0, 0])
    pat = re.compile(r"^(.+?):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$")
    for line in prof.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("mode:"):
            continue
        m = pat.match(line)
        if not m:
            continue
        key = (m.group(1), m.group(2), m.group(3), m.group(4), m.group(5))
        blocks[key][0] = max(blocks[key][0], int(m.group(7)))
        blocks[key][1] = max(blocks[key][1], int(m.group(6)))

    ledger = load_ledger(root)
    # 台账与就地注释窗口仍按 (文件, 起始行) 匹配；共享起始行的零块去重即可。
    zero = sorted({(k[0], int(k[1])) for k, (c, _) in blocks.items() if c == 0})
    unknown, exempted = [], []
    for fn, sln in zero:
        if (fn, sln) in ledger:
            exempted.append((fn, sln))
            continue
        rel = fn[len(mod) + 1:] if fn.startswith(mod + "/") else fn
        try:
            src = (root / rel).read_text(encoding="utf-8").splitlines()
        except OSError:
            unknown.append((fn, sln, "SRC NOT FOUND"))
            continue
        lo = max(0, sln - 8)
        hi = min(len(src), sln + 2)
        window = "\n".join(src[lo:hi]).lower()
        window = re.sub(r"\n\s*//\s*", " ", window)
        if not any(key in window for key in KEYS):
            unknown.append((fn, sln, src[sln - 1].strip() if sln - 1 < len(src) else "?"))

    print(f"blocks: {len(blocks)}, zero blocks: {len(zero)} "
          f"(annotated: {len(zero) - len(exempted) - len(unknown)}, ledger-exempted: {len(exempted)})")

    if unknown:
        print("--- UNANNOTATED zero blocks (cover with a test, annotate in place, or register in "
              f"{LEDGER}) ---")
        for fn, sln, note in unknown:
            print(f"{fn}:{sln}  [{note}]")
        return 1

    if args.gate is not None:
        total = sum(s for _, s in blocks.values())
        zero_stmts = sum(blocks[k][1] for k in zero)
        excluded = sum(blocks[k][1] for k in exempted)
        denominator = total - excluded
        equivalent = 100.0 * (denominator - (zero_stmts - excluded)) / denominator if denominator else 100.0
        print(f"equivalent stmt coverage (ledger blocks excluded): "
              f"{equivalent:.2f}%  (gate: >= {args.gate:g}%)")
        # 浮点安全：99.99999 与 100 的比较误差收敛到 0.005 个百分点内。
        if equivalent + 0.005 < args.gate:
            print("--- GATE FAILED: equivalent coverage below gate. Test the gap, or register "
                  f"justified blocks in {LEDGER} (in-place annotation alone does not count "
                  "toward the gate) ---")
            for fn, sln in exempted:
                print(f"excluded: {fn}:{sln}")
            return 1

    print("GREEN")
    return 0


if __name__ == "__main__":
    sys.exit(main())
