import { NextRequest, NextResponse } from "next/server";

export async function GET(_req: NextRequest) {
  const targetOrigin = (
    process.env.INTERNAL_API_BASE_URL ||
    (process.env.NODE_ENV === "production" && (process.env.RAILWAY_ENVIRONMENT || process.env.RAILWAY_SERVICE_ID)
      ? "http://api.railway.internal:8000"
      : "http://localhost:8080")
  ).replace(/\/$/, "");

  try {
    const res = await fetch(`${targetOrigin}/healthz`, {
      cache: "no-store",
    });
    const data = await res.json();
    return NextResponse.json(data, { status: res.status });
  } catch {
    return NextResponse.json(
      { status: "error", message: "Failed to connect to API backend" },
      { status: 502 }
    );
  }
}
