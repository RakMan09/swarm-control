import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dashboard talks to the API service (default :8081). Override with
// VITE_API_BASE / VITE_WS_URL at build or dev time.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
  },
});
