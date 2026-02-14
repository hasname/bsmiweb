import { describe, expect, it } from "@jest/globals";
import request from "supertest";
import app from "../src/index.js";

describe("GET /", () => {
  it("should return 200 with Hello, World!", async () => {
    const res = await request(app).get("/");
    expect(res.status).toBe(200);
    expect(res.text).toBe("Hello, World!");
  });
});

describe("GET /nonexistent", () => {
  it("should return 404", async () => {
    const res = await request(app).get("/nonexistent");
    expect(res.status).toBe(404);
  });
});
