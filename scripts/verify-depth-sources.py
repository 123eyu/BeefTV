#!/usr/bin/env python3
"""Bind both FFmpeg distributions in final depth ZIPs to reviewed source packets."""

import argparse
from contextlib import contextmanager
import hashlib
import importlib.util
import json
from pathlib import Path, PurePosixPath
import re
import shutil
import stat
import tempfile
from urllib.parse import urlsplit
import zipfile

from distribution_sources import SourceError, fingerprint, safe_name, verify_source

ROOT = Path(__file__).resolve().parent.parent
COMPONENTS = ("ffmpeg-windows-amd64", "imageio-ffmpeg-windows-amd64")
IMAGEIO_BINARIES = ".python/Lib/site-packages/imageio_ffmpeg/binaries/"
# Verified against the official imageio_ffmpeg-0.6.0-py3-none-win_amd64.whl.
# PyPI wheel SHA256: 02fa47c83703c37df6bfe4896aab339013f62bf02c5ebf2dce6da56af04ffc0a.
IMAGEIO_LICENSE = ".python/Lib/site-packages/imageio_ffmpeg-0.6.0.dist-info/LICENSE"
spec = importlib.util.spec_from_file_location("prepare_depth_manifest", Path(__file__).with_name("prepare-depth-manifest.py"))
manifest_builder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(manifest_builder)


@contextmanager
def runtime_archive(root, variant, artifact):
    name = f"beeftv-depth-runtime-v1-windows-amd64-{variant}.zip"
    if not artifact.get("parts"):
        yield root / name
        return
    with tempfile.TemporaryDirectory(prefix="beeftv-depth-sources-") as temporary:
        archive = Path(temporary) / name
        with archive.open("wb") as output:
            for part in artifact["parts"]:
                # prepare() already validated exact sequential part names and bytes.
                with (root / PurePosixPath(urlsplit(part["urls"][0]).path).name).open("rb") as source:
                    shutil.copyfileobj(source, output, 1024 * 1024)
        if fingerprint(archive) != (artifact["size"], artifact["sha256"]):
            raise SourceError("Reassembled depth runtime differs from verified parts")
        yield archive


def archive_hashes(path, artifact):
    with zipfile.ZipFile(path) as archive:
        entries = archive.infolist()
        names = {}
        for item in entries:
            safe_name(item.filename.rstrip("/") if item.is_dir() else item.filename)
            name = item.filename.casefold()
            if name in names or stat.S_ISLNK(item.external_attr >> 16):
                raise SourceError("Depth ZIP contains duplicate paths or links")
            names[name] = item
        if len(entries) != artifact["files"] or sum(item.file_size for item in entries) != artifact["expandedSize"]:
            raise SourceError("Depth ZIP contents differ from runtime metadata")

        def read(name):
            item = archive.getinfo(name)
            if item.is_dir() or not 0 < item.file_size <= 1024 * 1024:
                raise SourceError(f"Depth runtime missing or invalid notice: {name}")
            return archive.read(item)

        def digest(name):
            item = archive.getinfo(name)
            if item.is_dir() or item.file_size <= 0:
                raise SourceError(f"Depth runtime missing binary: {name}")
            with archive.open(item) as binary:
                return hashlib.file_digest(binary, "sha256").hexdigest()

        executables = {item.filename for item in entries if not item.is_dir()
                       and PurePosixPath(item.filename).name.lower().endswith(".exe")
                       and re.search(r"ffmpeg|ffprobe", PurePosixPath(item.filename).name, re.IGNORECASE)}
        imageio = [name for name in executables if name.startswith(IMAGEIO_BINARIES)
                   and PurePosixPath(name).name.lower().startswith("ffmpeg")]
        if len(imageio) != 1 or executables != {"bin/ffmpeg.exe", "bin/ffprobe.exe", *imageio}:
            raise SourceError("Depth runtime has missing or undeclared FFmpeg executables")
        read("licenses/ffmpeg/LICENSE")
        read("licenses/ffmpeg/README.txt")
        read(IMAGEIO_LICENSE)
        main = {"ffmpeg": digest("bin/ffmpeg.exe"), "ffprobe": digest("bin/ffprobe.exe")}
        manifest = json.loads(read("licenses/ffmpeg/manifest.json"))
        if manifest.get("binarySHA256") != main["ffmpeg"] or manifest.get("probeSHA256") != main["ffprobe"]:
            raise SourceError("Depth FFmpeg notice manifest does not match its binaries")
        return {COMPONENTS[0]: main, COMPONENTS[1]: {"ffmpeg": digest(imageio[0])}}


def verify(root, version, lock=ROOT / "licenses/distribution-inputs.json"):
    try:
        root = Path(root)
        # Keep the existing signing payload's size/hash/part protocol authoritative.
        payload = manifest_builder.prepare(root)
        if not re.fullmatch(r"v\d+\.\d+\.\d+(?:[.-][\w.-]+)?", version):
            raise SourceError("Invalid release version")
        document = json.loads(Path(lock).read_bytes())
        if document.get("schema") != 1:
            raise SourceError("Unsupported distribution input schema")
        components = {item["id"]: item for item in document["components"]}
        packets = {}
        for identifier in COMPONENTS:
            component = components[identifier]
            source = component.get("source")
            if not source:
                raise SourceError(f"{identifier}: {component.get('blocker', 'No reviewed source packet')}")
            packet = root / f"BeefTV-{version}-{identifier}-sources.tar.gz"
            if fingerprint(packet) != (source["size"], source["sha256"]):
                raise SourceError(f"Depth source packet checksum mismatch: {identifier}")
            verify_source(packet, component, component["binaryHashes"])
            packets[identifier] = packet
        if components[COMPONENTS[1]]["version"] != "0.6.0":
            raise SourceError("Imageio wheel license inventory requires review for this version")
        for variant in ("cpu", "cuda"):
            artifact = payload["runtimes"][f"windows-amd64/{variant}"]
            with runtime_archive(root, variant, artifact) as archive:
                for identifier, hashes in archive_hashes(archive, artifact).items():
                    component = components[identifier]
                    if hashes != component["binaryHashes"]:
                        raise SourceError(f"{variant}: {identifier} binaries differ from reviewed source inputs")
                    verify_source(packets[identifier], component, hashes)
        return list(packets.values())
    except (OSError, ValueError, KeyError, TypeError, zipfile.BadZipFile) as error:
        if isinstance(error, SourceError):
            raise
        raise SourceError(f"Depth source verification failed: {error}") from error


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    try:
        packets = verify(args.directory, args.version)
    except SourceError as error:
        parser.exit(1, f"Depth source delivery check failed: {error}\n")
    print(f"Verified CPU/CUDA runtime FFmpeg binaries and {len(packets)} corresponding source packets")
