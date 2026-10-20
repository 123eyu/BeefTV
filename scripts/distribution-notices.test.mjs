import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import test from "node:test";
import { distributionNotices } from "./distribution-notices.mjs";

function fixture(t) {
    const root = mkdtempSync(resolve(tmpdir(), "beeftv-notices-"));
    t.after(() => rmSync(root, { recursive: true, force: true }));
    for (const name of ["LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "licenses/GPL-2.0.txt", "licenses/GPL-3.0.txt", "licenses/LGPL-2.1.txt",
        "web/node_modules/@ffmpeg/core/dist/esm/ffmpeg-core.wasm", "web/node_modules/@ffmpeg/core/dist/esm/ffmpeg-core.js"]) {
        mkdirSync(resolve(root, name, ".."), { recursive: true });
        writeFileSync(resolve(root, name), name);
    }
    writeFileSync(resolve(root, "web/node_modules/@ffmpeg/core/package.json"), JSON.stringify({ version: "0.12.10", license: "GPL-2.0-or-later" }));
    return root;
}

test("every platform includes offline texts without inventing source downloads", (t) => {
    const root = fixture(t);
    for (const platform of ["web", "darwin-arm64", "darwin-amd64", "windows-amd64", "linux-amd64"]) {
        const files = distributionNotices({ root, version: "1.7.28", platform });
        const index = JSON.parse(files.get("sources.json"));
        assert.equal(index.version, "v1.7.28");
        assert.ok(index.components.every((c) => c.source === null && c.upstream.startsWith("https://")));
        assert.equal(index.components[0].binaryHashes.wasm, createHash("sha256").update("web/node_modules/@ffmpeg/core/dist/esm/ffmpeg-core.wasm").digest("hex"));
        for (const name of ["LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "GPL-2.0.txt", "GPL-3.0.txt", "LGPL-2.1.txt"]) assert.ok(files.get(name)?.length);
        assert.doesNotMatch(files.get("index.html").toString(), /releases\/download|href="sources\//);
    }
});

test("macOS source download names and hashes match the generated packet", (t) => {
    const root = fixture(t);
    mkdirSync(resolve(root, "dist"));
    const name = "BeefTV-v1.7.28-ffprobe-darwin-arm64-sources.tar.gz";
    writeFileSync(resolve(root, "dist", name), "source-packet");
    const files = distributionNotices({ root, version: "v1.7.28", platform: "darwin-arm64" });
    const source = JSON.parse(files.get("sources.json")).components[1].source;
    assert.equal(source.name, name);
    assert.equal(source.size, 13);
    assert.equal(source.sha256, createHash("sha256").update("source-packet").digest("hex"));
    assert.ok(source.url.endsWith(`/v1.7.28/${name}`));
    assert.ok(files.get("index.html").toString().includes(source.url));
});

test("slim builds omit WASM and invalid inputs fail before producing notices", (t) => {
    const root = fixture(t);
    const files = distributionNotices({ root, version: "v1.7.28", heavyMedia: false });
    assert.deepEqual(JSON.parse(files.get("sources.json")).components, []);
    assert.throws(() => distributionNotices({ root, version: '<script>' }), /version/);
    assert.throws(() => distributionNotices({ root, version: 'v1.7.28', platform: '../bad' }), /platform/);
    writeFileSync(resolve(root, "web/node_modules/@ffmpeg/core/package.json"), JSON.stringify({ version: "0.99.0", license: "GPL-2.0-or-later" }));
    assert.throws(() => distributionNotices({ root, version: 'v1.7.28' }), /third-party notice/);
});
