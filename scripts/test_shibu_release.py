import copy
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("release", Path(__file__).with_name("shibu_release.py"))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def test_version_same_base_increments(self):
        self.assertEqual(release.next_version("2.0.5-local.9", "v2.0.5"), "2.0.5-local.10")

    def test_version_new_base_resets(self):
        self.assertEqual(release.next_version("2.0.5-local.9", "v2.1.0"), "2.1.0-local.1")

    def test_bad_tags_and_downgrades_fail(self):
        for tag in ["v2.0.4", "v2.0.6-beta.1", "main", "--upload-pack=x"]:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                release.next_version("2.0.5-local.5", tag)

    def test_identity_and_feed_guard(self):
        config = {"name": "Shibu Code", "identifier": "dev.ddeyalla.kero",
                  "icon": "resources/shibu-icon.png", "updates": {
                      "github": "ddeyalla/kero", "tagPrefix": "shibu-v",
                      "changelog": "docs/changelog.md", "publicKey": "test"}}
        release.validate_config(config)
        for key, value in [("name", "Kero"), ("identifier", "sh.kero"),
                           ("icon", "resources/icon.png"), ("macos", {"signingIdentity": "upstream"})]:
            changed = copy.deepcopy(config)
            changed[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                release.validate_config(changed)
        for key, value in [("github", "egoist/kero"), ("url", "https://example.com"),
                           ("tagPrefix", "v"), ("privateKey", "secret")]:
            changed = copy.deepcopy(config)
            changed["updates"][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                release.validate_config(changed)

    def test_spotify_permission_description_preserves_identity_guard(self):
        config = json.loads((Path(__file__).resolve().parents[1] / "mygo.json").read_text())
        release.validate_config(config)
        for macos in [
            {"signingIdentity": "upstream"},
            {"infoPlist": {"CFBundleIdentifier": "sh.kero"}},
            {"infoPlist": {"NSAppleEventsUsageDescription": ""}},
            {"infoPlist": {"NSAppleEventsUsageDescription": 1}},
        ]:
            changed = copy.deepcopy(config)
            changed["macos"] = macos
            with self.subTest(macos=macos), self.assertRaises(ValueError):
                release.validate_config(changed)

    def test_protocol_and_persistence_changes_pause(self):
        release.assert_compatible("const Version = 2", "const Version = 2", b"state", b"state")
        with self.assertRaises(ValueError):
            release.assert_compatible("const Version = 2", "const Version = 3", b"state", b"state")
        with self.assertRaises(ValueError):
            release.assert_compatible("const Version = 2", "const Version = 2", b"old", b"new")


class MergeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.cwd = Path.cwd()
        self.addCleanup(os.chdir, self.cwd)
        os.chdir(self.temp.name)
        self.git("init", "-q")
        self.git("config", "user.name", "Test")
        self.git("config", "user.email", "test@example.com")
        Path("internal/proto").mkdir(parents=True)
        Path("internal/daemon").mkdir(parents=True)
        Path("resources").mkdir()
        Path("docs").mkdir()
        Path("internal/proto/proto.go").write_text("const Version = 2\n")
        Path("internal/daemon/store.go").write_text("saved schema\n")
        Path("feature.txt").write_text("base\n")
        Path("mygo.json").write_text('{"name":"Kero","version":"2.0.5"}\n')
        self.commit("base")
        self.base = self.git("rev-parse", "HEAD").stdout.strip()
        self.git("branch", "upstream-test")
        config = {"name": "Shibu Code", "identifier": "dev.ddeyalla.kero",
                  "icon": "resources/shibu-icon.png", "version": "2.0.5-local.5",
                  "updates": {"github": "ddeyalla/kero", "tagPrefix": "shibu-v",
                              "changelog": "docs/changelog.md", "publicKey": "test"}}
        Path("mygo.json").write_text(json.dumps(config) + "\n")
        Path("resources/shibu-icon.png").write_bytes(b"personal-icon")
        Path("docs/changelog.md").write_text("# Changelog\n\n## Unreleased\n\nNone\n\n## 2.0.5-local.5\n\nOld\n")
        Path("docs/features.md").write_text("Current release: **2.0.5-local.5** (Kero 2.0.5), 2026-10-20.\n\n| Example | Menu | Unreleased | Untried |\n")
        Path("shibu-upstream.json").write_text(json.dumps({"tag": "v2.0.5", "commit": self.base}))
        Path("feature.txt").write_text("personal feature\n")
        self.commit("Shibu")
        self.branch = self.git("branch", "--show-current").stdout.strip()

    def git(self, *args):
        return subprocess.run(["git", *args], check=True, capture_output=True, text=True)

    def commit(self, title):
        self.git("add", ".")
        self.git("commit", "-qm", title)

    def upstream(self, change=None):
        self.git("checkout", "-q", "upstream-test")
        Path("mygo.json").write_text('{"name":"Kero","version":"2.0.6"}\n')
        Path("upstream-feature.txt").write_text("new upstream feature\n")
        if change:
            Path(change).write_text("conflicting upstream change\n")
        self.commit("upstream")
        self.git("tag", "v2.0.6")
        self.git("checkout", "-q", self.branch)

    def prepare(self, mode="check", tag="v2.0.6"):
        real_run = release.run

        def command(*args, **kwargs):
            if args[:2] == ("gh", "api"):
                return subprocess.CompletedProcess(args, 0, json.dumps({"tag_name": tag}))
            return real_run(*args, **kwargs)

        with mock.patch.object(release, "UPSTREAM", self.temp.name), \
                mock.patch.object(release, "run", command), contextlib.redirect_stdout(io.StringIO()):
            release.prepare(mode)

    def test_clean_merge_preserves_customizations_and_advances_base(self):
        self.upstream()
        self.prepare()
        config = json.loads(Path("mygo.json").read_text())
        self.assertEqual(config["version"], "2.0.6-local.1")
        release.validate_config(config)
        self.assertEqual(Path("feature.txt").read_text(), "personal feature\n")
        self.assertEqual(Path("resources/shibu-icon.png").read_bytes(), b"personal-icon")
        self.assertTrue(Path("upstream-feature.txt").exists())

    def test_conflict_pauses_without_advancing_source(self):
        self.upstream("feature.txt")
        before = self.git("rev-parse", "HEAD").stdout
        with self.assertRaisesRegex(ValueError, "merge needs review"):
            self.prepare()
        self.assertEqual(self.git("rev-parse", "HEAD").stdout, before)
        self.assertEqual(Path("feature.txt").read_text(), "personal feature\n")

    def test_persistence_change_requires_review(self):
        self.upstream("internal/daemon/store.go")
        with self.assertRaisesRegex(ValueError, "persistence changed"):
            self.prepare()

    def test_same_upstream_is_a_noop(self):
        before = self.git("rev-parse", "HEAD").stdout
        self.prepare(tag="v2.0.5")
        self.assertEqual(self.git("rev-parse", "HEAD").stdout, before)

    def test_release_uses_reviewed_base_when_upstream_is_newer(self):
        self.upstream()
        self.prepare(mode="release")
        config = json.loads(Path("mygo.json").read_text())
        baseline = json.loads(Path("shibu-upstream.json").read_text())
        self.assertEqual(config["version"], "2.0.5-local.6")
        self.assertEqual(baseline["tag"], "v2.0.5")
        self.assertEqual(baseline["commit"], self.base)
        self.assertFalse(Path("upstream-feature.txt").exists())
        features = Path("docs/features.md").read_text()
        self.assertIn("Current release: **2.0.5-local.6** (Kero 2.0.5)", features)
        self.assertIn("| Example | Menu | 2.0.5-local.6 | Untried |", features)


if __name__ == "__main__":
    unittest.main()
