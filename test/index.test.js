import { describe, expect, it } from "@jest/globals";
import request from "supertest";
import app from "../src/index.js";

describe("GET /", () => {
  it("should return 200", async () => {
    const res = await request(app).get("/");
    expect(res.status).toBe(200);
  });
});

describe("GET /nonexistent", () => {
  it("should return 404", async () => {
    const res = await request(app).get("/nonexistent");
    expect(res.status).toBe(404);
  });
});
