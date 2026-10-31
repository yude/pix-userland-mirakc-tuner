#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Container checks for unmodified pixd and synthetic sysfs/exec/signal paths."""
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid

IMAGE = os.environ.get("MIRAKC_TEST_IMAGE", "pix-userland-mirakc-tuner:local")
PLATFORM = os.environ.get("MIRAKC_TEST_PLATFORM")


def docker(*args, expected=0):
    result = subprocess.run(["docker", *map(str, args)], capture_output=True, timeout=45)
    if result.returncode != expected:
        raise AssertionError((args, result.returncode, result.stdout.decode(), result.stderr.decode()))
    return result


def main():
    platform = ["--platform", PLATFORM] if PLATFORM else []
    for program in ("pixd", "pixctl", "pix-ts", "pixd-launch"):
        docker("run", "--rm", *platform, "--network", "none", "--entrypoint", program, IMAGE, "--help")
    docker("run", "--rm", *platform, "--network", "none", "--entrypoint", "sh", IMAGE,
           "-ec", "test ! -e /firmware/dt295.bin; test ! -e /src/pix-userland; test ! -e /usr/local/share/pix-userland/dt295.bin")
    with tempfile.TemporaryDirectory(prefix="pixd-launch-smoke-") as work:
        root = Path(work)
        sysfs = root / "sysfs"
        first = sysfs / "1-8.2"
        first.mkdir(parents=True)
        for name, value in {"idVendor": "06b8", "idProduct": "103b", "serial": "SYNTH-A"}.items():
            (first / name).write_text(value + "\n")
        mock = root / "pixd"
        mock.write_text('#!/bin/sh\nprintf "%s\\n" "$@"\nexit 23\n')
        mock.chmod(0o755)
        mounts = ["-v", f"{sysfs}:/sys/bus/usb/devices:ro", "-v", f"{mock}:/usr/local/bin/pixd:ro"]
        result = docker("run", "--rm", *platform, "--network", "none", *mounts,
                        "--entrypoint", "pixd-launch", IMAGE, "--device", "serial:SYNTH-A",
                        "--instance", "dt295", "--runtime-dir", "/run/pixd", "--group", expected=23)
        assert result.stdout.decode().splitlines() == ["--device", "usb:1-8.2", "--instance", "dt295", "--runtime-dir", "/run/pixd", "--group"]
        second = sysfs / "1-8.3"
        second.mkdir()
        for name in ("idVendor", "idProduct", "serial"):
            (second / name).write_bytes((first / name).read_bytes())
        result = docker("run", "--rm", *platform, "--network", "none", *mounts,
                        "--entrypoint", "pixd-launch", IMAGE, "--device", "serial:SYNTH-A", expected=2)
        assert not result.stdout and b"multiple" in result.stderr
        mock.write_text('#!/bin/sh\ntrap "exit 0" TERM INT\necho ready\nwhile :; do sleep 0.05; done\n')
        name = "pixd-launch-signal-" + uuid.uuid4().hex[:12]
        try:
            docker("run", "-d", "--init", *platform, "--network", "none", "--name", name, *mounts,
                   "--entrypoint", "pixd-launch", IMAGE, "--device", "usb:1-8.2")
            # Wait for the signal handler to be installed before stopping.
            for _ in range(100):
                if b"ready" in docker("logs", name).stdout:
                    break
                time.sleep(0.05)
            else:
                raise AssertionError("mock daemon did not become ready")
            docker("stop", "--time", "3", name)
            assert docker("inspect", "--format", "{{.State.ExitCode}}", name).stdout.strip() == b"0"
        finally:
            subprocess.run(["docker", "rm", "-f", name], capture_output=True, timeout=15)
    print("pixd tools, synthetic serial resolution, duplicate rejection, exec status and Docker stop passed")


if __name__ == "__main__":
    main()
