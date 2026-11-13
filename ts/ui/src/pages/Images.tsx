import { useCallback, useEffect, useMemo, useState } from 'react';

type ImageRow = {
  id: string;
  tags: string[];
  size: number;
  created: number;
  containers: number;
  dangling: boolean;
};

const GB = 1024 ** 3;

function fmtSize(bytes: number): string {
  if (bytes >= GB) return `${(bytes / GB).toFixed(2)} GB`;
  return `${Math.round(bytes / 1024 / 1024)} MB`;
}

function fmtDate(ms: number): string {
  return new Date(ms).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' });
}

export default function Images() {
  const [rows, setRows] = useState<ImageRow[]>([]);
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/images');
      const data = (await res.json()) as { items?: ImageRow[]; error?: string };
      if (data.error) throw new Error(data.error);
      setRows(data.items ?? []);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const notify = (msg: string) => {
    setToast(msg);
    window.setTimeout(() => setToast(null), 2400);
  };

  const remove = async (img: ImageRow) => {
    const label = img.tags[0] ?? img.id.slice(7, 19);
    if (!window.confirm(`删除镜像 ${label}？`)) return;
    setBusy(img.id);
    try {
      const res = await fetch(`/api/images/${encodeURIComponent(img.id)}?force=0`, { method: 'DELETE' });
      const data = (await res.json()) as { ok?: boolean; error?: string };
      if (!res.ok || data.error) throw new Error(data.error || `HTTP ${res.status}`);
      notify(`已删除 ${label}`);
      await load();
    } catch (err) {
      notify(`删除失败：${(err as Error).message}`);
    } finally {
      setBusy(null);
    }
  };

  const shown = useMemo(() => {
    const key = q.trim().toLowerCase();
    if (!key) return rows;
    return rows.filter((r) => r.tags.some((t) => t.toLowerCase().includes(key)) || r.id.includes(key));
  }, [rows, q]);

  const total = rows.reduce((n, r) => n + r.size, 0);

  return (
    <main className="main">
      <div className="top">
        <div className="crumb">docker <span>/</span> fnos <span>/</span> <b>镜像</b></div>
        <div className="sp" />
        <label className="search" style={{ width: 210 }}>
          <svg viewBox="0 0 24 24"><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" /></svg>
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="搜索镜像" />
        </label>
      </div>

      <div className="h">
        <h1>镜像</h1>
        <p>{rows.length} 个镜像 · 合计 {fmtSize(total)}</p>
      </div>

      <div className="ptable">
        <div className="phead">
          <span>镜像</span><span className="r">大小</span><span className="r">创建</span><span className="r">操作</span>
        </div>
        {error && <div className="err" style={{ margin: 12 }}>读取镜像失败：{error}</div>}
        {shown.map((r) => (
          <div className="prow" key={r.id}>
            <span className="pname">
              <b>{r.tags[0] ?? '（无标签）'}</b>
              <span>{r.id.slice(7, 19)}</span>
            </span>
            <span className="num r">{fmtSize(r.size)}</span>
            <span className="num r">{fmtDate(r.created)}</span>
            <span className="r">
              <button className="mini danger" disabled={busy === r.id} onClick={() => remove(r)}>
                {busy === r.id ? '删除中…' : '删除'}
              </button>
            </span>
          </div>
        ))}
        {!error && shown.length === 0 && <div className="empty">没有匹配的镜像</div>}
      </div>

      {toast && <div className="toast">{toast}</div>}
    </main>
  );
}
