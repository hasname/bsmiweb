const BSMI_URL = "https://civil.bsmi.gov.tw/bsmi_pqn/pqn/uqi5102f.do";

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

export async function fetchBsmi(markId) {
  const type = markId[0];
  const no = markId.slice(1);

  const body = new URLSearchParams({
    state: "queryAll",
    q_regType: type,
    q_regNo: no,
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
