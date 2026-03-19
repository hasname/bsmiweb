import { JSON_HEADERS } from "./http.js";

const SEARCH_URL = "https://aisearch-web.shopping.friday.tw/aisearch";

const BSMI_ID_RE = /[RTDQM]\d{5}/gi;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Search friDay shopping via aisearch API.
 * Keyword is base64 encoded as required by the API.
 */
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

/**
 * Scan friDay search results for BSMI IDs.
 *
 * Note: friDay product detail pages require JS execution and are not
 * directly accessible. We extract BSMI IDs from product names in
 * search results only.
 *
 * @param {object} options
 * @param {number} [options.maxPages=5] - Maximum search pages to scan
 * @returns {Promise<string[]>} Unique BSMI registration IDs
 */
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

/**
 * Scan friDay and upsert any new BSMI registrations found.
 *
 * @param {import('./db.js').default} prisma - Prisma client
 * @param {import('./bsmi.js').fetchBsmi} fetchBsmi - BSMI fetch function
 * @param {object} [options] - Options passed to scanFriday
 * @returns {Promise<string[]>} IDs that were newly imported
 */
export async function syncFromFriday(prisma, fetchBsmi, options) {
  const markIds = await scanFriday(options);
  const imported = [];

  for (const markId of markIds) {
    const existing = await prisma.registration.findUnique({
      where: { id: markId },
    });

    if (existing) continue;

    try {
      const data = await fetchBsmi(markId);
      if (!data) {
        console.log(`[friday] ${markId}: not found on BSMI`);
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
        `[friday] ${markId}: imported (${certificates.length} certs)`,
      );
    } catch (err) {
      console.error(`[friday] ${markId}: failed -`, err.message);
    }

    await sleep(2000);
  }

  console.log(`[friday] Imported ${imported.length} new registrations`);
  return imported;
}
