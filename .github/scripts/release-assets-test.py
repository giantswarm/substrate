#!/usr/bin/env python3

# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Exercise real curl retries, atomic cache writes, and offline tool installs."""
import hashlib
import http.server
import io
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import threading
import unittest

ROOT = Path(__file__).resolve().parents[2]


class ReleaseAssetsTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name)
        self.requests = []
        self.payloads = {}
        self.failures = 0
        owner = self

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                owner.requests.append(self.path)
                if owner.failures:
                    owner.failures -= 1
                    self.send_response(504)
                    self.end_headers()
                    self.wfile.write(b"gateway timeout")
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.write(owner.payloads[self.path])

            def log_message(self, *_args):
                pass

        self.server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)
        self.url = f"http://127.0.0.1:{self.server.server_port}"
        self.env = dict(os.environ)
        self.env.update(RELEASE_ASSET_CACHE=str(self.path / "cache"),
                        GITHUB_PATH=str(self.path / "github-path"),
                        HELM_PLUGINS=str(self.path / "plugins"))

    def run_script(self, name, *args, check=True):
        return subprocess.run([str(ROOT / ".github/scripts" / name), *args],
                              cwd=ROOT, env=self.env, check=check,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def test_504_retry_and_offline_cache(self):
        self.payloads["/asset"] = b"verified release bytes"
        self.failures = 2
        target = self.path / "asset"
        self.run_script("release-asset.sh", self.url + "/asset", str(target))
        self.assertEqual(target.read_bytes(), self.payloads["/asset"])
        self.assertEqual(len(self.requests), 3)
        self.failures = 100
        self.run_script("release-asset.sh", self.url + "/asset", str(target))
        self.assertEqual(len(self.requests), 3)

    def test_failed_download_is_not_cached(self):
        target = self.path / "asset"
        # An unsupported protocol fails immediately, without a long retry wait.
        result = self.run_script("release-asset.sh", "invalid://asset", str(target), check=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(target.exists())
        self.assertEqual(list(self.path.glob("asset.*")), [])

    def test_both_tool_installers_use_warm_cache_and_verify_checksums(self):
        wrappers = self.path / "wrappers"
        wrappers.mkdir()
        real_curl = shutil.which("curl")
        curl = wrappers / "curl"
        curl.write_text("#!/usr/bin/env python3\nimport subprocess, sys\n"
                        f"args = [a.replace('https://github.com', {self.url!r}) for a in sys.argv[1:]]\n"
                        f"sys.exit(subprocess.call([{real_curl!r}] + args))\n")
        curl.chmod(0o755)
        helm = wrappers / "helm"
        helm.write_text('#!/bin/sh\ntest -x "$HELM_PLUGINS/helm-unittest/untt"\n')
        helm.chmod(0o755)
        self.env["PATH"] = str(wrappers) + os.pathsep + self.env["PATH"]
        for tool, version, archive, mode, binary, base in [
            ("weaver", "v0.25.1", "weaver-x86_64-unknown-linux-gnu.tar.xz", "w:xz",
             "weaver-x86_64-unknown-linux-gnu/weaver", "/open-telemetry/weaver/releases/download/v0.25.1/"),
            ("helm-unittest", "1.0.3", "helm-unittest-linux-amd64-1.0.3.tgz", "w:gz",
             "untt", "/helm-unittest/helm-unittest/releases/download/v1.0.3/"),
        ]:
            with self.subTest(tool=tool):
                data = io.BytesIO()
                with tarfile.open(fileobj=data, mode=mode) as tar:
                    payload = b"#!/bin/sh\necho fixture-version\n"
                    info = tarfile.TarInfo(binary)
                    info.mode = 0o755
                    info.size = len(payload)
                    tar.addfile(info, io.BytesIO(payload))
                self.payloads[base + archive] = data.getvalue()
                digest = hashlib.sha256(data.getvalue()).hexdigest()
                checksum = archive + ".sha256" if tool == "weaver" else "helm-unittest-checksum.sha"
                entry = archive if tool == "weaver" else "*./_dist/" + archive
                self.payloads[base + checksum] = f"{digest}  {entry}\n".encode()
                self.failures = 1
                self.run_script("install-release-tool.sh", tool, version)
                requests = len(self.requests)
                self.failures = 100
                self.run_script("install-release-tool.sh", tool, version)
                self.assertEqual(len(self.requests), requests)
                cached = self.path / "cache" / tool / version / "linux-amd64" / archive
                cached.write_bytes(b"corrupt archive")
                self.assertNotEqual(self.run_script("install-release-tool.sh", tool, version,
                                                    check=False).returncode, 0)
                self.assertEqual(len(self.requests), requests)

    def test_setup_retry_backoff_and_exhaustion(self):
        wrappers = self.path / "wrappers"
        wrappers.mkdir()
        sleep = wrappers / "sleep"
        sleeps = self.path / "sleeps"
        sleep.write_text(f'#!/bin/sh\necho "$1" >> "{sleeps}"\n')
        sleep.chmod(0o755)
        self.env["PATH"] = str(wrappers) + os.pathsep + self.env["PATH"]
        command = self.path / "setup"
        count = self.path / "count"
        command.write_text(f"#!/usr/bin/env python3\nfrom pathlib import Path\n"
                           f"p = Path({str(count)!r})\n"
                           "n = int(p.read_text()) + 1 if p.exists() else 1\n"
                           "p.write_text(str(n))\nraise SystemExit(0 if n >= 3 else 1)\n")
        command.chmod(0o755)
        self.run_script("retry.sh", str(command))
        self.assertEqual(count.read_text(), "3")
        self.assertEqual(sleeps.read_text().splitlines(), ["2", "4"])
        sleeps.write_text("")
        result = self.run_script("retry.sh", "sh", "-c", "exit 42", check=False)
        self.assertEqual(result.returncode, 42)
        self.assertEqual(sleeps.read_text().splitlines(), ["2", "4", "8", "16", "32"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
