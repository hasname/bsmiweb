import path from "node:path";
import { fileURLToPath } from "node:url";

import express from "express";

const app = express();
const port = process.env.PORT || 3000;

const __dirname = path.dirname(fileURLToPath(import.meta.url));
app.set("view engine", "ejs");
app.set("views", path.join(__dirname, "views"));
app.use(express.static(path.join(__dirname, "..", "public")));

app.get("/", (req, res) => {
  res.render("index");
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
