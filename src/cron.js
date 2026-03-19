// data.bsmi.gov.tw uses a self-signed certificate
process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0";

import schedule from "node-schedule";

import prisma from "./db.js";
import { fetchBsmi, syncRecentChanges } from "./bsmi.js";
import { syncFromBooks } from "./books.js";
import { syncFromFriday } from "./friday.js";
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

const jobs = [
  {
    cron: "0 3 * * *",
    name: "importCertificates+Authorizations",
    fn: async () => {
      await importCertificates(prisma);
      await importAuthorizations(prisma);
    },
  },
  { cron: "0 4 * * *", name: "syncRecentChanges", fn: () => syncRecentChanges(prisma, 7) },
  { cron: "0 5 * * *", name: "syncFromPchome", fn: () => syncFromPchome(prisma, fetchBsmi) },
  { cron: "0 6 * * *", name: "syncFromMomo", fn: () => syncFromMomo(prisma, fetchBsmi) },
  { cron: "0 7 * * *", name: "syncFromBooks", fn: () => syncFromBooks(prisma, fetchBsmi) },
  { cron: "0 8 * * *", name: "syncFromFriday", fn: () => syncFromFriday(prisma, fetchBsmi) },
];

for (const job of jobs) {
  schedule.scheduleJob(job.cron, () => runJob(job.name, job.fn));
}

console.log("[cron] Scheduled jobs:");
for (const job of jobs) {
  console.log(`  - ${job.name}: ${job.cron}`);
}
