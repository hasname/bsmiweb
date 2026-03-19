import { BROWSER_HEADERS, JSON_HEADERS } from "./http.js";
import {
  extractBsmiIds,
  extractBsmiIdsWithContext,
  sleep,
  createSyncFromEc,
} from "./utils.js";

const SEARCH_URL = "https://ecshweb.pchome.com.tw/search/v4.3/all/results";
const PRODUCT_URL = "https://24h.pchome.com.tw/prod/";

async function searchPchome(query, page = 1) {
  const url = `${SEARCH_URL}?q=${encodeURIComponent(query)}&page=${page}&sort=new/dc`;
  const res = await fetch(url, {
    headers: { ...JSON_HEADERS, Referer: "https://24h.pchome.com.tw/" },
  });

  if (!res.ok) {
    throw new Error(`PChome search returned HTTP ${res.status}`);
  }

  return res.json();
}

async function extractBsmiFromProductPage(productId) {
  const res = await fetch(`${PRODUCT_URL}${productId}`, {
    headers: {
      ...BROWSER_HEADERS,
      Referer: "https://24h.pchome.com.tw/search/?q=bsmi",
    },
  });

  if (!res.ok) return [];

  const html = await res.text();
  return extractBsmiIdsWithContext(html);
}

export async function scanPchome({ query = "bsmi", maxPages = 5 } = {}) {
  const allIds = new Set();
  let totalPages = 1;

  for (let page = 1; page <= Math.min(maxPages, totalPages); page++) {
    console.log(`[pchome] Searching page ${page}...`);

    const data = await searchPchome(query, page);
    totalPages = data.TotalPage || 1;
    const prods = data.Prods || [];

    if (prods.length === 0) break;

    for (const prod of prods) {
      const text = `${prod.Name} ${prod.Describe || ""}`;
      const ids = extractBsmiIds(text);

      if (ids.length > 0) {
        for (const id of ids) allIds.add(id);
        continue;
      }

      await sleep(1000);
      const pageIds = await extractBsmiFromProductPage(prod.Id);
      for (const id of pageIds) allIds.add(id);
    }

    await sleep(1500);
  }

  console.log(`[pchome] Found ${allIds.size} unique BSMI IDs`);
  return [...allIds];
}

export const syncFromPchome = createSyncFromEc("pchome", scanPchome);
