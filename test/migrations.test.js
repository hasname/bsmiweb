import { readFileSync } from "node:fs";
import { afterEach, beforeEach, describe, expect, it } from "@jest/globals";
import Database from "better-sqlite3";

const migration = readFileSync(
  new URL("../prisma/migrations/0003_remove_empty_registrations/migration.sql", import.meta.url),
  "utf8",
);

let db;

beforeEach(() => {
  db = new Database(":memory:");
  db.exec(`
    PRAGMA foreign_keys = ON;
    CREATE TABLE registration (
      id TEXT PRIMARY KEY,
      tax_id TEXT NOT NULL DEFAULT '',
      applicant TEXT NOT NULL DEFAULT '',
      contact_addr TEXT NOT NULL DEFAULT '',
      company_addr TEXT NOT NULL DEFAULT '',
      phone TEXT NOT NULL DEFAULT '',
      note TEXT NOT NULL DEFAULT ''
    );
    CREATE TABLE certificate (
      id TEXT PRIMARY KEY,
      registration_id TEXT NOT NULL REFERENCES registration(id)
    );
  `);
});

afterEach(() => {
  db.close();
});

describe("remove empty registrations migration", () => {
  it("removes empty records and is safe to run again", () => {
    db.exec(`
      INSERT INTO registration (id) VALUES ('R00000');
      INSERT INTO registration
        VALUES ('R00001', ' ', ' ', ' ', ' ', ' ', ' ');
    `);

    db.exec(migration);
    expect(db.prepare("SELECT * FROM registration").all()).toEqual([]);

    db.exec(migration);
    expect(db.prepare("SELECT * FROM registration").all()).toEqual([]);
  });

  it.each([
    "tax_id",
    "applicant",
    "contact_addr",
    "company_addr",
    "phone",
    "note",
  ])("preserves records with %s even without certificates", (field) => {
    db.exec(`INSERT INTO registration (id, ${field}) VALUES ('R45879', 'data')`);
    const before = db.prepare("SELECT * FROM registration").all();

    db.exec(migration);

    expect(db.prepare("SELECT * FROM registration").all()).toEqual(before);
  });

  it("preserves records referenced by certificates while deleting other empty records", () => {
    db.exec(`
      INSERT INTO registration (id) VALUES ('R00000'), ('R45879');
      INSERT INTO certificate VALUES ('CI450068790054', 'R45879');
    `);
    const before = db.prepare("SELECT * FROM certificate").all();

    db.exec(migration);

    expect(db.prepare("SELECT id FROM registration").all()).toEqual([{ id: "R45879" }]);
    expect(db.prepare("SELECT * FROM certificate").all()).toEqual(before);
  });
});
