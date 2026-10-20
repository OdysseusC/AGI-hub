"""Check that public Markdown has both languages and working local file links."""

import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import unquote, urlsplit


def check(root: Path) -> list[str]:
    names = subprocess.check_output(
        ["git", "ls-files", "-co", "--exclude-standard", "-z", "--", "*.md"], cwd=root
    ).decode("utf-8").split("\0")
    documents = sorted({root / name for name in names if name})
    errors = []
    for document in documents:
        name = document.name
        other = name.removesuffix(".zh-CN.md") + ".md" if name.endswith(".zh-CN.md") else name.removesuffix(".md") + ".zh-CN.md"
        partner = document.with_name(other)
        relative = document.relative_to(root)
        if not partner.is_file():
            errors.append(f"{relative}: missing translation {other}")
        text = document.read_text(encoding="utf-8")
        if other not in text or "English" not in text or "中文" not in text:
            errors.append(f"{relative}: missing language navigation")
        text = re.sub(r"(?ms)^```[^\n]*\n.*?^```[^\n]*(?:\n|$)", "", text)
        for match in re.finditer(r"\[[^\]\n]*\]\(([^\n)]+)\)", text):
            target = match.group(1).strip()
            if target.startswith("<"):
                target = target[1:].split(">", 1)[0]
            else:
                target = target.split(' "', 1)[0]
            parsed = urlsplit(target)
            if parsed.scheme or parsed.netloc or not parsed.path:
                continue
            path = unquote(parsed.path)
            if not (document.parent / path).exists():
                errors.append(f"{relative}: missing local link {target}")
    print(f"Checked {len(documents)} public Markdown files ({len(documents) // 2} language pairs).")
    return errors


if __name__ == "__main__":
    problems = check(Path(__file__).resolve().parent.parent)
    for problem in problems:
        print(problem, file=sys.stderr)
    raise SystemExit(bool(problems))
