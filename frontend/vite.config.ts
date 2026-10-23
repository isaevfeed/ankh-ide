import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

// `--mode http` swaps the Wails runtime for an HTTP shim that talks to
// cmd/devserver, so the UI can run in a normal browser.
export default defineConfig(({ mode }) => ({
  plugins: [react()],
  base: "./",
  resolve: {
    alias:
      mode === "http"
        ? { "@wailsio/runtime": fileURLToPath(new URL("./src/lib/runtime-http.ts", import.meta.url)) }
        : {},
  },
  build: { outDir: "dist", emptyOutDir: true, chunkSizeWarningLimit: 2000 },
}));
