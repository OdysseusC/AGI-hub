"""Offline checks for release bodies; no GitHub or native model calls."""

import contextlib
import io
import tempfile
import unittest
from pathlib import Path

from release_notes import main, render_notes


class ReleaseNotesTests(unittest.TestCase):
    def render(self, body):
        return render_notes(body, "v0.2.3", "OdysseusC/AGI-hub", "## 0.2.3\n\n- 当前更新\n")

    def test_exact_section_contains_changes_and_excludes_other_versions(self):
        notes = self.render("# History\n\n## 0.2.30 — later\n\n- FUTURE\n\n## 0.2.3 — today\n\n- Real current change\n\n### Details\n\nPreserved detail.\n\n## 0.2.2 — before\n\n- OLD\n")
        self.assertIn("Real current change", notes)
        self.assertIn("Preserved detail.", notes)
        self.assertNotIn("FUTURE", notes)
        self.assertNotIn("OLD", notes)

    def test_missing_empty_and_duplicate_sections_fail_instead_of_placeholder(self):
        for body in (
            "## 0.2.30\n\n- Wrong version\n", "## 0.2.3-rc1\n\n- Prerelease\n",
            "## 0.2.3\n\n", "## 0.2.3\n\n<!-- fill this later -->\n",
            "## 0.2.3\n\n### Added\n", "## 0.2.3\n- First\n## [v0.2.3]\n- Duplicate\n",
        ):
            with self.subTest(body=body), self.assertRaises(ValueError):
                self.render(body)

    def test_chip_mapping_four_assets_checksum_and_docs_are_pinned(self):
        notes = self.render("## [0.2.3] — today\n\n- Change\n")
        rows = [line for line in notes.splitlines() if line.startswith(("| macOS", "| Windows"))]
        self.assertEqual(len(rows), 8)
        self.assertIn("Apple silicon", rows[0])
        self.assertIn("Apple M 系列", rows[4])
        self.assertIn("agi-hub-macos-arm64.zip", rows[0])
        self.assertIn("Intel", rows[1])
        self.assertIn("agi-hub-macos-amd64.zip", rows[1])
        self.assertIn("Intel / AMD", rows[2])
        self.assertIn("agi-hub-windows-amd64.zip", rows[2])
        self.assertIn("ARM64", rows[3])
        self.assertIn("agi-hub-windows-arm64.zip", rows[3])
        self.assertIn("/releases/download/v0.2.3/SHA256SUMS", notes)
        self.assertIn("/blob/v0.2.3/INSTALL.md", notes)
        self.assertIn("/blob/v0.2.3/docs/NATIVE-VALIDATION.md", notes)
        self.assertNotIn("/blob/main/", notes)
        self.assertIn("安装包未签名", notes)

    def test_invalid_tag_or_repository_cannot_inject_links(self):
        for tag, repo in (("v0.2.30\ninvalid", "owner/repo"), ("0.2.3", "owner/repo"), ("v00.2.3", "owner/repo"), ("v0.2.3", "owner/repo](bad)"), ("v0.2.3", "../repo")):
            with self.subTest(tag=tag, repo=repo), self.assertRaises(ValueError):
                render_notes("## 0.2.3\n- Change\n", tag, repo, "## 0.2.3\n- 更新\n")

    def test_missing_chinese_release_section_blocks_publication(self):
        for chinese in ("## 0.2.2\n- 旧版\n", "## 0.2.3\n\n", "## 0.2.3\n- 一\n## 0.2.3\n- 二\n"):
            with self.subTest(chinese=chinese), self.assertRaises(ValueError):
                render_notes("## 0.2.3\n- Change\n", "v0.2.3", "owner/repo", chinese)

    def test_changelog_file_links_are_pinned_in_both_languages(self):
        english = "## 0.2.3\n- [Evidence](docs/NATIVE-VALIDATION.md#claude) and [Guide](./INSTALL.md \"Installation\").\n"
        chinese = "## 0.2.3\n- [验收](docs/NATIVE-VALIDATION.zh-CN.md)和[安装](<./INSTALL.zh-CN.md>)。\n"
        notes = render_notes(english, "v0.2.3", "owner/repo", chinese)
        self.assertIn("[Evidence](https://github.com/owner/repo/blob/v0.2.3/docs/NATIVE-VALIDATION.md#claude)", notes)
        self.assertIn('[Guide](https://github.com/owner/repo/blob/v0.2.3/INSTALL.md "Installation")', notes)
        self.assertIn("[验收](https://github.com/owner/repo/blob/v0.2.3/docs/NATIVE-VALIDATION.zh-CN.md)", notes)
        self.assertIn("[安装](<https://github.com/owner/repo/blob/v0.2.3/INSTALL.zh-CN.md>)", notes)

    def test_external_and_non_file_links_are_preserved(self):
        links = "[Web](https://example.com/a) [Mail](mailto:help@example.com) [Heading](#details) [CDN](//example.com/a) [Absolute](/a) [Query](?tab=readme)"
        notes = self.render("## 0.2.3\n- " + links + "\n")
        self.assertIn(links, notes)

    def test_cli_writes_only_valid_current_version(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            changelog, chinese, output = root / "CHANGELOG.md", root / "CHANGELOG.zh-CN.md", root / "notes.md"
            changelog.write_text("## 0.2.3\n\n- Current change\n", encoding="utf-8")
            chinese.write_text("## 0.2.3\n\n- 当前更新\n", encoding="utf-8")
            args = ["--tag", "v0.2.3", "--repo", "owner/repo", "--changelog", str(changelog), "--changelog-zh", str(chinese), "--output", str(output)]
            main(args)
            self.assertIn("Current change", output.read_text(encoding="utf-8"))
            self.assertIn("当前更新", output.read_text(encoding="utf-8"))
            saved = output.read_bytes()
            changelog.write_text("## 0.2.30\n\n- Wrong version\n", encoding="utf-8")
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                main(args)
            self.assertEqual(output.read_bytes(), saved)


if __name__ == "__main__":
    unittest.main()
