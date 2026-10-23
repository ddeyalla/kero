#!/usr/bin/env python3
"""Prepare a Shibu release in a disposable, clean checkout; never install it."""

import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

UPSTREAM = "https://github.com/egoist/kero.git"
REPOSITORY = "ddeyalla/kero"


def run(*args, check=True):
    return subprocess.run(args, check=check, capture_output=True, text=True)


def stable_version(tag):
    if not re.fullmatch(r"v\d+\.\d+\.\d+", tag):
        raise ValueError(f"Not a stable Kero release tag: {tag!r}")
    return tuple(map(int, tag[1:].split(".")))


def next_version(current, tag):
    match = re.fullmatch(r"(\d+\.\d+\.\d+)-local\.([1-9]\d*)", current)
    if not match:
        raise ValueError(f"Invalid Shibu version: {current!r}")
    old = tuple(map(int, match[1].split(".")))
    new = stable_version(tag)
    if new < old:
        raise ValueError("Refusing an upstream downgrade")
    revision = int(match[2]) + 1 if old == new else 1
    return f"{tag[1:]}-local.{revision}"


def validate_config(config):
    expected = {"name": "Shibu Code", "identifier": "dev.ddeyalla.kero",
                "icon": "resources/shibu-icon.png"}
    for key, value in expected.items():
        if config.get(key) != value:
            raise ValueError(f"Shibu identity changed: {key}")
    updates = config.get("updates", {})
    if (updates.get("github") != REPOSITORY or
            updates.get("tagPrefix") != "shibu-v" or
            updates.get("changelog") != "docs/changelog.md" or
            not updates.get("publicKey") or updates.get("url") or
            updates.get("privateKey")):
        raise ValueError("Unexpected update feed, signing key or macOS identity")
    # Spotify's automation consent text belongs in the bundle. Allow that
    # declaration without permitting a signing or bundle identity override.
    macos = config.get("macos")
    if macos:
        if not isinstance(macos, dict) or set(macos) != {"infoPlist"}:
            raise ValueError("Unexpected macOS identity override")
        info = macos["infoPlist"]
        if (not isinstance(info, dict) or set(info) != {"NSAppleEventsUsageDescription"}
                or not isinstance(info["NSAppleEventsUsageDescription"], str)
                or not info["NSAppleEventsUsageDescription"].strip()):
            raise ValueError("Unexpected macOS permission declaration")


def protocol_version(text):
    match = re.search(r"const Version = (\d+)", text)
    if not match:
        raise ValueError("Cannot determine daemon protocol version")
    return match[1]


def assert_compatible(before, after, old_store, new_store):
    if protocol_version(before) != protocol_version(after):
        raise ValueError("Daemon protocol changed; manual compatibility review required")
    if old_store != new_store:
        raise ValueError("Session persistence changed; manual migration review required")


def output(**values):
    path = os.environ.get("GITHUB_OUTPUT")
    if path:
        with open(path, "a") as stream:
            for key, value in values.items():
                stream.write(f"{key}={value}\n")
    print(json.dumps(values))


def prepare(mode, tag=None):
    if run("git", "status", "--porcelain").stdout:
        raise ValueError("Release preparation requires a clean, disposable checkout")
    config = json.loads(Path("mygo.json").read_text())
    validate_config(config)
    baseline = json.loads(Path("shibu-upstream.json").read_text())
    if mode == "release":
        # A release publishes reviewed Shibu source. Upstream incorporation
        # belongs to the separate check path, not this final delivery step.
        tag = tag or baseline["tag"]
    else:
        release = json.loads(run("gh", "api", "repos/egoist/kero/releases/latest").stdout)
        if release.get("draft") or release.get("prerelease"):
            raise ValueError("Latest upstream release is not stable")
        tag = tag or release["tag_name"]
    stable_version(tag)
    if stable_version(tag) < stable_version(baseline["tag"]):
        raise ValueError("Refusing an upstream downgrade")
    if mode == "check" and tag == baseline["tag"]:
        output(changed="false")
        return

    icon = Path(config["icon"]).read_bytes()
    protocol = Path("internal/proto/proto.go").read_text()
    store = Path("internal/daemon/store.go").read_bytes()
    if tag != baseline["tag"]:
        ref = f"refs/shibu-upstream/{tag}"
        run("git", "fetch", "--no-tags", UPSTREAM, f"refs/tags/{tag}:{ref}")
        commit = run("git", "rev-parse", f"{ref}^{{commit}}").stdout.strip()
        if run("git", "merge-base", "--is-ancestor", baseline["commit"], commit,
               check=False).returncode:
            raise ValueError("Upstream history diverged; manual review required")
        merged = run("git", "merge", "--no-commit", "--no-ff", commit, check=False)
        conflicts = run("git", "diff", "--name-only", "--diff-filter=U").stdout.splitlines()
        # The app's distribution identity is deliberately ours. All other
        # conflicts require review; never use an unconditional ours strategy.
        if set(conflicts) - {"mygo.json"} or (merged.returncode and not conflicts):
            run("git", "merge", "--abort", check=False)
            raise ValueError(f"Upstream merge needs review: {conflicts or merged.stderr}")
        Path("mygo.json").write_text(json.dumps(config, indent=2) + "\n")
        run("git", "add", "mygo.json")
        assert_compatible(protocol, Path("internal/proto/proto.go").read_text(),
                          store, Path("internal/daemon/store.go").read_bytes())
        if Path(config["icon"]).read_bytes() != icon:
            raise ValueError("Upstream changed the Shibu icon; manual review required")
        baseline = {"repository": UPSTREAM, "tag": tag, "commit": commit}

    config["version"] = next_version(config["version"], tag)
    validate_config(config)
    Path("mygo.json").write_text(json.dumps(config, indent=2) + "\n")
    Path("shibu-upstream.json").write_text(json.dumps(baseline, indent=2) + "\n")
    version = config["version"]
    date = datetime.date.today().isoformat()
    notes = (f"## [{version}] - {date}\n\n"
             f"- Includes Kero {tag[1:]} (`{baseline['commit'][:12]}`) and Shibu customizations.\n"
             "- Preserves the Shibu app identity, icon, usage features and separate data folders.\n"
             "- Signed background updates apply on the next launch; no automatic restart.\n\n")
    changelog = Path("docs/changelog.md")
    text = changelog.read_text()
    start = text.find("\n## Unreleased")
    index = text.find("\n## ", start + 1)
    if start < 0 or index < 0:
        raise ValueError("Cannot locate release history in Shibu changelog")
    pending = text[start:index].split("\n", 2)[-1].strip()
    if pending and pending != "No pending changes.":
        notes += pending + "\n\n"
    changelog.write_text(text[:start] + "\n## Unreleased\n\nNo pending changes.\n\n" + notes + text[index + 1:])
    features = Path("docs/features.md")
    if features.exists():
        text = re.sub(r"(?m)^Current release: .*?$",
                      f"Current release: **{version}** (Kero {tag[1:]}), {date}.",
                      features.read_text())
        features.write_text(text.replace("| Unreleased |", f"| {version} |"))
    Path(".release-notes.md").write_text(notes.split("\n", 1)[1].strip() + "\n")
    # The notes file is an untracked build input, never part of the source commit.
    run("git", "add", "-u")
    run("git", "commit", "-m", f"Release Shibu Code {version} with Kero {tag[1:]}")
    output(changed="true", version=version, tag=f"shibu-v{version}",
           commit=run("git", "rev-parse", "HEAD").stdout.strip(),
           icon_sha256=hashlib.sha256(icon).hexdigest())


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["check", "release"])
    parser.add_argument("--upstream-tag", help="explicit stable tag for reviewed retries")
    args = parser.parse_args()
    try:
        prepare(args.mode, args.upstream_tag)
    except (ValueError, subprocess.CalledProcessError) as error:
        raise SystemExit(f"Shibu release paused: {error}") from error
