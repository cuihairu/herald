#!/usr/bin/env python3
"""零块核对器：Herald 覆盖率验收工具。

解析 Go coverprofile，按 (文件, 块) 聚合取各测试二进制报告的 max count
（同一块会被多个包的测试各报告一次，不聚合就会误判），列出真实的零计数块，
并按两种豁免通道分类：
1. 源码就地注释：块起始行上方 7 行加块体内 2 行含定性关键词
   （Defensive / Unreachable / not callable / Coverage note，大小写不敏感，
   跨行注释规范化后匹配）；
2. KNOWN_UNCOVERABLE.md 登记：条目格式 `- <import路径>:<行> — 原因`（路径与行写在反引号内）。

两者皆无的零块为未定性，退出码 1。用法（仓库根目录）：

    go test -count=1 -coverpkg=./... -coverprofile=coverage.out ./...
    python3 tools/zero_check.py coverage.out
"""
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
    if len(sys.argv) != 2:
        print(__doc__)
        return 2
    prof = Path(sys.argv[1])
    root = Path.cwd()

    mod = "github.com/cuihairu/herald"
    gomod = root / "go.mod"
    if gomod.exists():
        for line in gomod.read_text(encoding="utf-8").splitlines():
            if line.startswith("module "):
                mod = line.split()[1]
                break

    blocks: dict[tuple[str, int], int] = collections.defaultdict(int)
    pat = re.compile(r"^(.+?):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)$")
    for line in prof.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("mode:"):
            continue
        m = pat.match(line)
        if not m:
            continue
        key = (m.group(1), int(m.group(2)))
        blocks[key] = max(blocks[key], int(m.group(5)))

    ledger = load_ledger(root)
    zero = sorted(k for k, c in blocks.items() if c == 0)
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
    print("GREEN")
    return 0


if __name__ == "__main__":
    sys.exit(main())
