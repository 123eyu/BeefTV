import { createHash } from "node:crypto";
import { readFileSync, mkdirSync, writeFileSync, existsSync } from "node:fs";
import { resolve, dirname, basename } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const escapeHTML = (text) => text.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");

export function distributionNotices({ root = repo, version, platform = "web", heavyMedia = true, sourcesDir = resolve(root, "dist") }) {
    if (!/^v?\d+\.\d+\.\d+(?:[.-][\w.-]+)?$/.test(version)) throw new Error("Invalid distribution version");
    version = `v${version.replace(/^v/, "")}`;
    if (!["web", "darwin-arm64", "darwin-amd64", "windows-amd64", "linux-amd64"].includes(platform)) throw new Error("Invalid distribution platform");
    const files = new Map();
    for (const name of ["LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"]) files.set(name, readFileSync(resolve(root, name)));
    for (const name of ["GPL-2.0.txt", "GPL-3.0.txt", "LGPL-2.1.txt"]) files.set(name, readFileSync(resolve(root, "licenses", name)));
    const required = [];
    if (heavyMedia) {
        const core = JSON.parse(readFileSync(resolve(root, "web/node_modules/@ffmpeg/core/package.json"), "utf8"));
        if (core.version !== "0.12.10" || core.license !== "GPL-2.0-or-later") throw new Error("FFmpeg.wasm changed; update its source delivery before distributing it");
        const wasm = readFileSync(resolve(root, "web/node_modules/@ffmpeg/core/dist/esm/ffmpeg-core.wasm"));
        const js = readFileSync(resolve(root, "web/node_modules/@ffmpeg/core/dist/esm/ffmpeg-core.js"));
        required.push({ id: "ffmpeg-wasm", version: core.version, license: core.license,
            binaryHashes: { wasm: sha256(wasm), javascript: sha256(js) },
            binarySizes: { wasm: wasm.length, javascript: js.length },
            binaryPrefixes: { wasm: wasm.subarray(0, 64).toString("base64"), javascript: js.subarray(0, 64).toString("base64") } });
    }
    if (platform.startsWith("darwin-")) required.push({ id: `ffprobe-${platform}`, version: "8.0.1", license: "LGPL-2.1-or-later" });
    if (platform === "windows-amd64") required.push({ id: "ffmpeg-windows-amd64", version: "8.0.1", license: "GPL-3.0-or-later" });
    const components = required.map((component) => {
        const name = `BeefTV-${version}-${component.id}-sources.tar.gz`;
        const source = resolve(sourcesDir, name);
        // Local builds can prepare notices before source collection. Publication
        // rejects missing source fingerprints; this is never a legal approval.
        const bytes = existsSync(source) ? readFileSync(source) : null;
        return { ...component, source: { name, size: bytes?.length ?? null, sha256: bytes ? sha256(bytes) : null } };
    });
    const index = { schema: 1, version, platform, components };
    files.set("sources.json", Buffer.from(JSON.stringify(index, null, 2) + "\n"));
    const links = components.map((component) => {
        const href = platform === "web" ? `sources/${component.source.name}` : `https://github.com/glanderness/BeefTV/releases/download/${version}/${component.source.name}`;
        return `<li>${escapeHTML(component.id)} ${escapeHTML(component.version)} — ${escapeHTML(component.license)}<br><a href="${href}" target="_blank" rel="noopener noreferrer">对应源码与构建说明</a></li>`;
    }).join("\n");
    files.set("index.html", Buffer.from(`<!doctype html>
<html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>BeefTV 第三方许可与源码</title>
<style>:root{color-scheme:light dark}body{font:15px/1.7 system-ui,sans-serif;max-width:760px;margin:24px auto;padding:0 20px;color:CanvasText;background:Canvas;overflow-wrap:anywhere}h1{font-size:22px}ul{padding-left:24px}li{margin:16px 0}a{color:LinkText}a:focus-visible{outline:2px solid Highlight;outline-offset:3px}pre{white-space:pre-wrap}</style>
<h1>第三方许可与源码</h1><p>BeefTV ${escapeHTML(version)} 使用以下媒体组件。各组件继续适用自己的许可证；BeefTV 的商业使用限制不替代这些许可证。</p>
<ul>${links}</ul><p>源码包包含相应组件的源码、许可证和构建说明。源码的修改与再分发权利以各组件许可证为准。</p>
<p><a href="GPL-2.0.txt">GPL 2.0</a> · <a href="GPL-3.0.txt">GPL 3.0</a> · <a href="LGPL-2.1.txt">LGPL 2.1</a></p>
<p><a href="LICENSE">BeefTV 许可</a> · <a href="NOTICE">版权声明</a> · <a href="THIRD_PARTY_NOTICES.md">第三方清单</a> · <a href="sources.json">版本与校验信息</a></p></html>\n`));
    return files;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
    const [platform, destination, version, sourcesDir] = process.argv.slice(2);
    if (!destination || !version) throw new Error(`Usage: ${basename(process.argv[1])} platform destination version [sources-dir]`);
    mkdirSync(destination, { recursive: true });
    for (const [name, bytes] of distributionNotices({ platform, version, sourcesDir, heavyMedia: process.env.BEEFTV_FULL_MEDIA_RESOURCES !== "0" })) writeFileSync(resolve(destination, name), bytes);
}
