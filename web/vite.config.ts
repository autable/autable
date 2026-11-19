import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const apiProxyTarget = process.env.AUTABLE_API_PROXY ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  build: {
    cssMinify: "esbuild"
  },
  server: {
    port: 5173,
    proxy: {
      "/api": apiProxyTarget,
      // Hosted apps are served by the backend under /apps/{database}/{name}/.
      "^/apps/": apiProxyTarget
    }
  },
  test: {
    exclude: ["e2e/**", "node_modules/**", "dist/**"],
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts"
  }
});
