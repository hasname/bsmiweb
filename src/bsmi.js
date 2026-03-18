const BSMI_URL = "https://civil.bsmi.gov.tw/bsmi_pqn/pqn/uqi5102f.do";
const CERT_URL = "https://civil.bsmi.gov.tw/bsmi_pqn/pqn/uqi2102f.do";

function parseVendor(html) {
  const get = (label) => {
    const re = new RegExp(
      label +
        "[：:]\\s*</(?:p|label)>\\s*(?:</div>\\s*)?<div[^>]*>\\s*<p>([\\s\\S]*?)</p>",
    );
    const m = html.match(re);
    return m ? m[1].trim() : "";
  };

  return {
    taxId: get("統編"),
    applicant: get("申請人"),
    contactAddr: get("聯絡地址"),
    companyAddr: get("公司地址\\s*"),
    phone: get("電話"),
    note: get("廠商資料備註"),
  };
}

function parseCertificates(html) {
  const certs = [];

  // Find the certificate list section
  const listStart = html.indexOf("panel-heading \">證書資料");
  if (listStart === -1) return certs;
  const listSection = html.slice(listStart);

  // Split by the row divs
  const rows = listSection.split(/\n\s*<div\s+class='row'>/);

  for (let i = 1; i < rows.length; i++) {
    const row = rows[i];

    // Extract the 5 col divs (after the NO. col)
    const colRe =
      /<div class="col-xs-6 col-sm-\d+ col-md-\d+ col-lg-\d+">\s*([\s\S]*?)\s*<\/div>/g;
    const cols = [];
    let m;
    while ((m = colRe.exec(row)) !== null) {
      cols.push(m[1].trim());
    }

    if (cols.length < 4) continue;

    // Column 0: 證書編號<br>有效期間<br>狀態
    const certParts = cols[0].split(/<br\s*\/?>/);
    const certId = (certParts[0] || "").trim();
    const validDate = (certParts[1] || "").trim();
    const status = (certParts[2] || "").trim();

    // Column 1: 產品中文名稱
    const productName = cols[1].replace(/<[^>]*>/g, "").trim();

    // Column 2: 以他人名義銷售
    const soldAs = cols[2].replace(/<[^>]*>/g, "").trim();

    // Column 3: 型式(主型式/系列型號)
    const modelHtml = cols[3];
    const mainModelMatch = modelHtml.match(/<strong>\s*(.*?)\s*<\/strong>/);
    const mainModel = mainModelMatch ? mainModelMatch[1].trim() : "";

    // Series models: everything after <strong>...</strong><br> that isn't empty
    const afterStrong = modelHtml.replace(/<strong>[\s\S]*?<\/strong>/, "");
    const seriesModels = afterStrong
      .split(/<br\s*\/?>/)
      .map((s) => s.replace(/<[^>]*>/g, "").trim())
      .filter((s) => s.length > 0)
      .join("\n");

    // Column 4: 發證單位
    const issuer = cols[4]
      ? cols[4].replace(/<[^>]*>/g, "").trim()
      : "";

    if (!certId) continue;

    certs.push({
      id: certId,
      validDate,
      status,
      productName,
      soldAs,
      mainModel,
      seriesModels,
      issuer,
    });
  }

  return certs;
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Convert a Date to ROC date string "YYY/MM/DD"
function toRocDate(date) {
  const y = date.getFullYear() - 1911;
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}/${m}/${d}`;
}

// Certificate status codes for uqi2102f.do q_certificateStatus param
// 7=認可, 8=撤銷, 9=註銷, 12=廢止, 18=期限到期失效, A=全部
export const CERT_STATUS = {
  approved: "7",
  revoked: "8",
  cancelled: "9",
  abolished: "12",
  expired: "18",
  all: "A",
};

// Parse the certificate list rows from uqi2102f.do response.
// Columns: 證書號碼, 申請人, 商品名稱, 主型式, 申請模式, 證書狀態, 證書期限, 撤註銷日期, 依據
function parseCertList(html) {
  const listStart = html.indexOf('<div id="listContainer"');
  if (listStart === -1) return [];

  const listSection = html.slice(listStart);
  const rows = listSection.split(/<div class='row'/);
  const results = [];

  for (let i = 1; i < rows.length; i++) {
    const linkRe =
      /<a href="javascript:queryOne\('\d+'\)">([^<]*)<\/a>/g;
    const cols = [];
    let m;
    while ((m = linkRe.exec(rows[i])) !== null) {
      cols.push(m[1].trim());
    }

    if (cols.length < 7) continue;

    results.push({
      certificateNo: cols[0],
      applicant: cols[1],
      productName: cols[2],
      mainModel: cols[3],
      applicationMode: cols[4],
      certStatus: cols[5],
      certExpiry: cols[6],
      revokeDate: cols[7] || "",
      revokeBasis: cols[8] || "",
    });
  }

  return results;
}

// Parse total count from pagination info (e.g. "共65筆")
function parseTotalCount(html) {
  const m = html.match(/共(\d+)筆/);
  return m ? Number(m[1]) : 0;
}

/**
 * Query uqi2102f.do for certificates matching the given criteria.
 * Server requires q_regNo or q_certificateNo; date range alone is rejected.
 *
 * @param {object} options
 * @param {string} options.regNo - Numeric part of registration ID (e.g. "45879")
 * @param {Date} [options.fromDate] - Start of modification date range
 * @param {Date} [options.toDate] - End of modification date range
 * @param {string} [options.certStatus] - Certificate status code (see CERT_STATUS)
 * @param {number} [options.certType=1] - 1=RPC, 3=MRA
 * @param {number} [options.pageSize=100] - Results per page
 * @returns {Promise<{results: object[], totalCount: number}>}
 */
async function queryCertPage(options, page = 1) {
  const params = {
    state: "queryAll",
    q_certType: String(options.certType || 1),
    q_regNo: options.regNo,
    pageSize: String(options.pageSize || 100),
    currentPage: String(page),
  };

  if (options.fromDate) params.q_modDateS = toRocDate(options.fromDate);
  if (options.toDate) params.q_modDateE = toRocDate(options.toDate);
  if (options.certStatus) params.q_certificateStatus = options.certStatus;

  const body = new URLSearchParams(params);

  const res = await fetch(CERT_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: body.toString(),
  });

  if (!res.ok) {
    throw new Error(`BSMI cert query returned HTTP ${res.status}`);
  }

  const html = await res.text();

  if (html.includes("系統忙碌中")) {
    throw new Error("BSMI rate limited");
  }

  return {
    results: parseCertList(html),
    totalCount: parseTotalCount(html),
  };
}

/**
 * Fetch all recently changed certificates for a registration,
 * handling pagination automatically.
 *
 * @param {object} options
 * @param {string} options.regNo - Numeric part of registration ID
 * @param {Date} [options.fromDate] - Start of modification date range
 * @param {Date} [options.toDate] - End of modification date range
 * @param {string} [options.certStatus] - Certificate status filter
 * @param {number} [options.certType=1] - 1=RPC, 3=MRA
 * @returns {Promise<object[]>} All matching certificate records
 */
export async function fetchRecentChanges(options) {
  const pageSize = 100;
  const opts = { ...options, pageSize };

  const first = await queryCertPage(opts, 1);
  const allResults = [...first.results];
  const totalPages = Math.ceil(first.totalCount / pageSize);

  for (let page = 2; page <= totalPages; page++) {
    await sleep(1500);
    const next = await queryCertPage(opts, page);
    allResults.push(...next.results);
  }

  return allResults;
}

/**
 * Sync recently changed registrations by querying uqi2102f.do
 * for each known registration in the database.
 *
 * @param {number} days - Number of days to look back (default 7)
 * @param {import('./db.js').default} prisma - Prisma client instance
 * @returns {Promise<string[]>} Mark IDs that were refreshed
 */
export async function syncRecentChanges(prisma, days = 7) {
  const toDate = new Date();
  const fromDate = new Date(Date.now() - days * 86400 * 1000);

  const regs = await prisma.registration.findMany({
    select: { id: true },
    orderBy: { updatedAt: "asc" },
  });

  const refreshed = [];

  for (const reg of regs) {
    const regNo = reg.id.slice(1);

    try {
      // Check RPC (certType=1) for changes in date range
      const results = await fetchRecentChanges({
        regNo,
        fromDate,
        toDate,
        certType: 1,
      });

      if (results.length > 0) {
        // Fetch full registration data via uqi5102f.do and upsert
        const data = await fetchBsmi(reg.id);
        if (data) {
          const { certificates, ...vendor } = data;
          await prisma.$transaction(async (tx) => {
            await tx.certificate.deleteMany({ where: { registrationId: vendor.id } });
            await tx.registration.upsert({
              where: { id: vendor.id },
              create: { ...vendor, certificates: { create: certificates } },
              update: { ...vendor, certificates: { create: certificates } },
            });
          });
          refreshed.push(reg.id);
          console.log(`Refreshed ${reg.id} (${results.length} cert changes)`);
        }
      }
    } catch (err) {
      console.error(`Sync failed for ${reg.id}:`, err.message);
      if (err.message === "BSMI rate limited") {
        console.error("Rate limited, stopping sync");
        break;
      }
    }

    // Delay between registrations to avoid rate limiting
    await sleep(2000);
  }

  return refreshed;
}

export async function fetchBsmi(markId) {
  const type = markId[0];
  const no = markId.slice(1);

  const body = new URLSearchParams({
    state: "queryAll",
    q_regType: type,
    q_regNo: no,
    pageSize: "9999",
    currentPage: "1",
  });

  const res = await fetch(BSMI_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: body.toString(),
  });

  if (!res.ok) {
    throw new Error(`BSMI returned HTTP ${res.status}`);
  }

  const html = await res.text();

  // Check if we got results
  if (!html.includes("廠商資料")) {
    return null;
  }

  const vendor = parseVendor(html);
  const certificates = parseCertificates(html);

  return {
    id: markId.toUpperCase(),
    ...vendor,
    certificates,
  };
}
