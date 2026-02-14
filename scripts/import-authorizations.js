import prisma from "../src/db.js";

const XML_URL =
  "https://data.bsmi.gov.tw/opendata/download/313000000G-000129-001.action";

function extractTag(row, tag) {
  const re = new RegExp(`<${tag}>([\\s\\S]*?)</${tag}>`);
  const m = row.match(re);
  return m ? m[1].trim() : "";
}

async function main() {
  console.log("Downloading authorization XML...");

  const res = await fetch(XML_URL, {
    headers: { "User-Agent": "bsmiweb/1.0" },
    // Government site uses self-signed cert
  });

  if (!res.ok) {
    throw new Error(`HTTP ${res.status}`);
  }

  const xml = await res.text();
  console.log(`Downloaded ${(xml.length / 1024 / 1024).toFixed(1)} MB`);

  const rows = xml.match(/<row>[\s\S]*?<\/row>/g) || [];
  console.log(`Found ${rows.length} authorization records`);

  const records = rows.map((row) => ({
    id: extractTag(row, "授權證號"),
    certificateId: extractTag(row, "證書編號"),
    authorizerName: extractTag(row, "授權人名稱"),
    mainModel: extractTag(row, "主型式"),
    authorizeeTaxId: extractTag(row, "被授權人統編"),
    authorizeeName: extractTag(row, "被授權人名稱"),
    authorizeeAddr: extractTag(row, "被授權人地址"),
    authorizeePhone: extractTag(row, "被授權人電話"),
    validDate: extractTag(row, "授權有效時間"),
  }));

  // Filter out records without an id
  const valid = records.filter((r) => r.id);
  console.log(`Importing ${valid.length} valid records...`);

  // Batch import: delete all then create in batches
  await prisma.$transaction(async (tx) => {
    await tx.authorization.deleteMany();

    const BATCH_SIZE = 1000;
    for (let i = 0; i < valid.length; i += BATCH_SIZE) {
      const batch = valid.slice(i, i + BATCH_SIZE);
      await tx.authorization.createMany({ data: batch });
      console.log(`  Imported ${Math.min(i + BATCH_SIZE, valid.length)} / ${valid.length}`);
    }
  });

  console.log("Done.");
}

main()
  .catch((err) => {
    console.error(err);
    process.exit(1);
  })
  .finally(() => prisma.$disconnect());
