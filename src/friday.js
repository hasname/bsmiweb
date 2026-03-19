import { JSON_HEADERS } from "./http.js";
import { BSMI_ID_RE, sleep, createSyncFromEc } from "./utils.js";

const SEARCH_URL = "https://aisearch-web.shopping.friday.tw/aisearch";

async function searchFriday(page = 1, size = 40) {
  const keyword = Buffer.from("bsmi").toString("base64");

  const res = await fetch(SEARCH_URL, {
    method: "POST",
    headers: {
      ...JSON_HEADERS,
      "Content-Type": "application/json",
      Origin: "https://ec-w.shopping.friday.tw",
      Referer: "https://ec-w.shopping.friday.tw/",
    },
    body: JSON.stringify({
      remote: "w",
      sorting: "RELEVANT",
      page,
      size,
      kws_phrase64: keyword,
    }),
  });

  if (!res.ok) {
    throw new Error(`friDay search returned HTTP ${res.status}`);
  }

  const data = await res.json();
  const result = data[0] || {};

  return {
    products: (result.results || []).map((r) => ({
      pid: r.pid,
      name: r.prd_name || "",
    })),
    totalCount: result.all_cnts || 0,
    pageSize: result.page_size || size,
  };
}

export async function scanFriday({ maxPages = 5 } = {}) {
  const allIds = new Set();
  const pageSize = 40;
  let totalPages = 1;

  for (let page = 1; page <= Math.min(maxPages, totalPages); page++) {
    console.log(`[friday] Searching page ${page}...`);

    const data = await searchFriday(page, pageSize);
    totalPages = Math.ceil(data.totalCount / pageSize);

    if (data.products.length === 0) break;

    for (const prod of data.products) {
      for (const m of prod.name.matchAll(BSMI_ID_RE)) {
        allIds.add(m[0].toUpperCase());
      }
    }

    await sleep(1500);
  }

  console.log(`[friday] Found ${allIds.size} unique BSMI IDs`);
  return [...allIds];
}

export const syncFromFriday = createSyncFromEc("friday", scanFriday);
