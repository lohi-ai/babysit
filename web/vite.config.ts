import { defineConfig, type Plugin } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// Chrome blocks `<script type="module">` and `crossorigin` resource loads from
// file:// origins. Strip both so the bundle works as a classic script.
// `apply: 'build'` keeps this from rewriting dev-mode HTML, where vite still
// needs `type="module"` on `/src/main.tsx` to serve it as an ES module.
function fileProtocolFix(): Plugin {
  return {
    name: 'file-protocol-fix',
    apply: 'build',
    transformIndexHtml(html) {
      return html
        .replace(/\s+crossorigin(?=[\s>])/g, '')
        // type=module implies defer; classic scripts run before #root parses,
        // so add `defer` to keep boot order correct.
        .replace(/<script type="module" src=/g, '<script defer src=')
        .replace(/<script src="\.\/assets\//g, '<script defer src="./assets/');
    },
  };
}

export default defineConfig({
  plugins: [react(), tailwindcss(), fileProtocolFix()],
  // Relative paths so the generated index.html works under file://.
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: false,
    // IIFE single-file bundle — no ESM imports across files, runnable from disk.
    rollupOptions: {
      output: {
        format: 'iife',
        inlineDynamicImports: true,
        entryFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
    },
  },
});
