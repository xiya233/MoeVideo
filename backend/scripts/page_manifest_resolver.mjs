#!/usr/bin/env bun

import { chromium } from "playwright";

const MEDIA_EXT_RE = /\.(m3u8|mp4|webm|mov|mkv|ts)(\?|#|$)/i;
const ABS_MEDIA_RE = /(https?:\/\/[^\s"'<>\\]+?\.(?:m3u8|mp4|webm|mov|mkv|ts)(?:[^\s"'<>\\]*)?)/gi;
const REL_MEDIA_RE = /((?:\/|\.\.?\/)[^\s"'<>\\]+?\.(?:m3u8|mp4|webm|mov|mkv|ts)(?:[^\s"'<>\\]*)?)/gi;

function parseArgs(argv) {
  const opts = {
    url: "",
    timeoutMs: 25000,
    maxCandidates: 20,
  };
  for (let i = 2; i < argv.length; i += 1) {
    const token = argv[i];
    if (token === "--url") {
      opts.url = String(argv[i + 1] || "").trim();
      i += 1;
      continue;
    }
    if (token === "--timeout-ms") {
      const value = Number(argv[i + 1] || "");
      if (Number.isFinite(value) && value > 0) {
        opts.timeoutMs = value;
      }
      i += 1;
      continue;
    }
    if (token === "--max-candidates") {
      const value = Number(argv[i + 1] || "");
      if (Number.isFinite(value) && value > 0) {
        opts.maxCandidates = Math.floor(value);
      }
      i += 1;
      continue;
    }
  }
  return opts;
}

function isHTTPURL(value) {
  try {
    const parsed = new URL(value);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}

function normalizeURL(raw, baseURL) {
  const value = String(raw || "").trim().replace(/^["']|["']$/g, "");
  if (!value) {
    return "";
  }
  if (value.startsWith("javascript:") || value.startsWith("data:") || value.startsWith("blob:")) {
    return "";
  }
  if (value.startsWith("//")) {
    try {
      const base = new URL(baseURL);
      return `${base.protocol}${value}`;
    } catch {
      return `https:${value}`;
    }
  }
  try {
    return new URL(value, baseURL).toString();
  } catch {
    return "";
  }
}

function looksLikeMediaURL(value) {
  const lower = String(value || "").toLowerCase();
  if (!lower.startsWith("http://") && !lower.startsWith("https://")) {
    return false;
  }
  return (
    lower.includes(".m3u8") ||
    lower.includes(".mp4") ||
    lower.includes(".webm") ||
    lower.includes(".mov") ||
    lower.includes(".mkv") ||
    lower.includes(".ts")
  );
}

function scoreCandidate(url) {
  const lower = url.toLowerCase();
  let score = 0;
  if (lower.includes("master.m3u8")) score += 450;
  if (lower.includes("playlist.m3u8")) score += 420;
  if (lower.includes(".m3u8")) score += 350;
  if (lower.includes("index.m3u8")) score += 150;
  if (MEDIA_EXT_RE.test(lower)) score += 120;
  if (lower.includes("hls")) score += 60;
  if (lower.includes("manifest")) score += 50;
  return score;
}

function collectFromText(text, baseURL, collector) {
  if (!text) return;
  for (const match of text.matchAll(ABS_MEDIA_RE)) {
    collector.add(normalizeURL(match[1], baseURL));
  }
  for (const match of text.matchAll(REL_MEDIA_RE)) {
    collector.add(normalizeURL(match[1], baseURL));
  }
}

async function collectCandidates(page, pageURL, maxCandidates) {
  const raw = new Set();
  const pushCandidate = (input) => {
    const normalized = normalizeURL(input, page.url() || pageURL);
    if (!normalized || !looksLikeMediaURL(normalized)) {
      return;
    }
    raw.add(normalized);
  };

  page.on("request", (req) => {
    pushCandidate(req.url());
  });
  page.on("response", async (resp) => {
    const url = resp.url();
    const contentType = String(resp.headers()?.["content-type"] || "").toLowerCase();
    if (looksLikeMediaURL(url) || contentType.includes("mpegurl") || contentType.includes("video/")) {
      pushCandidate(url);
    }
  });

  await page.goto(pageURL, {
    waitUntil: "domcontentloaded",
    timeout: 20000,
  });
  await page.waitForTimeout(1200);
  await page.waitForLoadState("networkidle", { timeout: 5000 }).catch(() => {});

  const pageData = await page.evaluate(() => {
    const attrs = [];
    const addAttr = (value) => {
      if (typeof value === "string" && value.trim()) {
        attrs.push(value.trim());
      }
    };

    document.querySelectorAll("video, source, a, link, script").forEach((node) => {
      addAttr(node.getAttribute("src"));
      addAttr(node.getAttribute("href"));
      addAttr(node.getAttribute("data-src"));
      addAttr(node.getAttribute("data-url"));
      addAttr(node.getAttribute("data-video"));
      addAttr(node.getAttribute("data-hls"));
    });

    const inlineScripts = [];
    document.querySelectorAll("script:not([src])").forEach((node, idx) => {
      if (idx < 30 && node.textContent) {
        inlineScripts.push(node.textContent.slice(0, 8000));
      }
    });

    const htmlSnippet = (document.documentElement?.innerHTML || "").slice(0, 120000);
    return {
      title: document.title || "",
      attrs,
      inlineScripts,
      htmlSnippet,
      finalURL: window.location.href || "",
    };
  });

  for (const value of pageData.attrs) {
    pushCandidate(value);
  }
  for (const scriptText of pageData.inlineScripts) {
    collectFromText(scriptText, pageData.finalURL || pageURL, raw);
  }
  collectFromText(pageData.htmlSnippet, pageData.finalURL || pageURL, raw);

  const candidates = [...raw]
    .filter((value) => isHTTPURL(value) && looksLikeMediaURL(value))
    .sort((a, b) => scoreCandidate(b) - scoreCandidate(a))
    .slice(0, Math.max(1, maxCandidates));

  return {
    finalURL: pageData.finalURL || page.url() || pageURL,
    title: pageData.title || "",
    candidates,
  };
}

async function main() {
  const opts = parseArgs(process.argv);
  if (!isHTTPURL(opts.url)) {
    throw new Error("invalid --url, must be http/https");
  }

  const launchTimeout = Math.max(5000, opts.timeoutMs);
  const browser = await chromium.launch({ headless: true, timeout: launchTimeout });
  try {
    const context = await browser.newContext({
      userAgent:
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
    });
    const page = await context.newPage();
    page.setDefaultTimeout(launchTimeout);

    const result = await collectCandidates(page, opts.url, opts.maxCandidates);
    const reason = result.candidates.length > 0 ? "" : "no media candidates found";

    process.stdout.write(
      JSON.stringify({
        final_url: result.finalURL,
        title: result.title,
        candidates: result.candidates,
        reason,
      }),
    );
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  const message = err instanceof Error ? err.message : String(err);
  process.stderr.write(message);
  process.exit(1);
});
