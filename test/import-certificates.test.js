import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  jest,
} from "@jest/globals";

import { importCertificates } from "../scripts/import-certificates.js";
import prisma from "../src/db.js";

const NEW_ISSUE_URL =
  "https://data.bsmi.gov.tw/opendata/download/313000000G-000130-001.action";

const xmlByUrl = {
  "https://data.bsmi.gov.tw/opendata/download/313000000G-000116-001.action": `<?xml version="1.0" encoding="UTF-8"?>
<bsmi>
  <row>
    <證書號碼>CI111111111111</證書號碼>
    <證書期限>2027/05/20</證書期限>
    <商品名稱>電子類產品</商品名稱>
    <主型式>E1</主型式>
    <證書狀態>認可</證書狀態>
  </row>
  <row>
    <證書號碼>CI000000000000</證書號碼>
    <證書期限>2027/01/01</證書期限>
    <商品名稱>電子類重疊</商品名稱>
    <主型式>OLD</主型式>
    <證書狀態>認可</證書狀態>
  </row>
</bsmi>`,
  "https://data.bsmi.gov.tw/opendata/download/313000000G-000092-001.action": `<?xml version="1.0" encoding="UTF-8"?>
<bsmi>
  <row>
    <證書號碼>CI222222222222</證書號碼>
    <證書期限>2026/12/31</證書期限>
    <商品名稱>其他類產品</商品名稱>
    <主型式>O1</主型式>
    <證書狀態>認可</證書狀態>
  </row>
</bsmi>`,
  [NEW_ISSUE_URL]: `<?xml version="1.0" encoding="UTF-8"?>
<bsmi>
  <row>
    <廠商識別號>13077</廠商識別號>
    <證書號碼>CI333333333333</證書號碼>
    <申請人>新發申請人</申請人>
    <證書期限>2028/04/06</證書期限>
    <商品名稱>新發產品</商品名稱>
    <主型式>N1</主型式>
    <證書狀態>認可</證書狀態>
  </row>
  <row>
    <廠商識別號>13082</廠商識別號>
    <證書號碼>CI000000000000</證書號碼>
    <申請人>重疊申請人</申請人>
    <證書期限>2029/01/01</證書期限>
    <商品名稱>新發重疊</商品名稱>
    <主型式>NEW</主型式>
    <證書狀態>期限到期失效</證書狀態>
  </row>
</bsmi>`,
};

const originalFetch = global.fetch;

beforeAll(async () => {
  await prisma.$executeRawUnsafe(`
    CREATE TABLE IF NOT EXISTS registration (
      id TEXT PRIMARY KEY,
      tax_id TEXT NOT NULL,
      applicant TEXT NOT NULL,
      contact_addr TEXT NOT NULL,
      company_addr TEXT NOT NULL,
      phone TEXT NOT NULL,
      note TEXT NOT NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
    )`);
  await prisma.$executeRawUnsafe(`
    CREATE TABLE IF NOT EXISTS certificate (
      id TEXT PRIMARY KEY,
      registration_id TEXT NOT NULL,
      valid_date TEXT NOT NULL,
      status TEXT NOT NULL,
      product_name TEXT NOT NULL,
      sold_as TEXT NOT NULL,
      main_model TEXT NOT NULL,
      series_models TEXT NOT NULL,
      issuer TEXT NOT NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      FOREIGN KEY (registration_id) REFERENCES registration(id)
    )`);
});

beforeEach(async () => {
  await prisma.certificate.deleteMany();
  await prisma.registration.deleteMany();
  global.fetch = jest.fn(async (url) => {
    const xml = xmlByUrl[url];
    if (!xml) {
      return { ok: false, status: 404, text: async () => "" };
    }
    return { ok: true, status: 200, text: async () => xml };
  });
});

afterEach(() => {
  global.fetch = originalFetch;
});

describe("importCertificates", () => {
  it("should import the current-year newly issued dataset", async () => {
    await importCertificates(prisma);

    expect(global.fetch).toHaveBeenCalledWith(
      NEW_ISSUE_URL,
      expect.objectContaining({
        headers: { "User-Agent": "bsmiweb/1.0" },
      }),
    );

    const certs = await prisma.certificate.findMany({
      orderBy: { id: "asc" },
    });
    expect(certs.map((c) => c.id)).toEqual([
      "CI000000000000",
      "CI111111111111",
      "CI222222222222",
      "CI333333333333",
    ]);

    const newlyIssued = certs.find((c) => c.id === "CI333333333333");
    expect(newlyIssued).toMatchObject({
      validDate: "1170406",
      status: "認可",
      productName: "新發產品",
      mainModel: "N1",
      registrationId: "",
    });

    // Overlap keeps 電子類 (first dataset), not 當年度新發.
    const overlap = certs.find((c) => c.id === "CI000000000000");
    expect(overlap).toMatchObject({
      productName: "電子類重疊",
      mainModel: "OLD",
      status: "認可",
      validDate: "1160101",
    });
  });
});
