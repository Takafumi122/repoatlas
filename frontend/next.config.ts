import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  async rewrites() {
    const rawTarget =
      process.env.INTERNAL_API_BASE_URL ||
      (process.env.NEXT_PUBLIC_API_BASE_URL &&
      process.env.NEXT_PUBLIC_API_BASE_URL !== "same-origin"
        ? process.env.NEXT_PUBLIC_API_BASE_URL
        : "") ||
      "http://localhost:8080";
    const backendOrigin = rawTarget.replace(/\/$/, "");

    return [
      {
        source: "/api/v1/:path*",
        destination: `${backendOrigin}/api/v1/:path*`,
      },
      {
        source: "/healthz",
        destination: `${backendOrigin}/healthz`,
      },
    ];
  },
};

export default nextConfig;
