// Общий код из frontend/src ссылается на переменные Vite. На сервере их подставляет esbuild (build.mjs).
interface ImportMetaEnv {
  readonly BASE_URL: string;
  readonly VITE_USE_MOCK?: string;
  readonly VITE_API_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
