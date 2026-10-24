import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react()],
  // Beim Entwickeln leitet Vite /api an den Dashboard-Dienst weiter.
  server: { proxy: { "/api": { target: "http://127.0.0.1:8490", changeOrigin: true } } },
  build: { sourcemap: false },
  test: { environment: "jsdom", globals: true },
});
