import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// `autablectl app dev` sets AUTABLE_SERVER and AUTABLE_TOKEN, so the dev
// server forwards /api to autable as you. Built apps call /api on the
// server that hosts them and need neither.
const server = process.env.AUTABLE_SERVER;
const token = process.env.AUTABLE_TOKEN;

export default defineConfig({
  // Relative asset URLs: autable serves the same build at the published
  // address and at every preview address.
  base: "./",
  plugins: [react()],
  server: {
    proxy: server
      ? {
          "/api": {
            target: server,
            changeOrigin: true,
            headers: token ? { Authorization: `Bearer ${token}` } : {},
          },
        }
      : undefined,
  },
});
