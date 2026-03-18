// data.bsmi.gov.tw uses a self-signed certificate
process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0";

import prisma from "../src/db.js";

const isProduction = process.env.NODE_ENV === "production";

const DATASETS = [
  {
    name: "電子類",
    url: "https://data.bsmi.gov.tw/opendata/download/313000000G-000116-001.action",
  },
  {
    name: "其他類",
    url: "https://data.bsmi.gov.tw/opendata/download/313000000G-000092-001.action",
  },
];

function extractTag(row, tag) {
  const re = new RegExp(`<${tag}>([\\s\\S]*?)</${tag}>`);
  const m = row.match(re);
  return m ? m[1].trim() : "";
}

// Convert western date "2024/06/10" to ROC date "1130610"
function toRocDate(western) {
  if (!western) return "";
  const parts = western.split("/");
  if (parts.length !== 3) return western;
  const year = parseInt(parts[0], 10) - 1911;
  if (isNaN(year) || year < 0) return western;
  return `${String(year).padStart(3, "0")}${parts[1]}${parts[2]}`;
}

function parseRows(xml) {
  const rows = xml.match(/<row>[\s\S]*?<\/row>/g) || [];

  return rows
    .map((row) => ({
      id: extractTag(row, "證書號碼"),
      validDate: toRocDate(extractTag(row, "證書期限")),
      status: extractTag(row, "證書狀態"),
      productName: extractTag(row, "商品名稱"),
      mainModel: extractTag(row, "主型式"),
    }))
    .filter((r) => r.id);
}

async function fetchXml(url) {
  const res = await fetch(url, {
    headers: { "User-Agent": "bsmiweb/1.0" },
  });

  if (!res.ok) {
    throw new Error(`HTTP ${res.status}`);
  }

  return res.text();
}

function esc(value) {
  if (value == null) return "''";
  return "'" + String(value).replace(/\\/g, "\\\\").replace(/'/g, "\\'") + "'";
}

export async function importCertificates(db = prisma) {
  const allRecords = [];

  for (const dataset of DATASETS) {
    console.log(`Downloading ${dataset.name} XML...`);
    const xml = await fetchXml(dataset.url);
    console.log(`  Downloaded ${(xml.length / 1024 / 1024).toFixed(1)} MB`);

    const records = parseRows(xml);
    console.log(`  Parsed ${records.length} records`);
    allRecords.push(...records);
  }

  // Deduplicate by certificate id (keep first occurrence)
  const seen = new Set();
  const unique = allRecords.filter((r) => {
    if (seen.has(r.id)) return false;
    seen.add(r.id);
    return true;
  });
  console.log(`Total unique records: ${unique.length}`);
  console.log("Importing...");

  // Insert/update in batches using raw SQL to bypass FK constraint on
  // registrationId.  The XML data does not include registrationId; it is set
  // to empty string and will be populated when a user views the corresponding
  // registration page.  The upsert keeps registrationId unchanged if the row
  // already exists (so scraper-populated values are preserved).
  const BATCH_SIZE = 1000;
  const now = new Date().toISOString().slice(0, 19).replace("T", " ");
  const prod = isProduction;

  const upsertSql = prod
    ? (values) =>
        `INSERT INTO certificate (id, registration_id, valid_date, status, product_name, sold_as, main_model, series_models, issuer, created_at, updated_at) VALUES ${values} ON DUPLICATE KEY UPDATE valid_date=VALUES(valid_date), status=VALUES(status), product_name=VALUES(product_name), main_model=VALUES(main_model), updated_at=VALUES(updated_at)`
    : (values) =>
        `INSERT INTO certificate (id, registration_id, valid_date, status, product_name, sold_as, main_model, series_models, issuer, created_at, updated_at) VALUES ${values} ON CONFLICT(id) DO UPDATE SET valid_date=excluded.valid_date, status=excluded.status, product_name=excluded.product_name, main_model=excluded.main_model, updated_at=excluded.updated_at`;

  // Disable FK checks for bulk import
  if (prod) {
    await db.$executeRawUnsafe("SET FOREIGN_KEY_CHECKS=0");
  } else {
    await db.$executeRawUnsafe("PRAGMA foreign_keys=OFF");
  }

  try {
    for (let i = 0; i < unique.length; i += BATCH_SIZE) {
      const batch = unique.slice(i, i + BATCH_SIZE);
      const values = batch
        .map(
          (r) =>
            `(${esc(r.id)}, '', ${esc(r.validDate)}, ${esc(r.status)}, ${esc(r.productName)}, '', ${esc(r.mainModel)}, '', '', '${now}', '${now}')`,
        )
        .join(",\n");

      await db.$executeRawUnsafe(upsertSql(values));
      console.log(
        `  Imported ${Math.min(i + BATCH_SIZE, unique.length)} / ${unique.length}`,
      );
    }
  } finally {
    if (prod) {
      await db.$executeRawUnsafe("SET FOREIGN_KEY_CHECKS=1");
    } else {
      await db.$executeRawUnsafe("PRAGMA foreign_keys=ON");
    }
  }

  console.log("Done.");
}

// Run as standalone script
if (import.meta.url === `file://${process.argv[1]}`) {
  importCertificates()
    .catch((err) => {
      console.error(err);
      process.exit(1);
    })
    .finally(() => prisma.$disconnect());
}
