"""Exercise the real shell installer with local release fixtures, without HTTP."""
from __future__ import annotations

import hashlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


@unittest.skipUnless(sys.platform.startswith("linux"), "requires Linux sh")
class InstallerTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="mip-installer-test-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.installer = self.root / "install.sh"
        shutil.copyfile(ROOT / "install.sh", self.installer)
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.fixtures = self.root / "fixtures"
        self.fixtures.mkdir()
        sums = []
        self.payload = b"#!/bin/sh\necho 'mip-proxy 0.1.1 / sing-box 1.14.4'\n"
        for arch in ("amd64", "arm64"):
            name = f"mip-proxy-linux-{arch}"
            archive = self.fixtures / f"{name}.tar.gz"
            with tarfile.open(archive, "w:gz") as output:
                entry = tarfile.TarInfo(f"{name}/mip-proxy")
                entry.size, entry.mode = len(self.payload), 0o755
                output.addfile(entry, io.BytesIO(self.payload))
            sums.append(f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  {archive.name}\n")
        (self.fixtures / "SHA256SUMS").write_text("".join(sums))
        (self.fixtures / "release.json").write_text(json.dumps({"tag_name": "v0.1.1"}, indent=2))
        self.write_tool("uname", "#!/bin/sh\ncase $1 in -s) echo Linux;; -m) echo \"$MIP_TEST_ARCH\";; esac\n")
        self.write_tool("systemctl", "#!/bin/sh\nexit 0\n")
        self.write_tool("curl", f"#!{sys.executable}\n" + '''import os, pathlib, sys
args = sys.argv[1:]
url = next(value for value in args if value.startswith("https://"))
output = pathlib.Path(args[args.index("-o") + 1])
fixtures = pathlib.Path(os.environ["MIP_TEST_FIXTURES"])
with (fixtures / "requests.log").open("a") as log:
    log.write(url + "\\n")
name = "release.json" if url.endswith("/releases/latest") else url.rsplit("/", 1)[1]
data = (fixtures / name).read_bytes()
if os.environ.get("MIP_TEST_CORRUPT") == "1" and name.endswith(".tar.gz"):
    data += b"tampered"
output.write_bytes(data)
''')
        self.environment = dict(os.environ, PATH=f"{self.tools}:{os.environ['PATH']}", MIP_TEST_FIXTURES=str(self.fixtures), MIP_TEST_ARCH="x86_64")
        self.environment.pop("MIP_VERSION", None)
        self.environment.pop("MIP_REPOSITORY", None)

    def write_tool(self, name, text):
        path = self.tools / name
        path.write_text(text)
        path.chmod(0o755)

    def requests(self):
        path = self.fixtures / "requests.log"
        return path.read_text().splitlines() if path.exists() else []

    def download(self):
        output = self.root / "output"
        result = subprocess.run(["sh", str(self.installer), "--download-only", str(output)], env=self.environment, text=True, capture_output=True)
        return result, output

    def test_latest_detects_amd64_and_downloads_verified_archive(self):
        result, output = self.download()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((output / "mip-proxy-linux-amd64.tar.gz").exists())
        self.assertIn("/releases/latest", self.requests()[0])

    def test_arm64_and_pinned_version_skip_latest_lookup(self):
        self.environment.update(MIP_TEST_ARCH="aarch64", MIP_VERSION="0.1.1")
        result, output = self.download()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((output / "mip-proxy-linux-arm64.tar.gz").exists())
        self.assertTrue(all("/releases/download/v0.1.1/" in url for url in self.requests()))

    def test_corrupt_package_is_rejected(self):
        self.environment["MIP_TEST_CORRUPT"] = "1"
        result, output = self.download()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SHA256", result.stderr)
        self.assertFalse(output.exists())

    def test_invalid_repository_is_rejected_before_download(self):
        self.environment["MIP_REPOSITORY"] = "owner/repo;bad"
        result, _ = self.download()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.requests(), [])

    def isolated_install(self, version):
        if os.geteuid() != 0:
            self.skipTest("full installation test needs root and a private mount namespace")
        installed = self.root / "installed"
        installed.mkdir()
        binary = installed / "mip-proxy"
        binary.write_bytes(self.payload.replace(b"0.1.1", version.encode()))
        binary.chmod(0o755)
        release = self.root / "os-release"
        release.write_text('ID=debian\nVERSION_ID="13"\n')
        wrapper = self.root / "isolated.sh"
        wrapper.write_text('''#!/bin/sh
set -eu
[ "$(readlink /proc/self/ns/mnt)" != "$(readlink /proc/1/ns/mnt)" ] || exit 90
mount --make-rprivate /
mount --bind "$1" /usr/local/bin
mount --bind "$3" /etc/os-release
mount -t tmpfs tmpfs /run
mkdir -p /run/systemd/system
exec sh "$2" --no-menu
''')
        result = subprocess.run(["unshare", "-m", "sh", str(wrapper), str(installed), str(self.installer), str(release)], env=self.environment, text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return binary

    def test_existing_current_version_skips_archive_download(self):
        self.isolated_install("0.1.1")
        self.assertEqual(len(self.requests()), 1)
        self.assertTrue(self.requests()[0].endswith("/releases/latest"))

    def test_outdated_installed_version_is_replaced_after_verification(self):
        binary = self.isolated_install("0.1.0")
        self.assertEqual(binary.read_bytes(), self.payload)
        self.assertEqual(len(self.requests()), 3)


if __name__ == "__main__":
    unittest.main()
