import { describe, expect, it } from "vitest";
import { resolveApiBaseUrl } from "../lib/api";

describe("resolveApiBaseUrl (Railway & Same-Origin Proxy resolution)", () => {
  it("returns empty string in browser when NEXT_PUBLIC_API_BASE_URL is same-origin", () => {
    expect(resolveApiBaseUrl("same-origin", true)).toBe("");
  });

  it("blocks browser from calling .railway.internal directly and falls back to same-origin proxy", () => {
    expect(resolveApiBaseUrl("http://api.railway.internal:8080", true)).toBe("");
    expect(resolveApiBaseUrl("http://repoatlas-api.railway.internal:8080/", true)).toBe("");
  });

  it("returns explicit public HTTPS URL when configured", () => {
    expect(resolveApiBaseUrl("https://repoatlas-api.up.railway.app/", true)).toBe(
      "https://repoatlas-api.up.railway.app"
    );
  });

  it("uses localhost fallback on server side when same-origin is set without INTERNAL_API_BASE_URL", () => {
    expect(resolveApiBaseUrl("same-origin", false)).toBe("http://localhost:8080");
  });
});
