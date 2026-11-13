/** 镜像/容器图标上传：存到 DATA_DIR/icons，返回可直接引用的 URL。 */
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';

const ALLOWED: Record<string, string> = {
  'image/png': '.png',
  'image/jpeg': '.jpg',
  'image/webp': '.webp',
  'image/gif': '.gif',
  'image/svg+xml': '.svg',
};

export function iconsDir(): string {
  const d = path.join(process.env.DATA_DIR || path.resolve('./data'), 'icons');
  fs.mkdirSync(d, { recursive: true });
  return d;
}

export interface IconMeta {
  name: string;
  url: string;
  size: number;
  uploadedAt: number;
}

export function listIcons(): IconMeta[] {
  const d = iconsDir();
  return fs
    .readdirSync(d)
    .filter((f) => !f.startsWith('.'))
    .map((name) => {
      const st = fs.statSync(path.join(d, name));
      return { name, url: `/api/icons/${name}`, size: st.size, uploadedAt: st.mtimeMs };
    })
    .sort((a, b) => b.uploadedAt - a.uploadedAt);
}

export function saveIcon(folderName: string, bytes: Uint8Array, contentType: string): IconMeta {
  const ext = ALLOWED[contentType];
  if (!ext) throw new Error(`不支持的图片类型：${contentType}`);
  if (bytes.byteLength > 2 * 1024 * 1024) throw new Error('图标不能超过 2MB');
  const safe = folderName.replace(/[^a-zA-Z0-9_.-]/g, '_').slice(0, 60) || 'icon';
  const name = `${safe}-${crypto.randomBytes(4).toString('hex')}${ext}`;
  const full = path.join(iconsDir(), name);
  fs.writeFileSync(full, bytes);
  return { name, url: `/api/icons/${name}`, size: bytes.byteLength, uploadedAt: Date.now() };
}

export function readIcon(name: string): { bytes: Buffer; ext: string } | null {
  const safe = path.basename(name);
  const full = path.join(iconsDir(), safe);
  if (!fs.existsSync(full)) return null;
  return { bytes: fs.readFileSync(full), ext: path.extname(safe).toLowerCase() };
}

export function deleteIcon(name: string): void {
  fs.rmSync(path.join(iconsDir(), path.basename(name)), { force: true });
}
