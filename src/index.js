import path from "node:path";
import { fileURLToPath } from "node:url";

import compression from "compression";
import express from "express";
import { minify } from "html-minifier-terser";

import { fetchBsmi } from "./bsmi.js";
import prisma from "./db.js";

const app = express();
const port = process.env.PORT || 3000;

app.use(compression());

const __dirname = path.dirname(fileURLToPath(import.meta.url));
app.set("view engine", "ejs");
app.set("views", path.join(__dirname, "views"));
app.use(express.static(path.join(__dirname, "..", "public")));

const minifyOptions = {
  collapseWhitespace: true,
  removeComments: true,
  minifyCSS: true,
};

const originalRender = app.response.render;
app.response.render = function (view, options, callback) {
  originalRender.call(this, view, options, async (err, html) => {
    if (err) {
      if (callback) return callback(err);
      return this.req.next(err);
    }
    try {
      const minified = await minify(html, minifyOptions);
      this.send(minified);
    } catch {
      this.send(html);
    }
  });
};

app.get("/", (req, res) => {
  res.render("index");
});

const VALID_ID_RE = /^[RTDQMrtdqm][A-Za-z0-9]{5}$/;

async function upsertRegistration(data) {
  const { certificates, ...vendor } = data;
  return await prisma.$transaction(async (tx) => {
    await tx.certificate.deleteMany({ where: { registrationId: vendor.id } });
    return await tx.registration.upsert({
      where: { id: vendor.id },
      create: { ...vendor, certificates: { create: certificates } },
      update: { ...vendor, certificates: { create: certificates } },
      include: { certificates: true },
    });
  });
}

async function refreshRegistration(id) {
  try {
    const data = await fetchBsmi(id);
    if (!data) return;
    await upsertRegistration(data);
  } catch (err) {
    console.error(`Background refresh failed for ${id}:`, err.message);
  }
}

app.get("/bsmi/:id", async (req, res, next) => {
  try {
    const id = req.params.id.toUpperCase();

    if (!VALID_ID_RE.test(id)) {
      res.status(404).send("Not Found");
      return;
    }

    let registration = await prisma.registration.findUnique({
      where: { id },
      include: { certificates: true },
    });

    if (registration) {
      // Refresh in background if stale (> 86400s)
      const ageMs = Date.now() - registration.updatedAt.getTime();
      if (ageMs > 86400 * 1000) {
        refreshRegistration(id);
      }
    } else {
      const data = await fetchBsmi(id);
      if (!data) {
        res.status(404).send("Not Found");
        return;
      }

      registration = await upsertRegistration(data);
    }

    const canonicalUrl = `${req.protocol}://${req.get("host")}/bsmi/${registration.id}`;
    res.set("Cache-Control", "public, max-age=3600");
    res.render("item", { registration, canonicalUrl });
  } catch (err) {
    next(err);
  }
});

app.get("/ban/:id", async (req, res, next) => {
  try {
    const taxId = req.params.id;

    const registrations = await prisma.registration.findMany({
      where: { taxId },
      orderBy: { id: "asc" },
    });

    if (registrations.length === 0) {
      res.status(404).send("Not Found");
      return;
    }

    res.set("Cache-Control", "public, max-age=3600");
    res.render("ban", { taxId, registrations });
  } catch (err) {
    next(err);
  }
});

app.get("/sitemap.xml", async (req, res, next) => {
  try {
    const registrations = await prisma.registration.findMany({
      select: { id: true, updatedAt: true },
      orderBy: { id: "asc" },
    });

    const baseUrl = `${req.protocol}://${req.get("host")}`;
    let xml = '<?xml version="1.0" encoding="UTF-8"?>\n';
    xml += '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n';
    xml += `  <url><loc>${baseUrl}/</loc></url>\n`;
    for (const reg of registrations) {
      const lastmod = reg.updatedAt.toISOString().split("T")[0];
      xml += `  <url><loc>${baseUrl}/bsmi/${reg.id}</loc><lastmod>${lastmod}</lastmod></url>\n`;
    }
    xml += "</urlset>\n";

    res.set("Cache-Control", "public, max-age=3600");
    res.type("application/xml").send(xml);
  } catch (err) {
    next(err);
  }
});

// 404 handler
app.use((req, res) => {
  res.status(404).send("Not Found");
});

// Error handler
// eslint-disable-next-line no-unused-vars
app.use((err, req, res, next) => {
  console.error(err.stack);
  res.status(500).send("Internal Server Error");
});

if (import.meta.url === `file://${process.argv[1]}`) {
  app.listen(port, () => {
    console.log(`Server listening on port ${port}`);
  });
}

export default app;
