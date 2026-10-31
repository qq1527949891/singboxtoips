#!/usr/bin/env python3
"""Build static Debian release packages. Run on a developer machine with Go."""
from __future__ import annotations

import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile

ROOT = Path(__file__).resolve().parent.parent
DIST = ROOT / "dist"


def run(*args: str, env: dict[str, str] | None = None) -> None:
    subprocess.run(args, cwd=ROOT, env=env, check=True)


def add_file(archive: tarfile.TarFile, name: str, data: bytes, mode: int) -> None:
    info = tarfile.TarInfo(name)
    info.size = len(data)
    info.mode = mode
    info.mtime = 0
    info.uid = info.gid = 0
    info.uname = info.gname = "root"
    archive.addfile(info, io.BytesIO(data))


def main() -> None:
    run("go", "test", "./...")
    run("go", "vet", "./...")
    DIST.mkdir(exist_ok=True)
    checksums: list[str] = []
    go_env = json.loads(subprocess.check_output(("go", "env", "-json", "GOROOT"), cwd=ROOT))
    yaml_module = json.loads(subprocess.check_output(("go", "list", "-m", "-json", "gopkg.in/yaml.v3"), cwd=ROOT))
    notices = {
        "licenses/Go.txt": (Path(go_env["GOROOT"]) / "LICENSE").read_bytes(),
        "licenses/yaml.v3.txt": (Path(yaml_module["Dir"]) / "LICENSE").read_bytes(),
    }
    installer = DIST / "install.sh"
    installer.write_text((ROOT / "install.sh").read_text(encoding="utf-8"), encoding="utf-8", newline="\n")
    checksums.append(f"{hashlib.sha256(installer.read_bytes()).hexdigest()}  {installer.name}\n")
    for arch in ("amd64", "arm64"):
        name = f"mip-proxy-linux-{arch}"
        binary = DIST / name
        env = dict(os.environ, CGO_ENABLED="0", GOOS="linux", GOARCH=arch)
        run("go", "build", "-trimpath", "-ldflags=-s -w -buildid=", "-o", str(binary), "./cmd/mip-proxy", env=env)
        package = DIST / f"{name}.tar.gz"
        # Explicit Unix permissions and LF endings make packages portable from Windows.
        with package.open("wb") as output:
            with gzip.GzipFile(fileobj=output, mode="wb", filename="", mtime=0) as compressed:
                with tarfile.open(fileobj=compressed, mode="w") as archive:
                    add_file(archive, f"{name}/mip-proxy", binary.read_bytes(), 0o755)
                    for source in ("install.sh", "README.md", "LICENSE", "RELEASE_NOTES.txt", "samples/ip-map.csv"):
                        data = (ROOT / source).read_text(encoding="utf-8").replace("\r\n", "\n").encode("utf-8")
                        add_file(archive, f"{name}/{source}", data, 0o755 if source.endswith(".sh") else 0o644)
                    for target, data in notices.items():
                        add_file(archive, f"{name}/{target}", data, 0o644)
        for path in (binary, package):
            checksums.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n")
        print(f"Built {package.name} ({package.stat().st_size:,} bytes)", flush=True)
    source_package = DIST / "mip-proxy-source.tar.gz"
    with source_package.open("wb") as output:
        with gzip.GzipFile(fileobj=output, mode="wb", filename="", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w") as archive:
                sources = [ROOT / name for name in ("go.mod", "go.sum", ".gitignore", "install.sh", "README.md", "LICENSE", "RELEASE_NOTES.txt")]
                for directory in ("cmd", "internal", "integration", "samples", "scripts"):
                    sources.extend(path for path in (ROOT / directory).rglob("*") if path.is_file() and "__pycache__" not in path.parts)
                for path in sorted(sources):
                    relative = path.relative_to(ROOT).as_posix()
                    data = path.read_text(encoding="utf-8").replace("\r\n", "\n").encode("utf-8")
                    add_file(archive, f"mip-proxy/{relative}", data, 0o755 if relative.endswith(".sh") else 0o644)
                workflow_name = ".github/workflows/mip-proxy-release.yml"
                workflow = ROOT / workflow_name
                if not workflow.is_file():
                    workflow = ROOT.parent / workflow_name
                data = workflow.read_text(encoding="utf-8").replace("\r\n", "\n").encode("utf-8")
                add_file(archive, f"mip-proxy/{workflow_name}", data, 0o644)
    checksums.append(f"{hashlib.sha256(source_package.read_bytes()).hexdigest()}  {source_package.name}\n")
    (DIST / "SHA256SUMS").write_text("".join(checksums), encoding="ascii", newline="\n")


if __name__ == "__main__":
    main()
