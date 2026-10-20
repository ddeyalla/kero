#!/usr/bin/env python3
"""Exercise the real MyGo updater in disposable app bundles, never the installed app."""

import base64
import json
import os
from pathlib import Path
import plistlib
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parent.parent
GO = str(ROOT / ".toolchain/go/bin/go") if (ROOT / ".toolchain/go/bin/go").exists() else "go"
PROBE = '''package main
import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "time"
    "github.com/egoist/mygo"
)
func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
    defer cancel()
    up, err := mygo.Updater.Check(ctx)
    if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
    if up == nil { fmt.Fprintln(os.Stderr, "expected a newer Shibu release"); os.Exit(1) }
    if err := up.Install(ctx, nil); err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(2) }
    _ = json.NewEncoder(os.Stdout).Encode(map[string]string{"installed":up.Version})
}
'''


def main():
    config = json.loads((ROOT / "mygo.json").read_text())
    updates = config["updates"].copy()
    updates.pop("changelog", None)
    updates["deltas"] = 0
    env = os.environ.copy()
    # The probe never gets the signing secret or a daily-coding daemon socket.
    for key in ["MYGO_UPDATER_PRIVATE_KEY", "KERO_HOME", "KERO_CONFIG", "KERO_SOCKET", "KERO_PANE", "KERO_SESSION"]:
        env.pop(key, None)
    if (ROOT / ".toolchain/go/bin/go").exists():
        env.update(GOPATH=str(ROOT / ".toolchain/gopath"), GOCACHE=str(ROOT / ".toolchain/gocache"))
    with tempfile.TemporaryDirectory(prefix="shibu-update-test-") as directory:
        temp = Path(directory)
        for invalid in [True, False]:
            project = temp / ("invalid-key" if invalid else "valid-key")
            project.mkdir()
            for name in ["go.mod", "go.sum"]:
                shutil.copyfile(ROOT / name, project / name)
            (project / "main.go").write_text(PROBE)
            feed = updates.copy()
            if invalid:
                feed["publicKey"] = base64.b64encode(bytes(32)).decode()
            probe_config = {"name": "Shibu Update Probe", "identifier": "dev.ddeyalla.shibu-update-probe",
                            "version": "0.0.0", "out": "build", "updates": feed}
            (project / "mygo.json").write_text(json.dumps(probe_config))
            subprocess.run([GO, "tool", "mygo", "build", "-platform", "darwin/arm64", "-sign", "-",
                            "-skip-dmg", "-skip-notarize", str(project)], cwd=ROOT, env=env, check=True)
            app = project / "build/darwin-arm64/Shibu Update Probe.app"
            result = subprocess.run([str(app / "Contents/MacOS/Shibu Update Probe")],
                                    capture_output=True, text=True, env=env, timeout=240)
            info = plistlib.loads((app / "Contents/Info.plist").read_bytes())
            if invalid:
                assert result.returncode == 2 and "signature" in result.stderr.lower(), result.stderr
                assert info["CFBundleShortVersionString"] == "0.0.0"
                print("PASS: wrong signing key rejected; probe bundle was not replaced", flush=True)
            else:
                assert result.returncode == 0, result.stderr
                version = json.loads(result.stdout)["installed"]
                assert info["CFBundleShortVersionString"] == version
                assert info["CFBundleIdentifier"] == "dev.ddeyalla.kero"
                subprocess.run(["codesign", "--verify", "--deep", "--strict", str(app)], check=True)
                print(f"PASS: signed Shibu {version} downloaded and installed into temporary probe", flush=True)
    print("PASS: no GUI, daemon, shell, settings or session state was opened by the probes")


if __name__ == "__main__":
    main()
