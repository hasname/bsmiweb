export const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

export const BSMI_ID_RE = /[RTDQM]\d{5}/gi;

export const BSMI_CONTEXT_RE =
  /(?:BSMI|bsmi|檢驗|認證|標檢局|登錄字號|商檢字號)[^\n]{0,30}([RTDQM]\d{5})/gi;

export function extractBsmiIds(text) {
  const ids = new Set();
  for (const m of text.matchAll(BSMI_ID_RE)) {
    ids.add(m[0].toUpperCase());
  }
  return [...ids];
}

export function extractBsmiIdsWithContext(html) {
  const ids = new Set();
  for (const m of html.matchAll(BSMI_CONTEXT_RE)) {
    ids.add(m[1].toUpperCase());
  }
  return [...ids];
}

export async function upsertRegistration(prisma, data) {
  const { certificates, ...vendor } = data;
  return await prisma.$transaction(async (tx) => {
    await tx.certificate.deleteMany({ where: { registrationId: vendor.id } });
    return await tx.registration.upsert({
      where: { id: vendor.id },
      create: { ...vendor, certificates: { create: certificates } },
      update: { ...vendor, certificates: { create: certificates } },
      include: { certificates: true },
    });
  });
}

/**
 * Given a scan function and a label, create a sync function that:
 * 1. Calls scanFn to get BSMI IDs
 * 2. Skips IDs already in DB
 * 3. Fetches from BSMI and upserts new ones
 */
export function createSyncFromEc(label, scanFn) {
  return async function syncFromEc(prisma, fetchBsmi, options) {
    const markIds = await scanFn(options);
    const imported = [];

    for (const markId of markIds) {
      const existing = await prisma.registration.findUnique({
        where: { id: markId },
      });

      if (existing) continue;

      try {
        const data = await fetchBsmi(markId);
        if (!data) {
          console.log(`[${label}] ${markId}: not found on BSMI`);
          continue;
        }

        await upsertRegistration(prisma, data);
        imported.push(markId);
        console.log(
          `[${label}] ${markId}: imported (${data.certificates.length} certs)`,
        );
      } catch (err) {
        console.error(`[${label}] ${markId}: failed -`, err.message);
      }

      await sleep(2000);
    }

    console.log(`[${label}] Imported ${imported.length} new registrations`);
    return imported;
  };
}

export function extractTag(row, tag) {
  const re = new RegExp(`<${tag}>([\\s\\S]*?)</${tag}>`);
  const m = row.match(re);
  return m ? m[1].trim() : "";
}
