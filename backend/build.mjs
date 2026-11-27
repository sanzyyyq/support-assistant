// Сборка сервера в один файл. Общий код из frontend/src (логика агента, каталог, КП) вшивается в бандл,
// зависимости из node_modules остаются внешними.
import { build } from 'esbuild';

await build({
  entryPoints: ['src/server.ts'],
  outfile: 'dist/server.mjs',
  bundle: true,
  platform: 'node',
  format: 'esm',
  target: 'node20',
  packages: 'external',
  sourcemap: true,
  // Общий код фронтенда ссылается на import.meta.env — на сервере подставляем значения
  define: {
    'import.meta.env.BASE_URL': '"/"',
    'import.meta.env.VITE_USE_MOCK': '"false"',
  },
  logLevel: 'info',
});
