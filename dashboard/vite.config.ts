import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dashboard talks to the API service (default :8081). Override with
// VITE_API_BASE / VITE_WS_URL at build or dev time. VITE_BASE sets the public
// base path (e.g. "/swarm-control/" when hosted on GitHub Pages).
export default defineConfig({
  base: process.env.VITE_BASE || "/",
  plugins: [react()],
  server: {
    port: 5173,
  },
});
