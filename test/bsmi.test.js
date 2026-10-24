import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it, jest } from "@jest/globals";

import { fetchBsmi } from "../src/bsmi.js";

// Reduced from the BSMI response for R00000, which still has both sections.
const emptyHtml = readFileSync(
  new URL("./fixtures/bsmi-not-found.html", import.meta.url),
  "utf8",
);

function mockHtml(html, status = 200) {
  jest.spyOn(global, "fetch").mockResolvedValue(new Response(html, { status }));
}

afterEach(() => {
  jest.restoreAllMocks();
});

describe("fetchBsmi", () => {
  it("returns null for the empty vendor template returned by BSMI", async () => {
    mockHtml(emptyHtml);

    await expect(fetchBsmi("R00000")).resolves.toBeNull();
  });

  it("treats whitespace-only vendor fields as empty", async () => {
    mockHtml(emptyHtml.replaceAll("<p></p>", "<p> \n\t </p>"));

    await expect(fetchBsmi("R00000")).resolves.toBeNull();
  });

  it("keeps vendor data even without certificates or a tax ID", async () => {
    mockHtml(emptyHtml.replace(
      "申請人：</label>\n        <div class=\"form-control-static\"><p></p>",
      "申請人：</label>\n        <div class=\"form-control-static\"><p>測試公司</p>",
    ));

    await expect(fetchBsmi("R45879")).resolves.toMatchObject({
      id: "R45879",
      applicant: "測試公司",
      taxId: "",
      certificates: [],
    });
  });

  it("keeps certificates even when vendor fields are empty", async () => {
    mockHtml(emptyHtml.replace(
      '<div class="panel-body listRows"></div>',
      `<div class="panel-body listRows">
        <div class='row'>
          <div class="col-xs-6 col-sm-2 col-md-2 col-lg-2">CI450068790054<br>1130202<br>期限到期失效</div>
          <div class="col-xs-6 col-sm-2 col-md-2 col-lg-2">20W快充充電器(電源供應器)</div>
          <div class="col-xs-6 col-sm-2 col-md-2 col-lg-2"></div>
          <div class="col-xs-6 col-sm-3 col-md-2 col-lg-3"><strong>CH-866</strong><br></div>
          <div class="col-xs-6 col-sm-2 col-md-2 col-lg-2">經濟部標準檢驗局</div>
        </div>
      </div>`,
    ));

    const data = await fetchBsmi("R45879");
    expect(data.certificates).toHaveLength(1);
    expect(data.certificates[0]).toMatchObject({
      id: "CI450068790054",
      productName: "20W快充充電器(電源供應器)",
      mainModel: "CH-866",
    });
  });

  it.each([
    ["an HTTP error", "Service unavailable", 503],
    ["a rate limit page", `系統忙碌中${emptyHtml}`, 200],
    ["an unexpected page", "<html>Maintenance</html>", 200],
    ["a missing vendor field", emptyHtml.replace("統編：", "Unknown:"), 200],
    ["a missing certificate section", emptyHtml.replace("證書資料", "Unknown"), 200],
    ["a malformed certificate row", emptyHtml.replace(
      '<div class="panel-body listRows"></div>',
      '<div class="panel-body listRows">\n<div class=\'row\'>Unreadable certificate</div></div>',
    ), 200],
  ])("rejects %s instead of returning empty data", async (_label, html, status) => {
    mockHtml(html, status);

    await expect(fetchBsmi("R45879")).rejects.toThrow();
  });
});
