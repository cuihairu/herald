#!/usr/bin/env python3
"""零块核对器：Herald 覆盖率验收工具。

解析 Go coverprofile，按 (文件, 块) 聚合取各测试二进制报告的 max count
（同一块会被多个包的测试各报告一次，不聚合就会误判），列出真实的零计数块，
并按源码就地注释分类：带定性关键词的零块视为"已记录原因"，其余列为
未定性（必须补测试或补注释）。

用法（仓库根目录）：

    go test -count=1 -coverpkg=./... -coverprofile=coverage.out ./...
    python3 tools/zero_check.py coverage.out

退出码：存在未定性零块时为 1，否则 0。

定性关键词（与 docs/architecture/coverage.md 约定一致）：
Defensive / Unreachable / not callable / Coverage note（大小写不敏感）。

判定窗口为块起始行上方 7 行加块体内 2 行，跨行注释会被规范化后再匹配，
防止关键词被断行躲过检查。
"""
import collections
import re
import sys
from pathlib import Path

KEYS = ("defensive", "unreachable", "not callable", "coverage note")


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

    zero = sorted(k for k, c in blocks.items() if c == 0)
    unknown = []
    for fn, sln in zero:
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

    print(f"blocks: {len(blocks)}, zero blocks: {len(zero)} (annotated: {len(zero) - len(unknown)})")
    if unknown:
        print("--- UNANNOTATED zero blocks ---")
        for fn, sln, note in unknown:
            print(f"{fn}:{sln}  [{note}]")
        return 1
    print("GREEN")
    return 0


if __name__ == "__main__":
    sys.exit(main())
