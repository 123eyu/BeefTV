"""Exercise reviewed source collection with local packets and no network writes."""

import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from distribution_sources import SourceError, fingerprint
from test_distribution_sources import source_fixture


spec = importlib.util.spec_from_file_location("collect_distribution_sources", Path(__file__).with_name("collect-distribution-sources.py"))
collector = importlib.util.module_from_spec(spec)
spec.loader.exec_module(collector)


class CollectDistributionSourcesTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.destination = self.root / "collected"
        self.component = {"id": "ffmpeg-wasm", "version": "0.12.10", "license": "GPL-2.0-or-later",
                          "binaryHashes": {"wasm": hashlib.sha256(b"wasm fixture").hexdigest(),
                                           "javascript": hashlib.sha256(b"javascript fixture").hexdigest()}}
        self.packet = self.root / "reviewed-source.tar.gz"
        source_fixture(self.packet, self.component, self.component["binaryHashes"])
        size, sha = fingerprint(self.packet)
        self.component["source"] = {"url": "https://sources.example/immutable/ffmpeg-source.tar.gz", "size": size, "sha256": sha}
        self.lock = self.root / "inputs.json"
        self.write_lock([self.component])
        self.target = self.destination / "BeefTV-v1.7.28-ffmpeg-wasm-sources.tar.gz"

    def write_lock(self, components):
        self.lock.write_text(json.dumps({"schema": 1, "components": components}))

    def test_unconfigured_component_blocks_the_whole_set_without_files_or_network(self):
        missing = {"id": "ffmpeg-windows-amd64", "source": None, "blocker": "Exact corresponding source not reviewed"}
        self.write_lock([self.component, missing])
        before = set(self.root.rglob("*"))
        with patch.object(collector.urllib.request, "urlopen") as download:
            with self.assertRaisesRegex(SourceError, "Exact corresponding source not reviewed"):
                collector.collect("v1.7.28", self.destination, [self.component["id"], missing["id"]], self.lock)
            download.assert_not_called()
        self.assertEqual(set(self.root.rglob("*")), before)
        self.assertFalse(self.destination.exists())

    def test_same_size_corrupt_cache_is_rejected_without_redownload_or_overwrite(self):
        self.destination.mkdir()
        original = self.packet.read_bytes()
        corrupt = bytes([original[0] ^ 1]) + original[1:]
        self.target.write_bytes(corrupt)
        with patch.object(collector.urllib.request, "urlopen") as download:
            with self.assertRaisesRegex(SourceError, "Existing source packet checksum mismatch"):
                collector.collect("v1.7.28", self.destination, [self.component["id"]], self.lock)
            download.assert_not_called()
        self.assertEqual(self.target.read_bytes(), corrupt)
        self.assertEqual(list(self.destination.iterdir()), [self.target])

    def test_valid_reviewed_packet_is_collected_and_reused_without_network(self):
        original = self.packet.read_bytes()
        with patch.object(collector.urllib.request, "urlopen", return_value=io.BytesIO(original)) as download:
            self.assertEqual(collector.collect("v1.7.28", self.destination, [self.component["id"]], self.lock), [self.target])
            self.assertEqual(download.call_args.args[0], self.component["source"]["url"])
            download.assert_called_once()
        self.assertEqual(self.target.read_bytes(), original)
        self.assertEqual(list(self.destination.iterdir()), [self.target])
        with patch.object(collector.urllib.request, "urlopen") as download:
            self.assertEqual(collector.collect("v1.7.28", self.destination, [self.component["id"]], self.lock), [self.target])
            download.assert_not_called()

    def test_bad_download_never_becomes_a_collected_source_packet(self):
        original = self.packet.read_bytes()
        corrupt = bytes([original[0] ^ 1]) + original[1:]
        with patch.object(collector.urllib.request, "urlopen", return_value=io.BytesIO(corrupt)):
            with self.assertRaisesRegex(SourceError, "Downloaded source packet checksum mismatch"):
                collector.collect("v1.7.28", self.destination, [self.component["id"]], self.lock)
        self.assertFalse(self.target.exists())
        self.assertEqual(list(self.destination.iterdir()), [])

    def test_matching_checksum_does_not_make_unresolved_provenance_acceptable(self):
        source_fixture(self.packet, self.component, self.component["binaryHashes"], status="unresolved")
        size, sha = fingerprint(self.packet)
        self.component["source"].update(size=size, sha256=sha)
        self.write_lock([self.component])
        with patch.object(collector.urllib.request, "urlopen", return_value=io.BytesIO(self.packet.read_bytes())):
            with self.assertRaisesRegex(SourceError, "provenance is unresolved"):
                collector.collect("v1.7.28", self.destination, [self.component["id"]], self.lock)
        self.assertFalse(self.target.exists())
        self.assertEqual(list(self.destination.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
