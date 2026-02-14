import { beforeAll, beforeEach, describe, expect, it } from "@jest/globals";
import request from "supertest";
import app from "../src/index.js";
import prisma from "../src/db.js";

const createTableSQL = `
  CREATE TABLE IF NOT EXISTS Registration (
    id TEXT PRIMARY KEY,
    taxId TEXT NOT NULL,
    applicant TEXT NOT NULL,
    contactAddr TEXT NOT NULL,
    companyAddr TEXT NOT NULL,
    phone TEXT NOT NULL,
    note TEXT NOT NULL,
    createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updatedAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  )`;

const createCertTableSQL = `
  CREATE TABLE IF NOT EXISTS Certificate (
    id TEXT PRIMARY KEY,
    registrationId TEXT NOT NULL,
    validDate TEXT NOT NULL,
    status TEXT NOT NULL,
    productName TEXT NOT NULL,
    soldAs TEXT NOT NULL,
    mainModel TEXT NOT NULL,
    seriesModels TEXT NOT NULL,
    issuer TEXT NOT NULL,
    createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updatedAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (registrationId) REFERENCES Registration(id)
  )`;

const createAuthTableSQL = `
  CREATE TABLE IF NOT EXISTS Authorization (
    id TEXT PRIMARY KEY,
    certificateId TEXT NOT NULL,
    authorizerName TEXT NOT NULL,
    mainModel TEXT NOT NULL,
    authorizeeTaxId TEXT NOT NULL,
    authorizeeName TEXT NOT NULL,
    authorizeeAddr TEXT NOT NULL,
    authorizeePhone TEXT NOT NULL,
    validDate TEXT NOT NULL,
    createdAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updatedAt DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
  )`;

const createIndexSQL =
  "CREATE INDEX IF NOT EXISTS Certificate_registrationId_idx ON Certificate(registrationId)";

const createAuthCertIdIndexSQL =
  "CREATE INDEX IF NOT EXISTS Authorization_certificateId_idx ON Authorization(certificateId)";

const createAuthTaxIdIndexSQL =
  "CREATE INDEX IF NOT EXISTS Authorization_authorizeeTaxId_idx ON Authorization(authorizeeTaxId)";

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

describe("GET /nonexistent", () => {
  it("should return 404", async () => {
    const res = await request(app).get("/nonexistent");
    expect(res.status).toBe(404);
  });
});
