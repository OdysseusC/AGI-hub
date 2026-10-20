"""Render English and Chinese notes from both exact-version changelog sections."""

import argparse
import re
from pathlib import Path


def release_changes(changelog: str, tag: str) -> str:
    if not re.fullmatch(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", tag):
        raise ValueError("tag must be an exact vX.Y.Z release version")
    version = re.escape(tag[1:])
    exact = re.compile(r"^(?:v?" + version + r"|\[v?" + version + r"\])(?=\s|$)")
    headings = list(re.finditer(r"^##[ \t]+([^\n]+)$", changelog, re.MULTILINE))
    sections = []
    for index, heading in enumerate(headings):
        if exact.match(heading.group(1).strip()):
            end = headings[index + 1].start() if index + 1 < len(headings) else len(changelog)
            sections.append(changelog[heading.end():end].strip())
    if len(sections) != 1:
        raise ValueError(f"expected exactly one changelog section for {tag}; found {len(sections)}")
    body = re.sub(r"<!--[\s\S]*?-->", "", sections[0]).strip()
    if not re.sub(r"(?m)^\s*#+[^\n]*$", "", body).strip():
        raise ValueError(f"changelog section for {tag} is empty")
    return body


def render_notes(changelog: str, tag: str, repo: str, chinese_changelog: str) -> str:
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repo) or any(part in (".", "..") for part in repo.split("/")):
        raise ValueError("repo must be owner/repository")
    base = f"https://github.com/{repo}"
    def pin_links(body):
        def replace(match):
            target = match.group(3)
            if target.startswith(("#", "/", "?")) or re.match(r"[A-Za-z][A-Za-z0-9+.-]*:", target):
                return match.group(0)
            return match.group(1) + (match.group(2) or "") + f"{base}/blob/{tag}/" + target.removeprefix("./")
        return re.sub(r"(\[[^\]\n]*\]\()(<)?([^>\s)]+)", replace, body)
    english = pin_links(release_changes(changelog, tag))
    chinese = pin_links(release_changes(chinese_changelog, tag))
    assets = f"{base}/releases/download/{tag}"
    rows = [
        ("macOS", "Apple silicon (M series)", "Apple M 系列", "macos-arm64"),
        ("macOS", "Intel", "Intel", "macos-amd64"),
        ("Windows", "Intel / AMD x64", "Intel / AMD x64", "windows-amd64"),
        ("Windows", "ARM64 (e.g. Snapdragon)", "ARM64（如 Snapdragon）", "windows-arm64"),
    ]
    def table(chinese=False):
        return "\n".join(f"| {system} | {zh if chinese else en} | [{platform}]({assets}/agi-hub-{platform}.zip) |" for system, en, zh, platform in rows)
    return (
        f"# AGI Hub {tag}\n\n[English](#english) | [简体中文](#简体中文)\n\n"
        f"## English\n\n### Changes\n\n{english}\n\n### Downloads\n\n"
        f"| System | Processor | Package |\n| --- | --- | --- |\n{table()}\n\n"
        f"Checksums: [SHA256SUMS]({assets}/SHA256SUMS).\n\n"
        f"Read the [installation guide]({base}/blob/{tag}/INSTALL.md) and "
        f"[native acceptance record]({base}/blob/{tag}/docs/NATIVE-VALIDATION.md).\n\n"
        "Packages are unsigned; macOS or Windows may display a security prompt. "
        "Successful builds do not verify every native assistant integration.\n\n"
        f"## 简体中文\n\n### 本次更新\n\n{chinese}\n\n### 下载安装包\n\n"
        f"| 系统 | 处理器 | 安装包 |\n| --- | --- | --- |\n{table(True)}\n\n"
        f"校验文件：[SHA256SUMS]({assets}/SHA256SUMS)。\n\n"
        f"安装与接入见[安装说明]({base}/blob/{tag}/INSTALL.zh-CN.md)；"
        f"已验证的原生能力见[本机验收范围]({base}/blob/{tag}/docs/NATIVE-VALIDATION.zh-CN.md)。\n\n"
        "安装包未签名，macOS 和 Windows 可能显示系统安全提示，处理方法见安装说明。"
        "构建通过不代表所有原生助手接入均已验收。\n"
    )


def main(argv=None) -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--repo", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--changelog", default=Path("CHANGELOG.md"), type=Path)
    parser.add_argument("--changelog-zh", default=Path("CHANGELOG.zh-CN.md"), type=Path)
    args = parser.parse_args(argv)
    try:
        notes = render_notes(args.changelog.read_text(encoding="utf-8"), args.tag, args.repo, args.changelog_zh.read_text(encoding="utf-8"))
        args.output.write_text(notes, encoding="utf-8")
    except (OSError, ValueError) as error:
        parser.error(str(error))


if __name__ == "__main__":
    main()
