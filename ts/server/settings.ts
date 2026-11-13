import fs from 'node:fs';
import path from 'node:path';

export interface Settings {
  /** 优先检测名单（逗号分隔，不含 tag）；空 = 全部走快档 */
  checkOnly: string;
  /** 快档 cron（5 段） */
  checkCron: string;
  /** 慢档 cron；off/none/- 或空按默认 */
  checkCronOthers: string;
  /** 更新后是否清旧镜像 */
  cleanOldImage: boolean;
}

export const DEFAULTS: Settings = {
  checkOnly: '',
  checkCron: '30 * * * *',
  checkCronOthers: '30 4 * * *',
  cleanOldImage: true,
};

export function settingsPath(): string {
  const dir = process.env.DATA_DIR || path.resolve('./data');
  return path.join(dir, 'settings.json');
}

export function loadSettings(): Settings {
  try {
    const raw = fs.readFileSync(settingsPath(), 'utf8');
    const parsed = JSON.parse(raw) as Partial<Settings>;
    return { ...DEFAULTS, ...parsed };
  } catch {
    return { ...DEFAULTS };
  }
}

const CRON_RE = /^(\S+\s+){4}\S+$/;

export function validateSettings(input: Partial<Settings>): { ok: true; value: Settings } | { ok: false; error: string } {
  const cur = loadSettings();
  const next: Settings = {
    checkOnly: typeof input.checkOnly === 'string' ? input.checkOnly.trim() : cur.checkOnly,
    checkCron: typeof input.checkCron === 'string' ? input.checkCron.trim() : cur.checkCron,
    checkCronOthers: typeof input.checkCronOthers === 'string' ? input.checkCronOthers.trim() : cur.checkCronOthers,
    cleanOldImage: typeof input.cleanOldImage === 'boolean' ? input.cleanOldImage : cur.cleanOldImage,
  };
  if (next.checkCron && !CRON_RE.test(next.checkCron)) return { ok: false, error: 'checkCron 需要 5 段（分 时 日 月 周）' };
  const off = /^(off|none|-|disable|disabled)$/i.test(next.checkCronOthers);
  if (next.checkCronOthers && !off && !CRON_RE.test(next.checkCronOthers)) {
    return { ok: false, error: 'checkCronOthers 需要 5 段，或填 off 关闭' };
  }
  return { ok: true, value: next };
}

export function saveSettings(s: Settings): void {
  const file = settingsPath();
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const tmp = `${file}.tmp`;
  fs.writeFileSync(tmp, `${JSON.stringify(s, null, 2)}\n`);
  fs.renameSync(tmp, file);
}
