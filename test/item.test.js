import {
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  jest,
} from "@jest/globals";
import request from "supertest";

const mockFetchBsmi = jest.fn();

jest.unstable_mockModule("../src/bsmi.js", () => ({
  fetchBsmi: mockFetchBsmi,
}));

const { default: app } = await import("../src/index.js");
const { default: prisma } = await import("../src/db.js");

const sampleData = {
  id: "R45879",
  taxId: "82781974",
  applicant: "樂澤國際有限公司",
  contactAddr: "桃園市桃園區中德里桃智路1號10樓",
  companyAddr: "桃園市桃園區中德里桃智路1號10樓",
  phone: "03-3674356#13",
  note: "",
  certificates: [
    {
      id: "CI450068790054",
      validDate: "1130202",
      status: "期限到期失效",
      productName: "20W快充充電器(電源供應器)",
      soldAs: "",
      mainModel: "CH-866",
      seriesModels: "",
      issuer: "經濟部標準檢驗局",
    },
  ],
};

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

const createAuthCertIdIndexSQL =
  "CREATE INDEX IF NOT EXISTS Authorization_certificateId_idx ON Authorization(certificateId)";

const createAuthTaxIdIndexSQL =
  "CREATE INDEX IF NOT EXISTS Authorization_authorizeeTaxId_idx ON Authorization(authorizeeTaxId)";

const createIndexSQL =
  "CREATE INDEX IF NOT EXISTS Certificate_registrationId_idx ON Certificate(registrationId)";

beforeAll(async () => {
  await prisma.$executeRawUnsafe(createTableSQL);
  await prisma.$executeRawUnsafe(createCertTableSQL);
  await prisma.$executeRawUnsafe(createIndexSQL);
  await prisma.$executeRawUnsafe(createAuthTableSQL);
  await prisma.$executeRawUnsafe(createAuthCertIdIndexSQL);
  await prisma.$executeRawUnsafe(createAuthTaxIdIndexSQL);
});

beforeEach(async () => {
  mockFetchBsmi.mockReset();
  await prisma.authorization.deleteMany();
  await prisma.certificate.deleteMany();
  await prisma.registration.deleteMany();
});

describe("GET /bsmi/:id", () => {
  it("should return 200 for valid ID from BSMI", async () => {
    mockFetchBsmi.mockResolvedValue(sampleData);

    const res = await request(app).get("/bsmi/R45879");
    expect(res.status).toBe(200);
    expect(res.text).toContain("R45879");
    expect(res.text).toContain("82781974");
    expect(mockFetchBsmi).toHaveBeenCalledWith("R45879");
  });

  it("should return data from DB on second request", async () => {
    mockFetchBsmi.mockResolvedValue(sampleData);

    await request(app).get("/bsmi/R45879");
    mockFetchBsmi.mockReset();

    const res = await request(app).get("/bsmi/R45879");
    expect(res.status).toBe(200);
    expect(res.text).toContain("R45879");
    expect(mockFetchBsmi).not.toHaveBeenCalled();
  });

  it("should return 404 for invalid ID format", async () => {
    const res = await request(app).get("/bsmi/INVALID");
    expect(res.status).toBe(404);
  });

  it("should return 404 for ID not found on BSMI", async () => {
    mockFetchBsmi.mockResolvedValue(null);

    const res = await request(app).get("/bsmi/R00000");
    expect(res.status).toBe(404);
  });

  it("should accept lowercase ID", async () => {
    mockFetchBsmi.mockResolvedValue(sampleData);

    const res = await request(app).get("/bsmi/r45879");
    expect(res.status).toBe(200);
    expect(mockFetchBsmi).toHaveBeenCalledWith("R45879");
  });

  it("should display authorization data when available", async () => {
    mockFetchBsmi.mockResolvedValue(sampleData);

    await prisma.authorization.create({
      data: {
        id: "CI450078790054",
        certificateId: "CI450068790054",
        authorizerName: "樂澤國際有限公司",
        mainModel: "CH-866",
        authorizeeTaxId: "12345678",
        authorizeeName: "測試公司",
        authorizeeAddr: "台北市",
        authorizeePhone: "02-12345678",
        validDate: "2024/01/01",
      },
    });

    const res = await request(app).get("/bsmi/R45879");
    expect(res.status).toBe(200);
    expect(res.text).toContain("授權資料");
    expect(res.text).toContain("CI450078790054");
    expect(res.text).toContain("測試公司");
  });
});
