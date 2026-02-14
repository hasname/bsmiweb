import path from "node:path";
import { fileURLToPath } from "node:url";

import express from "express";

import { fetchBsmi } from "./bsmi.js";
import prisma from "./db.js";

const app = express();
const port = process.env.PORT || 3000;

const __dirname = path.dirname(fileURLToPath(import.meta.url));
app.set("view engine", "ejs");
app.set("views", path.join(__dirname, "views"));
app.use(express.static(path.join(__dirname, "..", "public")));

app.get("/", (req, res) => {
  res.render("index");
});

const VALID_ID_RE = /^[RTDQMrtdqm][A-Za-z0-9]{5}$/;

app.get("/item/:id", async (req, res, next) => {
  try {
    const id = req.params.id.toUpperCase();

    if (!VALID_ID_RE.test(id)) {
      res.status(404).send("Not Found");
      return;
    }

    // Check DB first
    let registration = await prisma.registration.findUnique({
      where: { id },
      include: { certificates: true },
    });

    if (!registration) {
      const data = await fetchBsmi(id);
      if (!data) {
        res.status(404).send("Not Found");
        return;
      }

      const { certificates, ...vendor } = data;
      registration = await prisma.registration.create({
        data: {
          ...vendor,
          certificates: {
            create: certificates,
          },
        },
        include: { certificates: true },
      });
    }

    res.render("item", { registration });
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
