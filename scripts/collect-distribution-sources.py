#!/usr/bin/env python3
"""Collect reviewed, immutable source packets before building a public release."""
import argparse
import json
from pathlib import Path
import re
import tempfile
import urllib.request

from distribution_sources import SourceError, fingerprint, verify_source

ROOT = Path(__file__).resolve().parent.parent


def collect(version, destination, ids, lock=ROOT / "licenses/distribution-inputs.json"):
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:[.-][\w.-]+)?", version):
        raise SourceError("Invalid release version")
    document = json.loads(Path(lock).read_bytes())
    if document.get("schema") != 1:
        raise SourceError("Unsupported distribution input schema")
    components = {item["id"]: item for item in document["components"]}
    selected = [components[key] for key in ids]
    # Inspect the whole set before creating any files or making network calls.
    for component in selected:
        source = component.get("source")
        if not source:
            raise SourceError(f"{component['id']}: {component.get('blocker', 'No reviewed source packet')}")
        if (not source["url"].startswith("https://") or
                not re.fullmatch(r"[a-f0-9]{64}", source["sha256"]) or
                type(source["size"]) is not int or not 0 < source["size"] < 2 * 1024**3):
            raise SourceError("Source URL, size and SHA256 must be pinned")
    destination = Path(destination)
    destination.mkdir(parents=True, exist_ok=True)
    results = []
    for component in selected:
        source = component["source"]
        target = destination / f"BeefTV-{version}-{component['id']}-sources.tar.gz"
        if not target.exists():
            with tempfile.TemporaryDirectory(dir=destination) as scratch:
                temporary = Path(scratch) / "source.tar.gz"
                with urllib.request.urlopen(source["url"], timeout=60) as response, temporary.open("wb") as output:
                    size = 0
                    while chunk := response.read(1024 * 1024):
                        size += len(chunk)
                        if size > source["size"]:
                            raise SourceError("Source packet exceeds pinned size")
                        output.write(chunk)
                if fingerprint(temporary) != (source["size"], source["sha256"]):
                    raise SourceError("Downloaded source packet checksum mismatch")
                verify_source(temporary, component, component["binaryHashes"])
                temporary.replace(target)
        if fingerprint(target) != (source["size"], source["sha256"]):
            raise SourceError("Existing source packet checksum mismatch")
        verify_source(target, component, component["binaryHashes"])
        results.append(target)
    return results


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--destination", type=Path, default=ROOT / "dist")
    parser.add_argument("--component", action="append", required=True,
                        choices=["ffmpeg-wasm", "ffmpeg-windows-amd64", "imageio-ffmpeg-windows-amd64"])
    args = parser.parse_args()
    try:
        for path in collect(args.version, args.destination, args.component):
            print(path.name)
    except (ValueError, KeyError, OSError) as error:
        parser.exit(1, f"Source delivery is not ready: {error}\n")
