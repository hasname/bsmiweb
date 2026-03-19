const SEARCH_URL = "https://ecshweb.pchome.com.tw/search/v4.3/all/results";
const PRODUCT_URL = "https://24h.pchome.com.tw/prod/";

const BSMI_ID_RE = /[RTDQM]\d{5}/gi;
// Stricter: BSMI ID near a BSMI-related keyword
const BSMI_CONTEXT_RE =
  /(?:BSMI|bsmi|檢驗|認證|標檢局|登錄字號)[^\n]{0,30}([RTDQM]\d{5})/gi;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Search PChome for products matching a query.
 * Returns product list with Id, Name, Describe.
 */
async function searchPchome(query, page = 1) {
  const url = `${SEARCH_URL}?q=${encodeURIComponent(query)}&page=${page}&sort=new/dc`;
  const res = await fetch(url, {
    headers: { "User-Agent": "bsmiweb/1.0" },
  });

  if (!res.ok) {
    throw new Error(`PChome search returned HTTP ${res.status}`);
  }

  return res.json();
}

/**
 * Extract BSMI IDs from text (Name + Describe).
 * Returns deduplicated uppercase IDs.
 */
function extractBsmiIds(text) {
  const ids = new Set();
  for (const m of text.matchAll(BSMI_ID_RE)) {
    ids.add(m[0].toUpperCase());
  }
  return [...ids];
}

/**
 * Fetch a PChome product page and extract BSMI IDs
 * using contextual matching (near BSMI-related keywords).
 */
async function extractBsmiFromProductPage(productId) {
  const res = await fetch(`${PRODUCT_URL}${productId}`, {
    headers: { "User-Agent": "bsmiweb/1.0" },
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
 * Scan PChome search results for BSMI IDs.
 *
 * Strategy:
 * 1. Search PChome for the query (default "bsmi")
 * 2. For each product, try to extract BSMI IDs from Name + Describe
 * 3. For products without IDs in search results, fetch the product page
 * 4. Return all unique BSMI IDs found
 *
 * @param {object} options
 * @param {string} [options.query="bsmi"] - Search query
 * @param {number} [options.maxPages=5] - Maximum search pages to scan
 * @returns {Promise<string[]>} Unique BSMI registration IDs
 */
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
      // Try extracting from search result text first
      const text = `${prod.Name} ${prod.Describe || ""}`;
      const ids = extractBsmiIds(text);

      if (ids.length > 0) {
        for (const id of ids) allIds.add(id);
        continue;
      }

      // Fall back to product page scraping
      await sleep(1000);
      const pageIds = await extractBsmiFromProductPage(prod.Id);
      for (const id of pageIds) allIds.add(id);
    }

    await sleep(1500);
  }

  console.log(`[pchome] Found ${allIds.size} unique BSMI IDs`);
  return [...allIds];
}

/**
 * Scan PChome and upsert any new BSMI registrations found.
 *
 * @param {import('./db.js').default} prisma - Prisma client
 * @param {import('./bsmi.js').fetchBsmi} fetchBsmi - BSMI fetch function
 * @param {object} [options] - Options passed to scanPchome
 * @returns {Promise<string[]>} IDs that were newly imported
 */
export async function syncFromPchome(prisma, fetchBsmi, options) {
  const markIds = await scanPchome(options);
  const imported = [];

  for (const markId of markIds) {
    // Check if already in DB
    const existing = await prisma.registration.findUnique({
      where: { id: markId },
    });

    if (existing) continue;

    // Fetch from BSMI and upsert
    try {
      const data = await fetchBsmi(markId);
      if (!data) {
        console.log(`[pchome] ${markId}: not found on BSMI`);
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
        `[pchome] ${markId}: imported (${certificates.length} certs)`,
      );
    } catch (err) {
      console.error(`[pchome] ${markId}: failed -`, err.message);
    }

    await sleep(2000);
  }

  console.log(`[pchome] Imported ${imported.length} new registrations`);
  return imported;
}
