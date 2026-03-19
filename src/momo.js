import { BROWSER_HEADERS } from "./http.js";

const SEARCH_URL = "https://www.momoshop.com.tw/search/bsmi";
const PRODUCT_URL = "https://www.momoshop.com.tw/goods/GoodsDetail.jsp";

const BSMI_ID_RE = /[RTDQM]\d{5}/gi;
const BSMI_CONTEXT_RE =
  /(?:BSMI|bsmi|檢驗|認證|標檢局|登錄字號|商檢字號)[^\n]{0,30}([RTDQM]\d{5})/gi;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Fetch a momo search page and parse product list from RSC payload.
 */
async function searchMomo(page = 1) {
  const url = `${SEARCH_URL}?_isFuzzy=0&searchType=5&curPage=${page}`;
  const res = await fetch(url, {
    headers: { ...BROWSER_HEADERS, Referer: "https://www.momoshop.com.tw/" },
  });

  if (!res.ok) {
    throw new Error(`Momo search returned HTTP ${res.status}`);
  }

  const html = await res.text();

  // Extract RSC payload containing search data
  const pushRe = /self\.__next_f\.push\(\[1,"(.*?)"\]\)/g;
  for (const m of html.matchAll(pushRe)) {
    if (!m[1].includes("rtnSearchData")) continue;

    const unescaped = JSON.parse(`"${m[1]}"`);
    const start = unescaped.indexOf('{"success"');
    if (start === -1) continue;

    const [obj] = jsonRawDecode(unescaped.slice(start));
    const rtn = obj.rtnSearchData || {};

    return {
      goods: (rtn.goodsInfoList || []).map((g) => ({
        code: g.goodsCode,
        name: g.goodsName || "",
        subName: g.goodsSubName || "",
      })),
      maxPage: rtn.maxPage || 1,
      totalCount: rtn.totCnt || 0,
    };
  }

  return { goods: [], maxPage: 1, totalCount: 0 };
}

/** Minimal JSON.parse that stops at the end of the first value. */
function jsonRawDecode(str) {
  let depth = 0;
  let inString = false;
  let escape = false;

  for (let i = 0; i < str.length; i++) {
    const ch = str[i];
    if (escape) {
      escape = false;
      continue;
    }
    if (ch === "\\") {
      escape = true;
      continue;
    }
    if (ch === '"') {
      inString = !inString;
      continue;
    }
    if (inString) continue;
    if (ch === "{" || ch === "[") depth++;
    if (ch === "}" || ch === "]") {
      depth--;
      if (depth === 0) {
        return [JSON.parse(str.slice(0, i + 1)), i + 1];
      }
    }
  }

  return [JSON.parse(str), str.length];
}

/**
 * Extract BSMI IDs from text.
 */
function extractBsmiIds(text) {
  const ids = new Set();
  for (const m of text.matchAll(BSMI_ID_RE)) {
    ids.add(m[0].toUpperCase());
  }
  return [...ids];
}

/**
 * Fetch a momo product page and extract BSMI IDs with context matching.
 */
async function extractBsmiFromProductPage(goodsCode) {
  const res = await fetch(`${PRODUCT_URL}?i_code=${goodsCode}`, {
    headers: { ...BROWSER_HEADERS, Referer: "https://www.momoshop.com.tw/search/bsmi" },
  });

  if (!res.ok) return [];

  const html = await res.text();
  const ids = new Set();

  for (const m of html.matchAll(BSMI_CONTEXT_RE)) {
    ids.add(m[1].toUpperCase());
  }

  return [...ids];
}

/**
 * Scan momo search results for BSMI IDs.
 *
 * @param {object} options
 * @param {number} [options.maxPages=5] - Maximum search pages to scan
 * @returns {Promise<string[]>} Unique BSMI registration IDs
 */
export async function scanMomo({ maxPages = 5 } = {}) {
  const allIds = new Set();
  let maxPage = 1;

  for (let page = 1; page <= Math.min(maxPages, maxPage); page++) {
    console.log(`[momo] Searching page ${page}...`);

    const data = await searchMomo(page);
    maxPage = data.maxPage;

    if (data.goods.length === 0) break;

    for (const prod of data.goods) {
      // Try extracting from search result text first
      const text = `${prod.name} ${prod.subName}`;
      const ids = extractBsmiIds(text);

      if (ids.length > 0) {
        for (const id of ids) allIds.add(id);
        continue;
      }

      // Fall back to product page scraping
      await sleep(1000);
      const pageIds = await extractBsmiFromProductPage(prod.code);
      for (const id of pageIds) allIds.add(id);
    }

    await sleep(1500);
  }

  console.log(`[momo] Found ${allIds.size} unique BSMI IDs`);
  return [...allIds];
}

/**
 * Scan momo and upsert any new BSMI registrations found.
 *
 * @param {import('./db.js').default} prisma - Prisma client
 * @param {import('./bsmi.js').fetchBsmi} fetchBsmi - BSMI fetch function
 * @param {object} [options] - Options passed to scanMomo
 * @returns {Promise<string[]>} IDs that were newly imported
 */
export async function syncFromMomo(prisma, fetchBsmi, options) {
  const markIds = await scanMomo(options);
  const imported = [];

  for (const markId of markIds) {
    const existing = await prisma.registration.findUnique({
      where: { id: markId },
    });

    if (existing) continue;

    try {
      const data = await fetchBsmi(markId);
      if (!data) {
        console.log(`[momo] ${markId}: not found on BSMI`);
        continue;
      }

      const { certificates, ...vendor } = data;
      await prisma.$transaction(async (tx) => {
        await tx.certificate.deleteMany({
          where: { registrationId: vendor.id },
        });
        await tx.registration.upsert({
          where: { id: vendor.id },
          create: { ...vendor, certificates: { create: certificates } },
          update: { ...vendor, certificates: { create: certificates } },
        });
      });

      imported.push(markId);
      console.log(
        `[momo] ${markId}: imported (${certificates.length} certs)`,
      );
    } catch (err) {
      console.error(`[momo] ${markId}: failed -`, err.message);
    }

    await sleep(2000);
  }

  console.log(`[momo] Imported ${imported.length} new registrations`);
  return imported;
}
