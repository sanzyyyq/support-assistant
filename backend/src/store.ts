import { existsSync, mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs';
import { dirname } from 'node:path';
import type { DB } from '../../frontend/src/api/mock/data';

/**
 * Хранилище демо-данных: вся база в памяти, на диск — JSON-файл.
 * Запись — через временный файл и rename, чтобы при падении не получить обрезанный JSON.
 * Для хакатона этого достаточно; для продакшена заменить на Postgres.
 */
export class FileStore {
  private db: DB | null = null;
  private timer: NodeJS.Timeout | null = null;

  constructor(
    private readonly file: string,
    private readonly seed: () => DB,
  ) {}

  load() {
    if (this.db) return;
    if (existsSync(this.file)) {
      try {
        this.db = JSON.parse(readFileSync(this.file, 'utf8')) as DB;
        return;
      } catch (e) {
        console.warn(`Не удалось прочитать ${this.file}, создаю демо-данные заново:`, e);
      }
    }
    this.db = this.seed();
    this.flush();
  }

  get(): DB {
    if (!this.db) throw new Error('Хранилище не загружено');
    return this.db;
  }

  mutate<T>(fn: (db: DB) => T): T {
    const result = fn(this.get());
    this.schedule();
    return result;
  }

  reset() {
    this.db = this.seed();
    this.flush();
  }

  private schedule() {
    if (this.timer) return;
    this.timer = setTimeout(() => this.flush(), 300);
  }

  flush() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
    if (!this.db) return;
    mkdirSync(dirname(this.file), { recursive: true });
    const tmp = `${this.file}.tmp`;
    writeFileSync(tmp, JSON.stringify(this.db));
    renameSync(tmp, this.file);
  }
}
