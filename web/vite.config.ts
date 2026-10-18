import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The build lands where the Go binary embeds it. The directory's .gitkeep is
// what lets the binary build without the page, so it is put back after every
// build empties the directory.
const outDir = resolve(__dirname, '../internal/web/dist');

export default defineConfig({
  plugins: [
    react(),
    { name: 'keep-dist', closeBundle: () => writeFileSync(resolve(outDir, '.gitkeep'), '') },
  ],
  build: { outDir, emptyOutDir: true, assetsInlineLimit: 0 },
});
