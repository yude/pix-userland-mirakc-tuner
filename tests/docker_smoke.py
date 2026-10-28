#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Exercise this image's Go adapter through mirakc's HTTP and service APIs."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
from urllib.request import urlopen
import uuid

ROOT = Path(__file__).resolve().parents[1]
HERE = Path(__file__).resolve().parent
IMAGE = os.environ.get("MIRAKC_TEST_IMAGE", "pix-userland-mirakc-tuner:local")


def docker(*args):
    return subprocess.run(["docker", *map(str, args)], capture_output=True, check=True, timeout=45)


def get_json(url):
    with urlopen(url, timeout=3) as response:
        return json.load(response)


def wait_for(predicate, seconds=25):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            result = predicate()
            if result:
                return result
        except (OSError, ValueError):
            pass
        time.sleep(0.1)
    raise AssertionError("timed out waiting for mirakc")


def main():
    name = f"pix-mirakc-smoke-{uuid.uuid4().hex[:12]}"
    with tempfile.TemporaryDirectory(prefix="pix-mirakc-docker-") as work:
        root = Path(work)
        fixture = root / "synthetic.ts"
        subprocess.run([sys.executable, str(HERE / "generate_synthetic_ts.py"), str(fixture)], check=True)
        config = root / "config.yml"
        config.write_text((ROOT / "config.yml").read_text() + "\njobs:\n  sync-clocks:\n    disabled: true\n  update-schedules:\n    disabled: true\n")
        (root / "epg").mkdir()
        try:
            docker("run", "-d", "--rm", "--init", "--name", name,
                   "-p", "127.0.0.1::40772", "-e", "PIX_TS_BIN=/usr/local/bin/mock-pix-ts",
                   "-e", "PIX_MOCK_TS=/tmp/synthetic.ts",
                   "-v", f"{config}:/etc/mirakc/config.yml:ro",
                   "-v", f"{HERE / 'mock_pix_ts.sh'}:/usr/local/bin/mock-pix-ts:ro",
                   "-v", f"{fixture}:/tmp/synthetic.ts:ro",
                   "-v", f"{root / 'epg'}:/var/lib/mirakc/epg", IMAGE)
            port = docker("port", name, "40772").stdout.decode().strip().rsplit(":", 1)[1]
            base = f"http://127.0.0.1:{port}"
            wait_for(lambda: get_json(base + "/api/tuners"))
            services = wait_for(lambda: get_json(base + "/api/services") or None)
            assert any(service["serviceId"] == 101 for service in services), services
            wait_for(lambda: get_json(base + "/api/tuners")[0]["isFree"])
            for decode in (0, 1):
                with urlopen(base + f"/api/channels/GR/T27/stream?decode={decode}", timeout=5) as stream:
                    data = stream.read(188 * 20)
                assert len(data) == 188 * 20
                assert any(all(data[offset + index * 188] == 0x47 for index in range(10))
                           for offset in range(188))
                wait_for(lambda: get_json(base + "/api/tuners")[0]["isFree"])
            print("mirakc HTTP stream, service scan, disconnect, and tuner reuse passed")
        finally:
            subprocess.run(["docker", "rm", "-f", name], capture_output=True, timeout=15)


if __name__ == "__main__":
    main()
