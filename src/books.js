import { BROWSER_HEADERS } from "./http.js";

const SEARCH_URL =
  "https://search.books.com.tw/search/query/cat/all/sort/5/v/1/spell/3/key/bsmi";

const BSMI_ID_RE = /[RTDQM]\d{5}/gi;

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Fetch a books.com.tw search page and extract product titles/descriptions.
 * Returns unique BSMI IDs found on the page and the max page number.
 */
async function searchBooks(page = 1) {
  const url = `${SEARCH_URL}/page/${page}`;
  const res = await fetch(url, {
    headers: { ...BROWSER_HEADERS, Referer: "https://www.books.com.tw/" },
  });

  if (!res.ok) {
    throw new Error(`books.com.tw search returned HTTP ${res.status}`);
  }

  const html = await res.text();

  // Extract BSMI IDs from the entire page
  // (titles, descriptions, and any visible text)
  const ids = new Set();
  for (const m of html.matchAll(BSMI_ID_RE)) {
    ids.add(m[0].toUpperCase());
  }

  // Detect max page from pagination links
  const pageNums = [...html.matchAll(/\/page\/(\d+)\//g)].map((m) =>
    Number(m[1]),
  );
  const maxPage = pageNums.length > 0 ? Math.max(...pageNums) : 1;

  return { ids: [...ids], maxPage };
}

/**
 * Scan books.com.tw search results for BSMI IDs.
 *
 * Note: books.com.tw product detail pages are behind Cloudflare protection,
 * so we can only extract IDs visible in search result titles/descriptions.
 * Yield is low (~1-2%) but still useful for discovery.
 *
 * @param {object} options
 * @param {number} [options.maxPages=6] - Maximum search pages to scan
 * @returns {Promise<string[]>} Unique BSMI registration IDs
 */
export async function scanBooks({ maxPages = 6 } = {}) {
  const allIds = new Set();
  let maxPage = 1;

  for (let page = 1; page <= Math.min(maxPages, maxPage); page++) {
    console.log(`[books] Searching page ${page}...`);

    const data = await searchBooks(page);
    maxPage = data.maxPage;

    for (const id of data.ids) allIds.add(id);

    await sleep(1500);
  }

  console.log(`[books] Found ${allIds.size} unique BSMI IDs`);
  return [...allIds];
}

/**
 * Scan books.com.tw and upsert any new BSMI registrations found.
 *
 * @param {import('./db.js').default} prisma - Prisma client
 * @param {import('./bsmi.js').fetchBsmi} fetchBsmi - BSMI fetch function
 * @param {object} [options] - Options passed to scanBooks
 * @returns {Promise<string[]>} IDs that were newly imported
 */
export async function syncFromBooks(prisma, fetchBsmi, options) {
  const markIds = await scanBooks(options);
  const imported = [];

  for (const markId of markIds) {
    const existing = await prisma.registration.findUnique({
      where: { id: markId },
    });

    if (existing) continue;

    try {
      const data = await fetchBsmi(markId);
      if (!data) {
        console.log(`[books] ${markId}: not found on BSMI`);
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
        `[books] ${markId}: imported (${certificates.length} certs)`,
      );
    } catch (err) {
      console.error(`[books] ${markId}: failed -`, err.message);
    }

    await sleep(2000);
  }

  console.log(`[books] Imported ${imported.length} new registrations`);
  return imported;
}
