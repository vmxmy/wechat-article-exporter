import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vite';

const workspaceRoot = fileURLToPath(new URL('../', import.meta.url));

export default defineConfig({
  resolve: {
    alias: [
      { find: '#shared', replacement: `${workspaceRoot}/shared` },
      { find: /^~\//, replacement: `${workspaceRoot}/` },
      { find: /^@\//, replacement: `${workspaceRoot}/` },
    ],
  },
});
