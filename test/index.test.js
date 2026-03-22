import { beforeAll, beforeEach, describe, expect, it } from "@jest/globals";
import request from "supertest";
import app from "../src/index.js";
import prisma from "../src/db.js";

const createTableSQL = `
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
  )`;

const createCertTableSQL = `
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
  )`;

const createAuthTableSQL = `
  CREATE TABLE IF NOT EXISTS authorization (
    id TEXT PRIMARY KEY,
    certificate_id TEXT NOT NULL,
    authorizer_name TEXT NOT NULL,
    main_model TEXT NOT NULL,
    authorizee_tax_id TEXT NOT NULL,
    authorizee_name TEXT NOT NULL,
    authorizee_addr TEXT NOT NULL,
    authorizee_phone TEXT NOT NULL,
    valid_date TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  )`;

const createIndexSQL =
  "CREATE INDEX IF NOT EXISTS certificate_registration_id_idx ON certificate(registration_id)";

const createAuthCertIdIndexSQL =
  "CREATE INDEX IF NOT EXISTS authorization_certificate_id_idx ON authorization(certificate_id)";

const createAuthTaxIdIndexSQL =
  "CREATE INDEX IF NOT EXISTS authorization_authorizee_tax_id_idx ON authorization(authorizee_tax_id)";

beforeAll(async () => {
  await prisma.$executeRawUnsafe(createTableSQL);
  await prisma.$executeRawUnsafe(createCertTableSQL);
  await prisma.$executeRawUnsafe(createIndexSQL);
  await prisma.$executeRawUnsafe(createAuthTableSQL);
  await prisma.$executeRawUnsafe(createAuthCertIdIndexSQL);
  await prisma.$executeRawUnsafe(createAuthTaxIdIndexSQL);
});

beforeEach(async () => {
  await prisma.authorization.deleteMany();
  await prisma.certificate.deleteMany();
  await prisma.registration.deleteMany();
});

describe("GET /", () => {
  it("should return 200 and show search form", async () => {
    const res = await request(app).get("/");
    expect(res.status).toBe(200);
    expect(res.text).toContain("搜尋");
    expect(res.text).toContain("p.hasname.com/js/pa-43fhI07VwO-gqJCOrcpFU.js");
  });

  it("should show current data counts on the homepage", async () => {
    await prisma.registration.create({
      data: {
        id: "R45879",
        taxId: "82781974",
        applicant: "樂澤國際有限公司",
        contactAddr: "桃園市桃園區",
        companyAddr: "桃園市桃園區",
        phone: "03-3674356",
        note: "",
      },
    });

    const res = await request(app).get("/");
    expect(res.status).toBe(200);
    expect(res.text).toContain("目前站上共有 1 筆檢驗標識、0 張證書、0 筆授權資料。");
  });

  it("should return search results for matching registration", async () => {
    await prisma.registration.create({
      data: {
        id: "R45879",
        taxId: "82781974",
        applicant: "樂澤國際有限公司",
        contactAddr: "桃園市桃園區",
        companyAddr: "桃園市桃園區",
        phone: "03-3674356",
        note: "",
      },
    });

    const res = await request(app).get("/?q=樂澤");
    expect(res.status).toBe(200);
    expect(res.text).toContain("R45879");
    expect(res.text).toContain("樂澤國際有限公司");
  });

  it("should show no results message for non-matching query", async () => {
    const res = await request(app).get("/?q=nonexistent_keyword_xyz");
    expect(res.status).toBe(200);
    expect(res.text).toContain("無搜尋結果");
  });
});

describe("GET /atom.xml", () => {
  it("should return Atom feed with recent registrations", async () => {
    await prisma.registration.create({
      data: {
        id: "R45879",
        taxId: "82781974",
        applicant: "樂澤國際有限公司",
        contactAddr: "桃園市桃園區",
        companyAddr: "桃園市桃園區",
        phone: "03-3674356",
        note: "",
      },
    });

    const res = await request(app).get("/atom.xml");
    expect(res.status).toBe(200);
    expect(res.headers["content-type"]).toContain("application/atom+xml");
    expect(res.text).toContain("<feed xmlns=\"http://www.w3.org/2005/Atom\">");
    expect(res.text).toContain("BSMI 檢驗標識更新");
    expect(res.text).toContain("R45879 — 樂澤國際有限公司");
    expect(res.text).toContain("/bsmi/R45879");
  });
});

describe("GET /nonexistent", () => {
  it("should return 404", async () => {
    const res = await request(app).get("/nonexistent");
    expect(res.status).toBe(404);
  });
});
