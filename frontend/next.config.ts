import type { NextConfig } from "next";
const config: NextConfig = {
  output: "standalone",
  async headers() {
    return [
      { source: "/:path*", headers: [{ key: "Permissions-Policy", value: "microphone=()" }] },
      { source: "/app/:path*", headers: [{ key: "Permissions-Policy", value: "microphone=(self)" }] },
    ];
  },
};
export default config;

