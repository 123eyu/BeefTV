"""Validate source packets against the actual shipped files. No legal verdicts."""
import argparse
import base64
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
import stat
import tarfile
import zipfile


class SourceError(ValueError):
    pass


def fingerprint(path):
    digest = hashlib.sha256()
    size = 0
    with Path(path).open("rb") as handle:
        while chunk := handle.read(1024 * 1024):
            digest.update(chunk)
            size += len(chunk)
    return size, digest.hexdigest()


def safe_name(name):
    path = PurePosixPath(name)
    if not name or "\\" in name or re.match(r"^[A-Za-z]:", name) or path.is_absolute() or ".." in path.parts or str(path) != name:
        raise SourceError("Unsafe source packet path")
    return name


def verify_embedded_core(executable, component):
    # Go embeds the emitted JS/wasm assets as contiguous bytes. Check those bytes
    # in the final executable instead of trusting a manifest generated before it.
    for key in ("wasm", "javascript"):
        prefix = base64.b64decode(component["binaryPrefixes"][key], validate=True)
        size = component["binarySizes"][key]
        if len(prefix) != 64 or type(size) is not int or not 64 <= size <= len(executable):
            raise SourceError("Invalid embedded core fingerprint")
        offset = 0
        for _ in range(64):
            offset = executable.find(prefix, offset)
            if offset < 0:
                break
            digest = hashlib.sha256(memoryview(executable)[offset:offset + size]).hexdigest()
            if digest == component["binaryHashes"][key]:
                break
            offset += 1
        else:
            raise SourceError("Too many unmatched embedded core candidates")
        if offset < 0:
            raise SourceError(f"Final executable does not contain the declared FFmpeg {key}")


def verify_source(path, component, binary_hashes):
    try:
        return _verify_source(path, component, binary_hashes)
    except (tarfile.TarError, KeyError, TypeError) as error:
        raise SourceError(f"Invalid source packet: {error}") from error


def _verify_source(path, component, binary_hashes):
    with tarfile.open(path, "r:gz") as archive:
        members = {}
        for member in archive.getmembers():
            safe_name(member.name.rstrip("/") if member.isdir() else member.name)
            if member.isdir():
                continue
            safe_name(member.name)
            if not member.isfile() or member.name in members:
                raise SourceError("Source packet contains links or duplicate paths")
            members[member.name] = member

        def read(name, limit=1024 * 1024):
            member = members.get(safe_name(name))
            if member is None or member.size <= 0 or member.size > limit:
                raise SourceError(f"Source packet missing or oversized file: {name}")
            return archive.extractfile(member).read()

        manifest = json.loads(read("manifest.json"))
        for key in ("id", "version", "license"):
            if manifest.get(key) != component.get(key):
                raise SourceError(f"Source packet {key} differs from the distributed component")
        if manifest.get("provenance", {}).get("status") not in ("built", "upstream-attested"):
            raise SourceError(f"{component['id']}: source-to-binary provenance is unresolved")
        if not binary_hashes or manifest.get("binaryHashes") != binary_hashes:
            raise SourceError(f"{component['id']}: source packet does not describe these binaries")
        sources = manifest.get("sources")
        if not isinstance(sources, list) or not sources:
            raise SourceError("Source packet has no source inputs")
        seen = set()
        for source in sources:
            name = safe_name(source["path"])
            if name in seen or name not in members:
                raise SourceError("Source input is missing or duplicated")
            seen.add(name)
            digest = hashlib.sha256()
            handle = archive.extractfile(members[name])
            while chunk := handle.read(1024 * 1024):
                digest.update(chunk)
            if digest.hexdigest() != source.get("sha256"):
                raise SourceError(f"Source checksum mismatch: {name}")
        read(manifest["buildInstructions"])
        licenses = manifest.get("licenses")
        if not isinstance(licenses, list) or not licenses:
            raise SourceError("Source packet has no license texts")
        for name in licenses:
            read(name)
        return manifest


def verify_archive(path, sources_dir, platform, version):
    try:
        return _verify_archive(path, sources_dir, platform, version)
    except (zipfile.BadZipFile, KeyError, TypeError) as error:
        raise SourceError(f"Invalid desktop archive: {error}") from error


def _verify_archive(path, sources_dir, platform, version):
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:[.-][\w.-]+)?", version):
        raise SourceError("Invalid release version")
    root = {"darwin-arm64": "BeefTV.app/Contents/Resources/", "darwin-amd64": "BeefTV.app/Contents/Resources/",
            "windows-amd64": "", "linux-amd64": "BeefTV-linux/"}[platform]
    legal = root + "agent-host/licenses/"
    with zipfile.ZipFile(path) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)):
            raise SourceError("Duplicate paths in final desktop ZIP")
        for member in archive.infolist():
            safe_name(member.filename.rstrip("/") if member.is_dir() else member.filename)
            if stat.S_ISLNK(member.external_attr >> 16):
                raise SourceError("Links are not allowed in final desktop ZIP")

        def read(name, limit=1024 * 1024):
            try:
                info = archive.getinfo(name)
            except KeyError as error:
                raise SourceError(f"Final ZIP missing {name}") from error
            if info.file_size <= 0 or info.file_size > limit:
                raise SourceError(f"Invalid packaged license metadata: {name}")
            return archive.read(name)

        for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "GPL-2.0.txt", "GPL-3.0.txt", "LGPL-2.1.txt", "index.html"):
            read(legal + name)
        index = json.loads(read(legal + "sources.json"))
        if index.get("schema") != 1 or index.get("platform") != platform or index.get("version") != version:
            raise SourceError("Packaged source index does not match this release/platform")
        expected = {"ffmpeg-wasm"}
        if platform.startswith("darwin-"):
            expected.add("ffprobe-" + platform)
        if platform == "windows-amd64":
            expected.add("ffmpeg-windows-amd64")
        components = index.get("components", [])
        if len(components) != len(expected) or {item.get("id") for item in components} != expected:
            raise SourceError("Final package omits a required media component")
        results = []
        for component in components:
            source = component["source"]
            name = f"BeefTV-{version}-{component['id']}-sources.tar.gz"
            if source.get("name") != name or not re.fullmatch(r"[a-f0-9]{64}", str(source.get("sha256"))):
                raise SourceError(f"{component['id']}: matching source packet has not been collected")
            local = Path(sources_dir) / name
            if not local.is_file() or fingerprint(local) != (source.get("size"), source["sha256"]):
                raise SourceError(f"Missing source packet or checksum mismatch: {name}")
            hashes = component.get("binaryHashes", {})
            if component["id"] == "ffmpeg-wasm":
                executable = "BeefTV.app/Contents/MacOS/BeefTV" if platform.startswith("darwin-") else root + ("BeefTV.exe" if platform == "windows-amd64" else "BeefTV")
                verify_embedded_core(read(executable, 512 * 1024 * 1024), component)
            else:
                media = root + "agent-host/media-runtime/"
                read(media + "LICENSE")
                read(media + "README.txt")
                binaries = {"ffprobe": "ffprobe"} if platform.startswith("darwin-") else {"ffmpeg": "ffmpeg.exe", "ffprobe": "ffprobe.exe"}
                hashes = {}
                for key, binary in binaries.items():
                    digest = hashlib.sha256()
                    try:
                        with archive.open(media + binary) as handle:
                            while chunk := handle.read(1024 * 1024):
                                digest.update(chunk)
                    except KeyError as error:
                        raise SourceError(f"Native media binary missing: {binary}") from error
                    hashes[key] = digest.hexdigest()
            verify_source(local, component, hashes)
            results.append(local)
        return results


def release_sources(payload, directory):
    paths = set()
    for platform, asset in payload["platforms"].items():
        archive = Path(directory) / f"BeefTV-{payload['version']}-{platform}.zip"
        if not archive.is_file() or fingerprint(archive) != (asset["size"], asset["sha256"]):
            raise SourceError("Cannot check sources for an unverified desktop ZIP")
        paths.update(verify_archive(archive, directory, platform, payload["version"]))
    return sorted(paths)


def verify_web(directory, version):
    directory = Path(directory)
    legal = directory / "licenses"
    for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "GPL-2.0.txt", "GPL-3.0.txt", "LGPL-2.1.txt", "index.html"):
        if not (legal / name).is_file() or (legal / name).stat().st_size == 0:
            raise SourceError(f"Web distribution missing {name}")
    index = json.loads((legal / "sources.json").read_bytes())
    if index.get("schema") != 1 or index.get("platform") != "web" or index.get("version") != version:
        raise SourceError("Web source index differs from this release")
    components = index.get("components", [])
    if len(components) != 1 or components[0].get("id") != "ffmpeg-wasm":
        raise SourceError("Web distribution must describe its WASM core")
    component = components[0]
    source = component["source"]
    name = f"BeefTV-{version}-ffmpeg-wasm-sources.tar.gz"
    if source.get("name") != name:
        raise SourceError("Web source filename does not match this release")
    packet = legal / "sources" / safe_name(name)
    if fingerprint(packet) != (source.get("size"), source.get("sha256")):
        raise SourceError("Web corresponding source checksum mismatch")
    for key in ("wasm", "javascript"):
        asset = directory / safe_name(component["binaryAssets"][key])
        if fingerprint(asset) != (component["binarySizes"][key], component["binaryHashes"][key]):
            raise SourceError(f"Emitted web {key} does not match its source packet")
    verify_source(packet, component, component["binaryHashes"])
    return [packet]


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    target = parser.add_mutually_exclusive_group(required=True)
    target.add_argument("--archive", type=Path)
    target.add_argument("--web-dir", type=Path)
    parser.add_argument("--sources-dir", type=Path)
    parser.add_argument("--platform")
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    try:
        if args.web_dir:
            paths = verify_web(args.web_dir, args.version)
        else:
            if not args.sources_dir or not args.platform:
                parser.error("--archive requires --sources-dir and --platform")
            paths = verify_archive(args.archive, args.sources_dir, args.platform, args.version)
    except (ValueError, KeyError, OSError, tarfile.TarError, zipfile.BadZipFile) as error:
        parser.exit(1, f"Source delivery check failed: {error}\n")
    print(f"Verified packaged license files and {len(paths)} matching media source packets")
