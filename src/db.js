import { PrismaClient } from "./generated/prisma/client.ts";

let prisma;

if (process.env.NODE_ENV === "production") {
  prisma = new PrismaClient();
} else {
  const { PrismaBetterSqlite3 } = await import("@prisma/adapter-better-sqlite3");
  const url = process.env.DATABASE_URL || "file:./prisma/dev.db";
  const adapter = new PrismaBetterSqlite3({ url });
  prisma = new PrismaClient({ adapter });
}

export default prisma;
