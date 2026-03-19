import { BROWSER_HEADERS } from "./http.js";
import { BSMI_ID_RE, sleep, createSyncFromEc } from "./utils.js";

const SEARCH_URL =
  "https://search.books.com.tw/search/query/cat/all/sort/5/v/1/spell/3/key/bsmi";

async function searchBooks(page = 1) {
  const url = `${SEARCH_URL}/page/${page}`;
  const res = await fetch(url, {
    headers: { ...BROWSER_HEADERS, Referer: "https://www.books.com.tw/" },
  });

  if (!res.ok) {
    throw new Error(`books.com.tw search returned HTTP ${res.status}`);
  }

  const html = await res.text();

  const ids = new Set();
  for (const m of html.matchAll(BSMI_ID_RE)) {
    ids.add(m[0].toUpperCase());
  }

  const pageNums = [...html.matchAll(/\/page\/(\d+)\//g)].map((m) =>
    Number(m[1]),
  );
  const maxPage = pageNums.length > 0 ? Math.max(...pageNums) : 1;

  return { ids: [...ids], maxPage };
}

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

export const syncFromBooks = createSyncFromEc("books", scanBooks);
