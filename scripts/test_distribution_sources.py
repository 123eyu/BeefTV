import base64
import gzip
import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
import zipfile

from distribution_sources import SourceError, fingerprint, release_sources, verify_archive, verify_source, verify_web


def write_source_packet(path, entries, extra_members=()):
    # Shared wasm sources must have identical bytes in all four desktop indexes.
    with Path(path).open("wb") as output, gzip.GzipFile(fileobj=output, mode="wb", filename="", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w") as archive:
            for name, data in entries.items():
                item = tarfile.TarInfo(name)
                item.size = len(data)
                archive.addfile(item, io.BytesIO(data))
            for item in extra_members:
                archive.addfile(item)


def source_fixture(path, component, hashes, status="built"):
    files = {"source.tar": b"source archive fixture", "BUILD.txt": b"build with the recorded toolchain", "COPYING": b"license fixture"}
    manifest = {"schema": 1, **{key: component[key] for key in ("id", "version", "license")},
                "binaryHashes": hashes, "provenance": {"status": status},
                "sources": [{"path": "source.tar", "sha256": hashlib.sha256(files["source.tar"]).hexdigest()}],
                "buildInstructions": "BUILD.txt", "licenses": ["COPYING"]}
    files["manifest.json"] = json.dumps(manifest).encode()
    write_source_packet(path, files)


def desktop_fixture(directory, platform="linux-amd64", version="v1.7.28", status="built"):
    directory = Path(directory)
    wasm, javascript = b"\0asm" + b"wasm-fixture-" * 20, b"var createFFmpegCore = " + b"js-fixture-" * 20
    component = {"id": "ffmpeg-wasm", "version": "0.12.10", "license": "GPL-2.0-or-later",
                 "binaryHashes": {key: hashlib.sha256(data).hexdigest() for key, data in [("wasm", wasm), ("javascript", javascript)]},
                 "binarySizes": {"wasm": len(wasm), "javascript": len(javascript)},
                 "binaryPrefixes": {"wasm": base64.b64encode(wasm[:64]).decode(), "javascript": base64.b64encode(javascript[:64]).decode()}}
    components = [component]
    binaries = {}
    root = "BeefTV-linux/" if platform == "linux-amd64" else "BeefTV.app/Contents/Resources/" if platform.startswith("darwin-") else ""
    executable = "BeefTV.app/Contents/MacOS/BeefTV" if platform.startswith("darwin-") else root + ("BeefTV.exe" if platform == "windows-amd64" else "BeefTV")
    entries = {executable: b"executable fixture" + wasm + javascript}
    if platform != "linux-amd64":
        binaries = {"ffprobe": b"native ffprobe fixture"}
        if platform == "windows-amd64":
            binaries["ffmpeg"] = b"native ffmpeg fixture"
        native = {"id": "ffprobe-" + platform if platform.startswith("darwin-") else "ffmpeg-windows-amd64",
                  "version": "8.0.1", "license": "LGPL-2.1-or-later" if platform.startswith("darwin-") else "GPL-3.0-or-later",
                  "binaryHashes": {key: hashlib.sha256(data).hexdigest() for key, data in binaries.items()}}
        components.append(native)
        media = root + "agent-host/media-runtime/"
        for key, data in binaries.items():
            entries[media + key + (".exe" if platform == "windows-amd64" else "")] = data
        entries.update({media + "LICENSE": b"GNU license fixture", media + "README.txt": b"build info fixture"})
    for item in components:
        name = f"BeefTV-{version}-{item['id']}-sources.tar.gz"
        source_fixture(directory / name, item, item["binaryHashes"], status)
        size, sha = fingerprint(directory / name)
        item["source"] = {"name": name, "size": size, "sha256": sha}
    legal = root + "agent-host/licenses/"
    for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "GPL-2.0.txt", "GPL-3.0.txt", "LGPL-2.1.txt", "index.html"):
        entries[legal + name] = b"preserved notice fixture"
    entries[legal + "sources.json"] = json.dumps({"schema": 1, "version": version, "platform": platform, "components": components}).encode()
    archive = directory / f"BeefTV-{version}-{platform}.zip"
    write_zip(archive, entries)
    return archive, entries, components


def write_zip(path, entries):
    with zipfile.ZipFile(path, "w") as archive:
        for name, data in entries.items():
            archive.writestr(name, data)


def web_fixture(directory, version="v1.7.28"):
    directory = Path(directory)
    legal, static = directory / "licenses", directory / "static"
    (legal / "sources").mkdir(parents=True)
    static.mkdir()
    binaries = {"wasm": b"\0asm" + b"web-wasm-fixture-" * 20,
                "javascript": b"var createFFmpegCore = " + b"web-js-fixture-" * 20}
    component = {"id": "ffmpeg-wasm", "version": "0.12.10", "license": "GPL-2.0-or-later",
                 "binaryHashes": {key: hashlib.sha256(data).hexdigest() for key, data in binaries.items()},
                 "binarySizes": {key: len(data) for key, data in binaries.items()}}
    assets = {}
    for key, suffix in (("wasm", "wasm"), ("javascript", "js")):
        assets[key] = static / f"ffmpeg-core-{component['binaryHashes'][key][:8]}.{suffix}"
        assets[key].write_bytes(binaries[key])
    component["binaryAssets"] = {key: asset.relative_to(directory).as_posix() for key, asset in assets.items()}
    # Vite also emits URL-export chunks with the same prefix as the raw core.
    for key, path in component["binaryAssets"].items():
        (static / f"ffmpeg-core-{key}-url.js").write_text(f'export default "/{path}";')
    packet = legal / "sources" / f"BeefTV-{version}-ffmpeg-wasm-sources.tar.gz"
    source_fixture(packet, component, component["binaryHashes"])
    size, sha = fingerprint(packet)
    component["source"] = {"name": packet.name, "size": size, "sha256": sha}
    for name in ("LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "GPL-2.0.txt", "GPL-3.0.txt", "LGPL-2.1.txt", "index.html"):
        (legal / name).write_bytes(b"preserved web notice fixture")
    (legal / "sources.json").write_text(json.dumps({"schema": 1, "version": version, "platform": "web", "components": [component]}))
    return packet, assets


class DistributionSourcesTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)

    def test_all_final_platform_layouts_have_matching_source_packets(self):
        payload = {"version": "v1.7.28", "platforms": {}}
        for platform in ("darwin-arm64", "darwin-amd64", "windows-amd64", "linux-amd64"):
            archive, _, _ = desktop_fixture(self.root, platform)
            size, sha = fingerprint(archive)
            payload["platforms"][platform] = {"size": size, "sha256": sha}
        self.assertEqual(len(release_sources(payload, self.root)), 4)

    def test_deleting_license_from_final_zip_fails(self):
        archive, entries, _ = desktop_fixture(self.root)
        del entries["BeefTV-linux/agent-host/licenses/GPL-2.0.txt"]
        write_zip(archive, entries)
        with self.assertRaisesRegex(SourceError, "Final ZIP missing"):
            verify_archive(archive, self.root, "linux-amd64", "v1.7.28")

    def test_source_links_or_candidate_sources_are_not_delivery(self):
        archive, _, _ = desktop_fixture(self.root, status="unresolved")
        with self.assertRaisesRegex(SourceError, "provenance is unresolved"):
            verify_archive(archive, self.root, "linux-amd64", "v1.7.28")
        next(self.root.glob("*.tar.gz")).unlink()
        with self.assertRaisesRegex(SourceError, "Missing source packet"):
            verify_archive(archive, self.root, "linux-amd64", "v1.7.28")

    def test_manifest_cannot_hide_changed_embedded_wasm(self):
        archive, entries, _ = desktop_fixture(self.root)
        entries["BeefTV-linux/BeefTV"] = entries["BeefTV-linux/BeefTV"].replace(b"wasm-fixture-", b"fake-fixture-")
        write_zip(archive, entries)
        with self.assertRaisesRegex(SourceError, "does not contain"):
            verify_archive(archive, self.root, "linux-amd64", "v1.7.28")

    def test_changed_native_binary_is_not_the_documented_build(self):
        archive, entries, _ = desktop_fixture(self.root, "windows-amd64")
        entries["agent-host/media-runtime/ffmpeg.exe"] = b"different binary"
        write_zip(archive, entries)
        with self.assertRaisesRegex(SourceError, "does not describe these binaries"):
            verify_archive(archive, self.root, "windows-amd64", "v1.7.28")

    def test_archive_fingerprint_and_internal_sources_are_both_checked(self):
        archive, _, components = desktop_fixture(self.root)
        source = self.root / components[0]["source"]["name"]
        with source.open("ab") as handle:
            handle.write(b"changed after indexing")
        with self.assertRaisesRegex(SourceError, "checksum mismatch"):
            verify_archive(archive, self.root, "linux-amd64", "v1.7.28")
        with tarfile.open(source) as packet:
            entries = {item.name: packet.extractfile(item).read() for item in packet.getmembers()}
        entries["source.tar"] = b"changed source"
        with tarfile.open(source, "w:gz") as packet:
            for name, data in entries.items():
                item = tarfile.TarInfo(name)
                item.size = len(data)
                packet.addfile(item, io.BytesIO(data))
        with self.assertRaisesRegex(SourceError, "Source checksum mismatch"):
            verify_source(source, components[0], components[0]["binaryHashes"])

    def test_malformed_source_and_desktop_archives_fail_with_source_error(self):
        archive, _, components = desktop_fixture(self.root)
        component = components[0]
        source = self.root / component["source"]["name"]
        for data in (b"not gzip", gzip.compress(b"not a tar archive")):
            with self.subTest(data=data):
                source.write_bytes(data)
                with self.assertRaises(SourceError):
                    verify_source(source, component, component["binaryHashes"])
        archive.write_bytes(b"not a zip archive")
        with self.assertRaises(SourceError):
            verify_archive(archive, self.root, "linux-amd64", "v1.7.28")

    def test_source_packet_rejects_unsafe_files_and_directories(self):
        _, _, components = desktop_fixture(self.root)
        component = components[0]
        source = self.root / component["source"]["name"]
        with tarfile.open(source) as packet:
            entries = {item.name: packet.extractfile(item).read() for item in packet.getmembers()}
        for name in ("../outside", "/outside", "nested/../../outside", "a\\outside", "C:/outside", "C:outside"):
            for kind in (tarfile.REGTYPE, tarfile.DIRTYPE):
                with self.subTest(name=name, kind=kind):
                    item = tarfile.TarInfo(name)
                    item.type = kind
                    write_source_packet(source, entries, [item])
                    with self.assertRaisesRegex(SourceError, "Unsafe"):
                        verify_source(source, component, component["binaryHashes"])

    def test_source_packet_rejects_links_and_duplicate_paths(self):
        _, _, components = desktop_fixture(self.root)
        component = components[0]
        source = self.root / component["source"]["name"]
        with tarfile.open(source) as packet:
            entries = {item.name: packet.extractfile(item).read() for item in packet.getmembers()}
        for kind, name in ((tarfile.SYMTYPE, "link"), (tarfile.LNKTYPE, "link"), (tarfile.REGTYPE, "COPYING")):
            with self.subTest(kind=kind, name=name):
                item = tarfile.TarInfo(name)
                item.type, item.linkname = kind, "COPYING"
                write_source_packet(source, entries, [item])
                with self.assertRaisesRegex(SourceError, "links|duplicate"):
                    verify_source(source, component, component["binaryHashes"])

    def test_final_zip_rejects_unsafe_entries_and_symlinks(self):
        archive, entries, _ = desktop_fixture(self.root)
        for name in ("../outside", "/outside", "nested/../../outside/", "a\\outside", "C:/outside", "C:outside"):
            with self.subTest(name=name):
                write_zip(archive, {**entries, name: b"unexpected file"})
                with self.assertRaisesRegex(SourceError, "Unsafe"):
                    verify_archive(archive, self.root, "linux-amd64", "v1.7.28")
        write_zip(archive, entries)
        with zipfile.ZipFile(archive, "a") as packet:
            link = zipfile.ZipInfo("BeefTV-linux/linked-license")
            link.create_system = 3
            link.external_attr = 0o120777 << 16
            packet.writestr(link, "agent-host/licenses/LICENSE")
        with self.assertRaisesRegex(SourceError, "(?i)link"):
            verify_archive(archive, self.root, "linux-amd64", "v1.7.28")

    def test_web_distribution_has_matching_emitted_assets_and_source_packet(self):
        packet, _ = web_fixture(self.root)
        self.assertEqual(verify_web(self.root, "v1.7.28"), [packet])

    def test_web_distribution_requires_its_local_source_packet(self):
        packet, _ = web_fixture(self.root)
        packet.unlink()
        with self.assertRaises((SourceError, OSError)):
            verify_web(self.root, "v1.7.28")

    def test_web_distribution_rejects_same_size_replacement_of_either_core_asset(self):
        _, assets = web_fixture(self.root)
        for key, asset in assets.items():
            with self.subTest(asset=key):
                original = asset.read_bytes()
                asset.write_bytes(bytes([original[0] ^ 1]) + original[1:])
                with self.assertRaisesRegex(SourceError, "does not match"):
                    verify_web(self.root, "v1.7.28")
                asset.write_bytes(original)

    def test_web_stale_core_copy_cannot_mask_a_replaced_asset(self):
        _, assets = web_fixture(self.root)
        for key, asset in assets.items():
            with self.subTest(asset=key):
                original = asset.read_bytes()
                stale = asset.with_name("ffmpeg-core-retained" + asset.suffix)
                stale.write_bytes(original)
                asset.write_bytes(bytes([original[0] ^ 1]) + original[1:])
                with self.assertRaises(SourceError):
                    verify_web(self.root, "v1.7.28")
                asset.write_bytes(original)
                stale.unlink()


if __name__ == "__main__":
    unittest.main()
