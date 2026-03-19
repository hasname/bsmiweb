import { BROWSER_HEADERS } from "./http.js";
import { extractBsmiIds, extractBsmiIdsWithContext, sleep, createSyncFromEc } from "./utils.js";

const SEARCH_URL = "https://www.momoshop.com.tw/search/bsmi";
const PRODUCT_URL = "https://www.momoshop.com.tw/goods/GoodsDetail.jsp";

async function searchMomo(page = 1) {
  const url = `${SEARCH_URL}?_isFuzzy=0&searchType=5&curPage=${page}`;
  const res = await fetch(url, {
    headers: { ...BROWSER_HEADERS, Referer: "https://www.momoshop.com.tw/" },
  });

  if (!res.ok) {
    throw new Error(`Momo search returned HTTP ${res.status}`);
  }

  const html = await res.text();

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

async function extractBsmiFromProductPage(goodsCode) {
  const res = await fetch(`${PRODUCT_URL}?i_code=${goodsCode}`, {
    headers: { ...BROWSER_HEADERS, Referer: "https://www.momoshop.com.tw/search/bsmi" },
  });

  if (!res.ok) return [];

  const html = await res.text();
  return extractBsmiIdsWithContext(html);
}

export async function scanMomo({ maxPages = 5 } = {}) {
  const allIds = new Set();
  let maxPage = 1;

  for (let page = 1; page <= Math.min(maxPages, maxPage); page++) {
    console.log(`[momo] Searching page ${page}...`);

    const data = await searchMomo(page);
    maxPage = data.maxPage;

    if (data.goods.length === 0) break;

    for (const prod of data.goods) {
      const text = `${prod.name} ${prod.subName}`;
      const ids = extractBsmiIds(text);

      if (ids.length > 0) {
        for (const id of ids) allIds.add(id);
        continue;
      }

      await sleep(1000);
      const pageIds = await extractBsmiFromProductPage(prod.code);
      for (const id of pageIds) allIds.add(id);
    }

    await sleep(1500);
  }

  console.log(`[momo] Found ${allIds.size} unique BSMI IDs`);
  return [...allIds];
}

export const syncFromMomo = createSyncFromEc("momo", scanMomo);
