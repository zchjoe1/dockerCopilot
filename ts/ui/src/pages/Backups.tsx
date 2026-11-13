import { useCallback, useEffect, useState } from 'react';

type Backup = { file: string; name: string; image: string; createdAt: number; size: number };

function fmtDate(ms: number): string {
  return new Date(ms).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

export default function Backups() {
  const [items, setItems] = useState<Backup[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/backups');
      const data = (await res.json()) as { items?: Backup[]; error?: string };
      if (data.error) throw new Error(data.error);
      setItems(data.items ?? []);
      setError(null);
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const notify = (m: string) => {
    setToast(m);
    window.setTimeout(() => setToast(null), 2600);
  };

  const restore = async (b: Backup) => {
    if (!window.confirm(`用 ${b.name} 的备份恢复一个新容器？`)) return;
    setBusy(b.file);
    try {
      const res = await fetch(`/api/backups/${encodeURIComponent(b.file)}/restore`, { method: 'POST' });
      const data = (await res.json()) as { ok?: boolean; name?: string; error?: string };
      if (!res.ok || data.error) throw new Error(data.error || `HTTP ${res.status}`);
      notify(`已恢复为 ${data.name ?? '新容器'}`);
      await load();
    } catch (err) {
      notify(`恢复失败：${(err as Error).message}`);
    } finally {
      setBusy(null);
    }
  };

  const remove = async (b: Backup) => {
    if (!window.confirm(`删除备份 ${b.name}？`)) return;
    setBusy(b.file);
    try {
      const res = await fetch(`/api/backups/${encodeURIComponent(b.file)}`, { method: 'DELETE' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      notify('已删除备份');
      await load();
    } catch (err) {
      notify(`删除失败：${(err as Error).message}`);
    } finally {
      setBusy(null);
    }
  };

  return (
    <main className="main">
      <div className="top">
        <div className="crumb">docker <span>/</span> fnos <span>/</span> <b>备份</b></div>
      </div>

      <div className="h">
        <h1>备份</h1>
        <p>{items.length} 份容器配置备份 · 在容器详情里点「备份」生成</p>
      </div>

      <div className="ptable">
        <div className="phead">
          <span>备份</span><span className="r">时间</span><span className="r">大小</span><span className="r">操作</span>
        </div>
        {error && <div className="err" style={{ margin: 12 }}>读取失败：{error}</div>}
        {items.map((b) => (
          <div className="prow" key={b.file}>
            <span className="pname">
              <b>{b.name}</b>
              <span>{b.image || b.file}</span>
            </span>
            <span className="num r">{fmtDate(b.createdAt)}</span>
            <span className="num r">{Math.max(1, Math.round(b.size / 1024))} KB</span>
            <span className="r" style={{ display: 'flex', gap: 6, justifyContent: 'flex-end' }}>
              <button className="mini" disabled={busy === b.file} onClick={() => restore(b)}>恢复</button>
              <button className="mini danger" disabled={busy === b.file} onClick={() => remove(b)}>删除</button>
            </span>
          </div>
        ))}
        {!error && items.length === 0 && <div className="empty">还没有备份</div>}
      </div>

      {toast && <div className="toast">{toast}</div>}
    </main>
  );
}
