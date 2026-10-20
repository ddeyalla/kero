#!/usr/bin/env python3
"""Fail closed before commits/pushes; never print matching secret text."""
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import sys


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def private_path(name):
    p = PurePosixPath(name)
    return (p.name in {".env", "credentials.json", "usage-credentials.json", "auth.json"}
            or (p.name.startswith(".env.") and p.name != ".env.example")
            or p.suffix in {".key", ".pem", ".p12", ".pfx"}
            or name.startswith("plans/research/probes/out/"))


def check_paths(names):
    bad = [n for n in names if private_path(n)]
    if bad:
        raise SystemExit("BLOCKED: private files must stay outside Git: " + ", ".join(bad))


def scan(mode, revision=None):
    scanner = shutil.which("gitleaks")
    if scanner is None:
        raise SystemExit("BLOCKED: install Gitleaks first: brew install gitleaks")
    env = os.environ.copy()
    for key in ("GITLEAKS_CONFIG", "GITLEAKS_CONFIG_TOML"):
        env.pop(key, None)
    args = [scanner, "git", "--config", str(Path(__file__).resolve().parent.parent / ".gitleaks.toml"),
            "--redact", "--no-banner", "--ignore-gitleaks-allow", "--gitleaks-ignore-path", os.devnull]
    if mode == "staged":
        check_paths(git("diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z").split("\0"))
        args += ["--staged", "--pre-commit"]
    else:
        # Entire reachable history catches a key introduced and deleted in
        # separate outgoing commits, including on newly created branches.
        check_paths(git("ls-tree", "-r", "--name-only", revision, "-z").split("\0"))
        args += ["--log-opts=" + revision]
    result = subprocess.run(args, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        # Scanner output is intentionally withheld: even failure diagnostics
        # must not echo a credential into a terminal or agent transcript.
        raise SystemExit("BLOCKED: secret scan found a credential or could not complete. "
                         "Review locally with gitleaks git --redact; do not bypass the hook.")
    print("Secret guard passed.")


def main():
    os.chdir(git("rev-parse", "--show-toplevel"))
    if len(sys.argv) == 2 and sys.argv[1] == "staged":
        scan("staged")
    elif len(sys.argv) == 2 and sys.argv[1] == "history":
        scan("history", "HEAD")
    elif len(sys.argv) == 4 and sys.argv[1] == "push":
        # Exact destination allowlist, independent of the remote's name.
        destination = sys.argv[3].removesuffix(".git")
        if destination not in {"https://github.com/ddeyalla/kero", "git@github.com:ddeyalla/kero",
                               "ssh://git@github.com/ddeyalla/kero"}:
            raise SystemExit("BLOCKED: Shibu pushes may only target ddeyalla/kero.")
        for line in sys.stdin:
            fields = line.split()
            if len(fields) != 4:
                raise SystemExit("BLOCKED: malformed Git push update")
            revision = fields[1]
            if set(revision) != {"0"}:
                scan("history", revision)
    else:
        raise SystemExit("usage: secret_guard.py staged|history|push <remote> <url>")


if __name__ == "__main__":
    main()
