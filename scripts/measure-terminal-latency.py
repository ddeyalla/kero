"""Compare synchronized-redraw transport latency in two isolated Kero daemons.

No GUI input, user sessions, or user configuration is touched. This measures
PTY/input-to-mirror-frame delivery, not keyboard-to-display presentation.
"""

import argparse
import json
import os
from pathlib import Path
import socket
import statistics
import struct
import subprocess
import sys
import tempfile
import time


PROGRAM = """
import os, time, tty
tty.setraw(0)
os.write(1,b'\\x1b[?1049h\\x1b[HREADY')
index=0
while os.read(0,1):
    index+=1
    os.write(1, ('\\x1b[?2026h\\x1b[HFRAME-%04d' % index).encode())
    time.sleep(0.010)
    os.write(1,b'\\x1b[?2026l')
"""


def measure(executable: str, count: int) -> dict:
    executable = str(Path(executable).resolve(strict=True))
    with tempfile.TemporaryDirectory(prefix="kero-latency-") as directory:
        home = Path(directory)
        config = home / "config"
        config.write_text("command = /bin/sh\n")
        env = os.environ.copy()
        for key in ("KERO_SOCKET", "KERO_SESSION", "KERO_PANE"):
            env.pop(key, None)
        env.update(KERO_HOME=directory, KERO_CONFIG=str(config))

        def cli(*args: str, check: bool = True) -> str:
            return subprocess.run(
                [executable, *args], env=env, capture_output=True,
                text=True, timeout=15, check=check,
            ).stdout.strip()

        connection = None
        session = None
        try:
            session = cli("new", "-n", "isolated-latency-probe", "-d", directory,
                          "--", sys.executable, "-u", "-c", PROGRAM)
            connection = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            connection.settimeout(10)
            connection.connect(str(home / "kerod.sock"))
            connection.sendall(json.dumps({"kind": "attach", "version": 2,
                                           "session": session}).encode() + b"\n")
            stream = connection.makefile("rb")
            welcome = json.loads(stream.readline())
            if welcome.get("error"):
                raise RuntimeError(welcome["error"])

            def frame() -> tuple[bytes, bytes]:
                header = stream.read(5)
                if len(header) != 5:
                    raise RuntimeError("daemon closed the probe stream")
                length = struct.unpack(">I", header[1:])[0]
                if length > 16 << 20:
                    raise RuntimeError("invalid frame length")
                payload = stream.read(length)
                if len(payload) != length:
                    raise RuntimeError("incomplete frame")
                return header[:1], payload

            while True:
                kind, payload = frame()
                if kind == b"o" and b"READY" in payload:
                    break
            timings = []
            for _ in range(count):
                started = time.perf_counter()
                connection.sendall(b"i" + struct.pack(">I", 1) + b"x")
                while frame()[0] != b"o":
                    pass
                timings.append((time.perf_counter() - started) * 1000)
            stream.close()
            ordered = sorted(timings)
            return {"samples": count, "median_ms": round(statistics.median(timings), 2),
                    "p95_ms": round(ordered[min(count - 1, int(count * .95))], 2),
                    "min_ms": round(min(timings), 2), "max_ms": round(max(timings), 2)}
        finally:
            if connection is not None:
                connection.close()
            if session:
                cli("close", session, check=False)
            cli("daemon", "stop", check=False)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("before")
    parser.add_argument("after")
    parser.add_argument("--samples", type=int, default=15)
    args = parser.parse_args()
    if not 1 <= args.samples <= 100:
        parser.error("samples must be between 1 and 100")
    print(json.dumps({"before": measure(args.before, args.samples),
                      "after": measure(args.after, args.samples)}, indent=2))
