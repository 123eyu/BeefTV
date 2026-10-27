import { afterAll, beforeAll, expect, test } from "bun:test";
import { existsSync } from "node:fs";
import { chromium, type Browser } from "playwright";

/**
 * 助手回复必须在终态之前分多段出现在面板里。
 *
 * 慢速替身按 750ms 间隔分四次发 text_snapshot，中间夹一次工具进度，最后才发 turn_end。
 * 只要有任何一层把正文攒到最后，面板在终态前就只会看到一段或零段。
 */

let browser: Browser, server: ReturnType<typeof Bun.serve>;
let chatRequests = 0;

const CHUNKS = ["第一段内容。", "第二段内容。", "第三段内容。", "第四段内容。"];
const CHUNK_GAP_MS = 750;

function slowChatStream(): Response {
    const encoder = new TextEncoder();
    const stream = new ReadableStream({
        async start(controller) {
            const line = (payload: unknown) => controller.enqueue(encoder.encode(JSON.stringify(payload) + "\n"));
            line({ type: "session", sessionId: "stream-session" });
            let text = "";
            for (const [index, chunk] of CHUNKS.entries()) {
                await new Promise((resolve) => setTimeout(resolve, CHUNK_GAP_MS));
                text += chunk;
                line({ type: "text_snapshot", text });
                // 工具进度夹在正文中间：工具前后的正文都要留着，且不重复。
                if (index === 1) line({ type: "lifecycle", phase: "tool", toolName: "canvas_get" });
            }
            await new Promise((resolve) => setTimeout(resolve, CHUNK_GAP_MS));
            line({ type: "turn_end", reply: text, toolCalls: [], error: null, cancelled: false });
            controller.close();
        },
    });
    return new Response(stream, { headers: { "content-type": "application/x-ndjson" } });
}

beforeAll(async () => {
    const build = await Bun.build({
        entrypoints: [import.meta.dir + "/fixtures/assistant-stream-harness.tsx"],
        target: "browser",
        define: {
            "import.meta.env.DEV": "false", "import.meta.env.PROD": "true", "import.meta.env.MODE": '"production"',
            "import.meta.env.VITE_CANVAS_LOCAL_MODE": '"true"', "import.meta.env.VITE_CANVAS_BACKEND_URL": '"/api"', "process.env.NODE_ENV": '"production"',
        },
        plugins: [{
            name: "test-source",
            setup(builder) { builder.onResolve({ filter: /^@\// }, args => ({ path: Bun.resolveSync("../src/" + args.path.slice(2), import.meta.dir) })); },
        }],
    });
    if (!build.success) throw new Error(build.logs.join("\n"));
    const js = await build.outputs.find(output => output.path.endsWith(".js"))!.text();
    const ok = (data: unknown) => Response.json({ code: 0, data });
    server = Bun.serve({
        port: 0,
        async fetch(req) {
            const pathname = new URL(req.url).pathname;
            if (pathname === "/harness.js") return new Response(js, { headers: { "content-type": "application/javascript" } });
            if (pathname.endsWith("/assistant/ui-session")) return ok({ token: "synthetic-only", expiresAt: new Date(Date.now() + 1_800_000).toISOString() });
            if (pathname.endsWith("/assistant/status")) return ok({ available: true });
            if (pathname.endsWith("/assistant/sessions")) return ok({ currentSessionId: "stream-session", sessions: [] });
            if (pathname.endsWith("/assistant/history")) return ok({ sessionId: "stream-session", turns: [], active: null });
            if (pathname.endsWith("/assistant/chat")) { chatRequests++; return slowChatStream(); }
            if (pathname.startsWith("/api/")) return ok({});
            return new Response('<html><div id="root"></div><script type="module" src="/harness.js"></script></html>', { headers: { "content-type": "text/html" } });
        },
    });
    const executablePath = [process.env.CHROME_PATH, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"]
        .find((candidate): candidate is string => Boolean(candidate && existsSync(candidate)));
    browser = await chromium.launch({ executablePath, headless: true });
}, 60000);
afterAll(async () => { await browser?.close(); server?.stop(true); });

test("终态之前面板已分多段显示助手正文", async () => {
    const page = await browser.newPage();
    try {
        await page.goto(server.url.toString());
        await page.evaluate(() => {
            const samples: string[] = [];
            (window as unknown as { streamSamples: string[] }).streamSamples = samples;
            setInterval(() => {
                const element = document.querySelector(".canvas-assistant-reply");
                if (element) samples.push((element as HTMLElement).innerText);
            }, 100);
        });
        await page.getByRole("button", { name: "开始", exact: true }).click();
        await page.waitForFunction(() => document.querySelector('[data-testid="streaming"]')?.textContent === "true");
        // 终态在最后一次分片后 750ms 才到；这里必须已经能看到第三段，第四段还没到。
        await page.waitForFunction(
            () => (document.querySelector(".canvas-assistant-reply") as HTMLElement | null)?.innerText.includes("第三段内容。") === true,
            undefined, { timeout: 10_000 },
        );
        const beforeEnd = await page.evaluate(() => (document.querySelector(".canvas-assistant-reply") as HTMLElement | null)?.innerText || "");
        expect(beforeEnd).toContain("第一段内容。");
        expect(beforeEnd).toContain("第二段内容。");
        expect(beforeEnd).toContain("第三段内容。");
        expect(beforeEnd).not.toContain("第四段内容。");
        expect(await page.getByTestId("streaming").innerText()).toBe("true");

        await page.waitForFunction(() => document.querySelector('[data-testid="streaming"]')?.textContent === "false", undefined, { timeout: 10_000 });
        const samples = await page.evaluate(() => [...new Set((window as unknown as { streamSamples: string[] }).streamSamples)]);
        // 至少三个分开发出的片段各自单独出现过一次。
        expect(samples.filter(text => text.includes("第一段内容。")).length).toBeGreaterThan(0);
        expect(samples.filter(text => text.includes("第二段内容。") && !text.includes("第三段内容。")).length).toBeGreaterThan(0);
        expect(samples.filter(text => text.includes("第三段内容。") && !text.includes("第四段内容。")).length).toBeGreaterThan(0);
        expect(samples.at(-1)).toContain("第四段内容。");
        expect(chatRequests).toBe(1);
    } finally { await page.close(); }
}, 40000);
