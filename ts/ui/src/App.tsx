import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

type Row = {
  id: string;
  name: string;
  image: string;
  state: 'running' | 'exited' | 'created' | 'paused' | 'restarting' | 'dead';
  status: string;
  uptimeSec: number;
  cpuPct: number | null;
  memBytes: number | null;
  createdAt: number;
  project?: string;
  hasUpdate?: boolean;
};

type Detail = {
  id: string;
  name: string;
  image: string;
  imageId: string;
  state: string;
  startedAt: string | null;
  restartPolicy: string;
  createdAt: number | null;
};

const GB = 1024 ** 3;

function fmtMem(bytes: number | null): string {
  if (!bytes) return '—';
  if (bytes >= GB) return `${(bytes / GB).toFixed(1)}G`;
  return `${Math.round(bytes / 1024 / 1024)}M`;
}

function fmtUp(sec: number): string {
  if (!sec) return '—';
  const h = Math.floor(sec / 3600);
  if (h >= 24) return `${Math.floor(h / 24)}d ${String(h % 24).padStart(2, '0')}h`;
  if (h >= 1) return `${h}h ${String(Math.floor((sec % 3600) / 60)).padStart(2, '0')}m`;
  return `${Math.max(1, Math.floor(sec / 60))}m`;
}

const ACT_LABEL: Record<string, string> = {
  start: '已启动',
  stop: '已停止',
  restart: '已重启',
  update: '已开始更新',
  rename: '已重命名',
};

const IconGrid = () => (
  <svg viewBox="0 0 24 24">
    <rect x="3.5" y="3.5" width="7" height="7" rx="1" />
    <rect x="13.5" y="3.5" width="7" height="7" rx="1" />
    <rect x="3.5" y="13.5" width="7" height="7" rx="1" />
    <rect x="13.5" y="13.5" width="7" height="7" rx="1" />
  </svg>
);

export default function App() {
  const [rows, setRows] = useState<Row[]>([]);
  const [sel, setSel] = useState<string | null>(null);
  const [detail, setDetail] = useState<Detail | null>(null);
  const [toast, setToast] = useState<string | null>(null);
  const [mode, setMode] = useState<string>('');
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sheetOpen, setSheetOpen] = useState(false);
  const timer = useRef<number | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/containers');
      const data = (await res.json()) as { items: Row[]; error?: string };
      if (data.error) throw new Error(data.error);
      setRows(data.items);
      setError(null);
      setSel((prev) => prev ?? data.items.find((r) => r.hasUpdate)?.id ?? data.items[0]?.id ?? null);
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);

  useEffect(() => {
    fetch('/api/health')
      .then((r) => r.json())
      .then((d) => setMode(d.mode))
      .catch(() => {});
    load();
    timer.current = window.setInterval(load, 10_000);
    return () => {
      if (timer.current) window.clearInterval(timer.current);
    };
  }, [load]);

  useEffect(() => {
    if (!sel) return;
    let alive = true;
    fetch(`/api/containers/${sel}`)
      .then((r) => r.json())
      .then((d) => {
        if (alive) setDetail(d.error ? null : (d as Detail));
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [sel, rows]);

  const selected = useMemo(() => rows.find((r) => r.id === sel) ?? null, [rows, sel]);

  const notify = (msg: string) => {
    setToast(msg);
    window.setTimeout(() => setToast(null), 2600);
  };

  const run = async (id: string, action: 'start' | 'stop' | 'restart' | 'update') => {
    setBusy(`${id}:${action}`);
    try {
      const res = await fetch(`/api/containers/${id}/${action}`, { method: 'POST' });
      const data = (await res.json()) as { ok?: boolean; error?: string };
      if (!res.ok || data.error) throw new Error(data.error || `HTTP ${res.status}`);
      notify(`${ACT_LABEL[action] ?? '已执行'}：${rows.find((r) => r.id === id)?.name ?? id}`);
      await load();
    } catch (err) {
      notify(`操作失败：${(err as Error).message}`);
    } finally {
      setBusy(null);
    }
  };

  const rename = async (id: string, current: string) => {
    const next = window.prompt('新的容器名', current);
    if (!next || next.trim() === current) return;
    setBusy(`${id}:rename`);
    try {
      const res = await fetch(`/api/containers/${id}/rename`, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ name: next.trim() }),
      });
      const data = (await res.json()) as { ok?: boolean; error?: string };
      if (!res.ok || data.error) throw new Error(data.error || `HTTP ${res.status}`);
      notify(`${ACT_LABEL.rename}：${next.trim()}`);
      await load();
    } catch (err) {
      notify(`重命名失败：${(err as Error).message}`);
    } finally {
      setBusy(null);
    }
  };

  const updates = rows.filter((r) => r.hasUpdate);
  const totalMem = rows.reduce((n, r) => n + (r.memBytes ?? 0), 0);
  const maxMem = Math.max(...rows.map((r) => r.memBytes ?? 0), 1);

  const inspector = selected ? (
    <>
      <div className="hd">
        <div className="r1">
          <span className={`led${selected.state === 'running' ? '' : ' off'}`} style={{ flex: 'none' }} />
          <span>{selected.state === 'running' ? '运行中' : '已停止'} · {fmtUp(selected.uptimeSec)}</span>
          <span className="sp" />
          <button className="mini" onClick={() => rename(selected.id, selected.name)} disabled={busy !== null}>重命名</button>
          <button className="mini close" onClick={() => setSheetOpen(false)} aria-label="关闭">✕</button>
        </div>
        <h2>{selected.name}</h2>
        <div className="im">{selected.image}</div>
      </div>

      <div className="bd">
        <div className="kpi">
          <div><span>CPU</span><b>{(selected.cpuPct ?? 0).toFixed(1)}<em>%</em></b></div>
          <div><span>内存</span><b>{fmtMem(selected.memBytes)}</b></div>
        </div>

        <div className="sec">
          <div className="lb"><span>镜像</span><span>{selected.hasUpdate ? '可更新' : '最新'}</span></div>
          <div className="note">
            <div>{selected.hasUpdate ? '远端 digest 与本地不一致' : '镜像与远端一致'}</div>
            <div>{detail?.imageId ? `${detail.imageId.slice(0, 20)}…` : '—'}</div>
          </div>
        </div>

        <div className="sec">
          <div className="lb"><span>运行信息</span></div>
          <div className="info">
            <div><span>重启策略</span><b>{detail?.restartPolicy ?? '—'}</b></div>
            <div><span>状态</span><b>{detail?.state ?? selected.state}</b></div>
            <div><span>容器 ID</span><b>{selected.id.slice(0, 12)}</b></div>
          </div>
        </div>

        <div className="actions">
          <button className="b" disabled={busy !== null} onClick={() => run(selected.id, 'restart')}>重启</button>
          <button className="b" disabled={busy !== null} onClick={() => run(selected.id, 'stop')}>停止</button>
          {selected.hasUpdate && (
            <button className="b go" disabled={busy !== null} onClick={() => run(selected.id, 'update')}>拉取并重建</button>
          )}
        </div>
      </div>

      <div className="ft"><span>{selected.id.slice(0, 12)}</span></div>
    </>
  ) : (
    <div className="hd"><span style={{ color: 'var(--fg-3)', fontSize: 11 }}>加载中…</span></div>
  );

  return (
    <div className="app">
      <nav className="rail">
        <div className="logo">
          <svg viewBox="0 0 24 24">
            <path d="M12 2.8 21 7.6v8.8L12 21.2 3 16.4V7.6z" />
            <path d="M3 7.6 12 12.4l9-4.8M12 12.4v8.8" />
          </svg>
        </div>
        <a href="#" aria-current="page" title="容器"><IconGrid /></a>
        <a href="#" title="镜像">
          <svg viewBox="0 0 24 24"><path d="M12 2.8 21 7.6v8.8L12 21.2 3 16.4V7.6z" /><path d="M3 7.6 12 12.4l9-4.8M12 12.4v8.8" /></svg>
        </a>
        <a href="#" title="备份">
          <svg viewBox="0 0 24 24"><rect x="3.5" y="6" width="17" height="13" rx="2" /><path d="M3.5 10h17M9 14.5h6" /></svg>
        </a>
        <a href="#" title="Compose">
          <svg viewBox="0 0 24 24"><path d="M12 3.5 21 8l-9 4.5L3 8z" /><path d="M3 12.2 12 16.7l9-4.5" /><path d="M3 16.4 12 20.9l9-4.5" /></svg>
        </a>
        <div className="sp" />
        <a href="#" title="设置">
          <svg viewBox="0 0 24 24"><path d="M4 7h9M16.5 7H20M4 17h3.5M11 17h9" /><circle cx="14.5" cy="7" r="2" /><circle cx="9" cy="17" r="2" /></svg>
        </a>
      </nav>

      <main className="main">
        <div className="top">
          <div className="crumb">docker <span>/</span> fnos <span>/</span> <b>容器</b></div>
          <div className="sp" />
          <span className="live"><i />每 10 秒检测{mode ? ` · ${mode}` : ''}</span>
          <span className="chip">
            <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="8" /><path d="M12 7.5V12l3 1.8" /></svg>
            最近 30 分钟
          </span>
        </div>

        {updates.length > 0 && (
          <div className="inc">
            <span className="d" />
            <span className="tag">UPD</span>
            <span className="t">
              <b>{updates[0].name}</b> 有新镜像可用 · <span className="num" style={{ fontSize: 10.5, color: '#8C7C55' }}>{updates[0].image}</span>
            </span>
            <span className="sp" />
            <a className="lk" href="#" onClick={(e) => { e.preventDefault(); setSel(updates[0].id); setSheetOpen(true); }}>
              处理<svg viewBox="0 0 24 24"><path d="m9 6 6 6-6 6" /></svg>
            </a>
          </div>
        )}

        <div className="h">
          <h1>容器控制墙</h1>
          <p>已加载 {rows.length} 个容器 · {updates.length} 个待处理 · 按内存排序</p>
        </div>

        <div className="wall">
          {error && <div className="err">读取容器列表失败：{error}</div>}
          {rows.map((r) => {
            const cpu = r.cpuPct ?? 0;
            const memW = Math.round(((r.memBytes ?? 0) / maxMem) * 100);
            return (
              <div
                key={r.id}
                className={`tile${r.hasUpdate ? ' upd' : ''}${sel === r.id ? ' sel' : ''}`}
                onClick={() => { setSel(r.id); setSheetOpen(true); }}
              >
                <div className="r1">
                  <span className={`led${r.state === 'running' ? '' : ' off'}`} />
                  <b>{r.name}</b>
                </div>
                <div className="im">{r.image}</div>
                <div className="m">
                  <span>CPU</span>
                  <div className="tr"><i style={{ width: `${Math.min(100, cpu * 4 || 2)}%` }} /></div>
                  <em>{cpu.toFixed(1)}%</em>
                </div>
                <div className="m">
                  <span>内存</span>
                  <div className="tr"><i style={{ width: `${Math.max(2, memW)}%` }} /></div>
                  <em>{fmtMem(r.memBytes)}</em>
                </div>
                {r.hasUpdate ? (
                  <button
                    className="ub"
                    disabled={busy === `${r.id}:update`}
                    onClick={(e) => { e.stopPropagation(); run(r.id, 'update'); }}
                  >
                    {busy === `${r.id}:update` ? '正在更新…' : '拉取并重建'}
                  </button>
                ) : (
                  <div className="tf">
                    <span>运行 {fmtUp(r.uptimeSec)}</span>
                    <svg viewBox="0 0 24 24"><path d="m9 6 6 6-6 6" /></svg>
                  </div>
                )}
              </div>
            );
          })}
        </div>

        <div className="band">
          <span className="lbl">共 {rows.length} 个 · 内存合计 {fmtMem(totalMem)}</span>
          <span className="sp" />
          <span className="lbl">上次采样 <b>{mode === 'mock' ? 'mock' : '10s'}</b></span>
        </div>
      </main>

      <aside className="insp panel">{inspector}</aside>

      {sheetOpen && (
        <>
          <div className="backdrop" onClick={() => setSheetOpen(false)} />
          <div className="sheet panel">{inspector}</div>
        </>
      )}

      {toast && <div className="toast">{toast}</div>}
    </div>
  );
}
