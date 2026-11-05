import { NextRequest, NextResponse } from "next/server";

function getTargetOrigin(): string {
  const envTarget =
    process.env.INTERNAL_API_BASE_URL ||
    (process.env.NEXT_PUBLIC_API_BASE_URL &&
    process.env.NEXT_PUBLIC_API_BASE_URL !== "same-origin"
      ? process.env.NEXT_PUBLIC_API_BASE_URL
      : "");

  if (envTarget && envTarget.trim().length > 0) {
    return envTarget.trim().replace(/\/$/, "");
  }

  // Default to Railway internal API domain on port 8000 when running in Railway production
  if (process.env.NODE_ENV === "production" && (process.env.RAILWAY_ENVIRONMENT || process.env.RAILWAY_SERVICE_ID)) {
    return "http://api.railway.internal:8000";
  }
  return "http://localhost:8080";
}

async function proxyRequest(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> }
) {
  try {
    const { path } = await context.params;
    const targetOrigin = getTargetOrigin();
    const search = request.nextUrl.search || "";
    const targetUrl = `${targetOrigin}/api/v1/${path.join("/")}${search}`;

    const headers = new Headers();
    request.headers.forEach((value, key) => {
      // Exclude hop-by-hop headers and host
      if (
        !["host", "connection", "keep-alive", "transfer-encoding"].includes(
          key.toLowerCase()
        )
      ) {
        headers.set(key, value);
      }
    });

    const body = ["GET", "HEAD"].includes(request.method)
      ? undefined
      : await request.arrayBuffer();

    const response = await fetch(targetUrl, {
      method: request.method,
      headers,
      body,
      cache: "no-store",
      redirect: "manual",
    });

    const resHeaders = new Headers();
    response.headers.forEach((value, key) => {
      resHeaders.set(key, value);
    });

    return new NextResponse(response.body, {
      status: response.status,
      statusText: response.statusText,
      headers: resHeaders,
    });
  } catch (error) {
    console.error("API proxy runtime error:", error);
    return NextResponse.json(
      {
        error: {
          code: "INTERNAL_ERROR",
          message: "バックエンドAPIとの通信に失敗しました。接続先: " + getTargetOrigin(),
        },
      },
      { status: 502 }
    );
  }
}

export const GET = proxyRequest;
export const POST = proxyRequest;
export const PUT = proxyRequest;
export const DELETE = proxyRequest;
export const PATCH = proxyRequest;
export const OPTIONS = proxyRequest;
