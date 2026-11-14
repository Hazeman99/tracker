import { defineConfig } from "vite";
import type { Plugin } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

// Strict CSP for the built app: only bundled scripts, styles, fonts and
// images; no remote requests. (The Go side sends the same policy as a header.)
// Dev builds served by the Vite dev server skip it, because hot reload needs
// a websocket and injected <style> tags.
const CSP =
  "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; " +
  "connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'";

function csp(): Plugin {
  return {
    name: "tracker-csp",
    transformIndexHtml(html, ctx) {
      const tag = ctx.server ? "" : `<meta http-equiv="Content-Security-Policy" content="${CSP}" />`;
      return html.replace("<!--%CSP%-->", tag);
    },
  };
}

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: {
    // Inline nothing as data: URLs except tiny images; fonts stay files.
    assetsInlineLimit: 0,
    sourcemap: false,
  },
  plugins: [react(), wails("./bindings"), csp()],
});
