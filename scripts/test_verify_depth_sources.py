import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

from distribution_sources import SourceError, fingerprint
from test_distribution_sources import source_fixture

spec = importlib.util.spec_from_file_location("verify_depth_sources", Path(__file__).with_name("verify-depth-sources.py"))
depth = importlib.util.module_from_spec(spec)
spec.loader.exec_module(depth)


class DepthSourceTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.version = "v1.7.28"
        self.lock = self.root / "inputs.json"
        self.binaries = {"bin/ffmpeg.exe": b"Gyan ffmpeg fixture", "bin/ffprobe.exe": b"Gyan ffprobe fixture",
                         depth.IMAGEIO_BINARIES + "ffmpeg-win-x86_64-v7.1.exe": b"wheel ffmpeg fixture"}
        self.components = [
            {"id": depth.COMPONENTS[0], "version": "8.0.1", "license": "GPL-3.0-or-later",
             "binaryHashes": {key: hashlib.sha256(self.binaries[f"bin/{key}.exe"]).hexdigest() for key in ("ffmpeg", "ffprobe")}},
            {"id": depth.COMPONENTS[1], "version": "0.6.0", "license": "GPL-3.0-or-later",
             "binaryHashes": {"ffmpeg": hashlib.sha256(self.binaries[depth.IMAGEIO_BINARIES + "ffmpeg-win-x86_64-v7.1.exe"]).hexdigest()}},
        ]
        for component in self.components:
            packet = self.root / f"BeefTV-{self.version}-{component['id']}-sources.tar.gz"
            source_fixture(packet, component, component["binaryHashes"])
            size, sha = fingerprint(packet)
            component["source"] = {"url": "https://sources.example/reviewed.tar.gz", "size": size, "sha256": sha}
        self.write_lock()
        hashes = self.components[0]["binaryHashes"]
        self.entries = {**self.binaries, "licenses/ffmpeg/LICENSE": b"GNU license fixture",
                        "licenses/ffmpeg/README.txt": b"build instructions fixture",
                        "licenses/ffmpeg/manifest.json": json.dumps({"binarySHA256": hashes["ffmpeg"], "probeSHA256": hashes["ffprobe"]}).encode(),
                        depth.IMAGEIO_LICENSE: b"BSD wheel license fixture"}
        self.write_runtime("cpu", self.entries)
        self.write_runtime("cuda", self.entries)

    def write_lock(self):
        self.lock.write_text(json.dumps({"schema": 1, "components": self.components}))

    def write_runtime(self, variant, entries):
        name = f"beeftv-depth-runtime-v1-windows-amd64-{variant}.zip"
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            for path, data in entries.items():
                archive.writestr(path, data)
        data = buffer.getvalue()
        metadata = {"name": name, "size": len(data), "sha256": hashlib.sha256(data).hexdigest(),
                    "files": len(entries), "expandedSize": sum(map(len, entries.values()))}
        if variant == "cuda":
            parts = []
            middle = len(data) // 2
            for index, content in enumerate((data[:middle], data[middle:]), 1):
                part_name = f"{name}.part-{index:03d}"
                (self.root / part_name).write_bytes(content)
                parts.append({"name": part_name, "size": len(content), "sha256": hashlib.sha256(content).hexdigest()})
            metadata["parts"] = parts
        else:
            (self.root / name).write_bytes(data)
        (self.root / (name + ".json")).write_text(json.dumps(metadata), encoding="utf-8-sig")

    def verify(self):
        return depth.verify(self.root, self.version, self.lock)

    def test_final_cpu_and_split_cuda_bind_both_ffmpeg_distributions(self):
        scratch = self.root / "scratch"
        scratch.mkdir()
        before = set(self.root.iterdir())
        with patch.object(depth.tempfile, "tempdir", str(scratch)):
            self.assertEqual({path.name for path in self.verify()},
                             {f"BeefTV-{self.version}-{component['id']}-sources.tar.gz" for component in self.components})
        self.assertEqual(list(scratch.iterdir()), [])
        self.assertEqual(set(self.root.iterdir()), before)
        self.assertFalse((self.root / "beeftv-depth-runtime-v1-windows-amd64-cuda.zip").exists())

    def test_existing_part_protocol_is_checked_before_source_packets(self):
        part = self.root / "beeftv-depth-runtime-v1-windows-amd64-cuda.zip.part-002"
        content = part.read_bytes()
        part.write_bytes(bytes([content[0] ^ 1]) + content[1:])
        with patch.object(depth, "verify_source") as source_check:
            with self.assertRaisesRegex(SourceError, "artifact hash mismatch"):
                self.verify()
            source_check.assert_not_called()

    def test_swapped_cuda_part_order_is_rejected(self):
        path = self.root / "beeftv-depth-runtime-v1-windows-amd64-cuda.zip.json"
        metadata = json.loads(path.read_text(encoding="utf-8-sig"))
        metadata["parts"].reverse()
        path.write_text(json.dumps(metadata))
        with self.assertRaisesRegex(SourceError, "invalid release part"):
            self.verify()

    def test_unreviewed_or_missing_source_packet_blocks_otherwise_valid_runtime(self):
        for component in self.components:
            with self.subTest(component=component["id"]):
                source = component["source"]
                component["source"] = None
                self.write_lock()
                with self.assertRaisesRegex(SourceError, "No reviewed source packet"):
                    self.verify()
                component["source"] = source
        self.write_lock()
        next(self.root.glob("*-sources.tar.gz")).unlink()
        with self.assertRaises(SourceError):
            self.verify()

    def test_each_binary_must_match_the_reviewed_source_hashes(self):
        for binary in self.binaries:
            with self.subTest(binary=binary):
                original = self.entries[binary]
                entries = {**self.entries, binary: bytes([original[0] ^ 1]) + original[1:]}
                # Updating the local notice cannot override the reviewed lock.
                if binary.startswith("bin/"):
                    hashes = dict(self.components[0]["binaryHashes"])
                    hashes[Path(binary).stem] = hashlib.sha256(entries[binary]).hexdigest()
                    entries["licenses/ffmpeg/manifest.json"] = json.dumps({"binarySHA256": hashes["ffmpeg"], "probeSHA256": hashes["ffprobe"]}).encode()
                self.write_runtime("cuda", entries)
                with self.assertRaisesRegex(SourceError, "binaries differ from reviewed"):
                    self.verify()

    def test_undeclared_or_missing_ffmpeg_executables_are_rejected(self):
        for name in ("bin/ffmpeg-old.exe", "helpers/custom-ffmpeg.exe", ".python/Scripts/ffprobe.exe",
                     depth.IMAGEIO_BINARIES + "ffmpeg-second.exe"):
            with self.subTest(extra=name):
                self.write_runtime("cpu", {**self.entries, name: b"undeclared executable"})
                with self.assertRaisesRegex(SourceError, "missing or undeclared"):
                    self.verify()
        entries = dict(self.entries)
        del entries["bin/ffprobe.exe"]
        self.write_runtime("cpu", entries)
        with self.assertRaisesRegex(SourceError, "missing or undeclared"):
            self.verify()

    def test_missing_licenses_are_rejected_and_reassembled_zip_is_cleaned(self):
        scratch = self.root / "scratch"
        scratch.mkdir()
        for notice in ("licenses/ffmpeg/LICENSE", depth.IMAGEIO_LICENSE):
            with self.subTest(notice=notice), patch.object(depth.tempfile, "tempdir", str(scratch)):
                entries = dict(self.entries)
                del entries[notice]
                self.write_runtime("cuda", entries)
                with self.assertRaises(SourceError):
                    self.verify()
                self.assertEqual(list(scratch.iterdir()), [])

    def test_source_packet_same_size_tampering_is_rejected(self):
        packet = next(self.root.glob("*-sources.tar.gz"))
        original = packet.read_bytes()
        packet.write_bytes(bytes([original[0] ^ 1]) + original[1:])
        with self.assertRaisesRegex(SourceError, "source packet checksum mismatch"):
            self.verify()


if __name__ == "__main__":
    unittest.main()
