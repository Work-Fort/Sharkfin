import { defineConfig } from 'vite';
import solid from 'vite-plugin-solid';
import { federation } from '@module-federation/vite';

export default defineConfig({
  plugins: [
    solid(),
    federation({
      name: 'sharkfin',
      filename: 'remoteEntry.js',
      exposes: {
        './index': './src/index.tsx',
      },
      shared: {
        'solid-js': { singleton: true },
        '@workfort/ui': { singleton: true },
        '@workfort/ui-solid': { singleton: true },
      },
    }),
  ],
  build: {
    target: 'esnext',
    outDir: 'dist',
  },
});
