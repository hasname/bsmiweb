// data.bsmi.gov.tw uses a self-signed certificate
process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0";

import schedule from "node-schedule";

import prisma from "./db.js";
import { fetchBsmi, syncRecentChanges } from "./bsmi.js";
import { syncFromMomo } from "./momo.js";
import { syncFromPchome } from "./pchome.js";
import { importCertificates } from "../scripts/import-certificates.js";
import { importAuthorizations } from "../scripts/import-authorizations.js";

let running = false;

async function runJob(name, fn) {
  if (running) {
    console.log(`[cron] Skipping ${name}: previous job still running`);
    return;
  }
  running = true;
  const start = Date.now();
  console.log(`[cron] Starting ${name}`);
  try {
    await fn();
    console.log(`[cron] Finished ${name} in ${((Date.now() - start) / 1000).toFixed(1)}s`);
  } catch (err) {
    console.error(`[cron] ${name} failed:`, err.message);
  } finally {
    running = false;
  }
}

// Daily at 03:00 — import certificates and authorizations XML from open data
schedule.scheduleJob("0 3 * * *", () => {
  runJob("importCertificates+Authorizations", async () => {
    await importCertificates(prisma);
    await importAuthorizations(prisma);
  });
});

// Daily at 04:00 — sync recently changed registrations (past 7 days)
schedule.scheduleJob("0 4 * * *", () => {
  runJob("syncRecentChanges", () => syncRecentChanges(prisma, 7));
});

// Daily at 05:00 — scan PChome for new BSMI registrations
schedule.scheduleJob("0 5 * * *", () => {
  runJob("syncFromPchome", () => syncFromPchome(prisma, fetchBsmi));
});

// Daily at 06:00 — scan momo for new BSMI registrations
schedule.scheduleJob("0 6 * * *", () => {
  runJob("syncFromMomo", () => syncFromMomo(prisma, fetchBsmi));
});

console.log("[cron] Scheduled jobs:");
console.log("  - importCertificates+Authorizations: daily at 03:00");
console.log("  - syncRecentChanges:                 daily at 04:00");
console.log("  - syncFromPchome:                    daily at 05:00");
console.log("  - syncFromMomo:                      daily at 06:00");
