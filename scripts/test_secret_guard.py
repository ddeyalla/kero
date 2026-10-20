"""Exercise real Gitleaks and Git hooks in disposable repositories."""
import importlib.util
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SOURCE = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("guard", SOURCE / "scripts/secret_guard.py")
guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(guard)


class SecretGuardTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="shibu-secret-test-")
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        for name in ["scripts/secret_guard.py", ".gitleaks.toml", ".githooks/pre-commit", ".githooks/pre-push"]:
            dest = self.repo / name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(SOURCE / name, dest)
        self.run_git("init", "-q")
        self.run_git("config", "user.name", "Guard Test")
        self.run_git("config", "user.email", "guard@example.test")
        self.run_git("config", "core.hooksPath", ".githooks")
        self.run_git("add", ".")
        self.run_git("commit", "-qm", "clean baseline")

    def run_git(self, *args, check=True):
        env = os.environ.copy()
        # Do not inherit a parent's Git directory/index into fixtures.
        for key in list(env):
            if key.startswith("GIT_"):
                env.pop(key)
        return subprocess.run(["git", *args], cwd=self.repo, env=env,
                              capture_output=True, text=True, check=check)

    def key(self):
        return "lin_" + "api_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6"

    def test_blocks_secret_even_if_worktree_is_cleaned_after_staging(self):
        file = self.repo / "source.txt"
        file.write_text(self.key())
        self.run_git("add", "source.txt")
        file.write_text("clean worktree")
        result = self.run_git("commit", "-qm", "must be blocked", check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn(self.key(), result.stdout + result.stderr)

    def test_blocks_private_files_without_a_recognized_key(self):
        (self.repo / "credentials.json").write_text('{"password":"short"}')
        self.run_git("add", "credentials.json")
        self.assertNotEqual(self.run_git("commit", "-qm", "blocked", check=False).returncode, 0)

    def test_clean_source_and_keyboard_shortcut_pass(self):
        (self.repo / "source.go").write_text('Keys: "CmdOrCtrl+Shift+U"\n')
        self.run_git("add", "source.go")
        self.run_git("commit", "-qm", "safe source")

    def test_upstream_url_is_blocked_before_scan(self):
        p = subprocess.run([".githooks/pre-push", "anything", "https://github.com/egoist/kero.git"],
                           cwd=self.repo, input="", capture_output=True, text=True)
        self.assertNotEqual(p.returncode, 0)
        self.assertIn("ddeyalla/kero", p.stderr)

    def test_push_scans_secret_deleted_in_later_commit(self):
        # Create fixture history without hooks, then exercise the actual
        # push hook. No network or real credential is used.
        f = self.repo / "source.txt"
        f.write_text(self.key())
        self.run_git("add", "source.txt")
        self.run_git("-c", "core.hooksPath=/dev/null", "commit", "-qm", "fixture secret")
        self.run_git("rm", "source.txt")
        self.run_git("commit", "-qm", "remove fixture")
        sha = self.run_git("rev-parse", "HEAD").stdout.strip()
        p = subprocess.run([".githooks/pre-push", "origin", "https://github.com/ddeyalla/kero.git"],
                           cwd=self.repo, input=f"refs/heads/test {sha} refs/heads/test {'0'*40}\n",
                           capture_output=True, text=True)
        self.assertNotEqual(p.returncode, 0)
        self.assertNotIn(self.key(), p.stdout + p.stderr)

    def test_private_path_rules(self):
        self.assertTrue(guard.private_path("a/.env.local"))
        self.assertTrue(guard.private_path("update.key"))
        self.assertFalse(guard.private_path(".env.example"))


if __name__ == "__main__":
    unittest.main()
