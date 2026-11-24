#!/usr/bin/env python3
"""go_check.py — Go 代码规范工具链聚合检查

按顺序执行三步并汇总结果：
1. gofmt -l            格式检查：列出未按 gofmt 格式化的 .go 文件
2. go vet ./...        静态检查：在每个 go.mod 模块目录下执行（支持一个仓库多个模块）
3. golangci-lint run   可选：未安装时跳过并提示；模块内有 .golangci.yml 则自动使用

用法：
  python3 go_check.py                     # 检查整个仓库
  python3 go_check.py --repo server/      # 只检查某个模块目录
  python3 go_check.py --files a.go b.go   # 只检查指定文件（vet 仍按其所属模块执行）

退出码：0 = 全部通过（golangci-lint 跳过不算失败）；1 = 存在问题。
"""

import argparse
import subprocess
import sys
from pathlib import Path

SKIP_DIRS = {".git", "node_modules", "vendor", ".temp_repos", "frontend", "dist", "build"}


def run(cmd, cwd=None, stdin_data=None):
    """Run a command; missing executables become exit 127 instead of a crash."""
    try:
        p = subprocess.run(
            cmd, cwd=str(cwd) if cwd else None,
            input=stdin_data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        )
        return p.returncode, p.stdout, p.stderr
    except (FileNotFoundError, OSError) as e:
        return 127, "", str(e)


def crlf_only_issue(path: Path) -> bool:
    """True if the file only differs from gofmt output by CRLF line endings."""
    try:
        data = path.read_bytes()
    except OSError:
        return False
    if b"\r\n" not in data:
        return False
    normalized = data.replace(b"\r\n", b"\n").decode("utf-8", errors="replace")
    code, out, _ = run(["gofmt"], stdin_data=normalized)
    return code == 0 and out == normalized


def find_modules(root: Path):
    """Find directories containing go.mod (skipping vendored/generated dirs)."""
    modules = []
    for go_mod in sorted(root.rglob("go.mod")):
        if any(part in SKIP_DIRS for part in go_mod.parts[:-1]):
            continue
        modules.append(go_mod.parent)
    return modules


def gofmt_check(targets, label):
    if not targets:
        return True, "no targets"
    code, out, err = run(["gofmt", "-l"] + [str(t) for t in targets])
    if code != 0:
        print(f"  gofmt 运行失败:\n{out}{err}")
        return False, "gofmt failed"
    unformatted = [l for l in out.splitlines() if l.strip()]
    if not unformatted:
        return True, "formatted"

    real_issues, crlf_files = [], []
    for f in unformatted:
        if crlf_only_issue(Path(f)):
            crlf_files.append(f)
        else:
            real_issues.append(f)

    if real_issues:
        print("  未格式化的文件（运行 gofmt -w 修复）:")
        for f in real_issues:
            print(f"    {f}")

    if crlf_files:
        print("  ⚠ 以下文件仅行尾为 CRLF（内容符合 gofmt），不判为格式错误:")
        for f in crlf_files:
            print(f"    {f}")
        print("  修复建议: 在仓库根目录 .gitattributes 加 `*.go text eol=lf`，"
              "然后 `git add --renormalize .` 提交一次行尾统一。")

    if real_issues:
        return False, f"{len(real_issues)} file(s) not gofmt-formatted"
    return True, "formatted (CRLF-only warnings above)"


def vet_check(module: Path):
    code, out, err = run(["go", "vet", "./..."], cwd=module)
    combined = (out + ("\n" if out and err else "") + err).strip()
    if code != 0:
        print(f"  go vet 失败 ({module}):\n{combined}")
        return False
    if combined:
        print(f"  {combined}")
    return True


def golangci_check(module: Path):
    code, _, _ = run(["golangci-lint", "--version"], cwd=module)
    if code != 0:
        print("  golangci-lint 未安装，跳过（安装: https://golangci-lint.run/welcome/install/）")
        return True  # optional step: skip is not a failure
    code, out, err = run(["golangci-lint", "run"], cwd=module)
    combined = (out + ("\n" if out and err else "") + err).strip()
    if code != 0:
        print(f"  golangci-lint 发现问题 ({module}):\n{combined}")
        return False
    print(f"  golangci-lint 通过 ({module})")
    return True


def main():
    parser = argparse.ArgumentParser(description="Go 规范工具链检查（gofmt / go vet / golangci-lint）")
    parser.add_argument("--repo", help="仓库路径（默认当前目录）")
    parser.add_argument("--files", nargs="+", help="只检查指定 .go 文件")
    parser.add_argument("--skip-golangci", action="store_true", help="跳过 golangci-lint")
    args = parser.parse_args()

    root = Path(args.repo).resolve() if args.repo else Path.cwd()
    if not root.exists():
        print(f"路径不存在: {root}")
        return 1

    modules = find_modules(root)
    if not modules:
        print(f"未找到 go.mod（{root} 不是 Go 仓库）")
        return 1
    print(f"发现 {len(modules)} 个 Go 模块: {', '.join(m.name for m in modules)}")

    failed = []

    # 1) gofmt
    print("\n[1/3] gofmt 格式检查")
    if args.files:
        targets = [Path(f) for f in args.files]
    else:
        targets = modules
    ok, msg = gofmt_check(targets, str(root))
    print(f"  结果: {'通过' if ok else msg}")
    if not ok:
        failed.append("gofmt")

    # 2) go vet — always per module (vet works on packages, not single files)
    print("\n[2/3] go vet 静态检查")
    for m in modules:
        ok = vet_check(m)
        print(f"  结果 ({m.name}): {'通过' if ok else '失败'}")
        if not ok:
            failed.append(f"go vet:{m.name}")

    # 3) golangci-lint (optional)
    if not args.skip_golangci:
        print("\n[3/3] golangci-lint（可选）")
        for m in modules:
            if not golangci_check(m):
                failed.append(f"golangci-lint:{m.name}")
    else:
        print("\n[3/3] golangci-lint 已跳过 (--skip-golangci)")

    print("\n" + "=" * 50)
    if failed:
        print(f"检查未通过: {', '.join(failed)}")
        return 1
    print("All checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
