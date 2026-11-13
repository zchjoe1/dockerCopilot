import { useEffect, useState } from 'react';

type Settings = {
  checkOnly: string;
  checkCron: string;
  checkCronOthers: string;
  cleanOldImage: boolean;
};

const EMPTY: Settings = { checkOnly: '', checkCron: '', checkCronOthers: '', cleanOldImage: true };

export default function SettingsPage() {
  const [form, setForm] = useState<Settings>(EMPTY);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    fetch('/api/settings')
      .then((r) => r.json())
      .then((d: Settings) => setForm({ ...EMPTY, ...d }))
      .catch((err) => setError((err as Error).message))
      .finally(() => setLoading(false));
  }, []);

  const set = <K extends keyof Settings>(key: K, value: Settings[K]) => {
    setForm((f) => ({ ...f, [key]: value }));
    setSaved(false);
  };

  const save = async () => {
    setSaving(true);
    setError(null);
    try {
      const res = await fetch('/api/settings', {
        method: 'PUT',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(form),
      });
      const data = (await res.json()) as Settings & { error?: string };
      if (!res.ok || data.error) throw new Error(data.error || `HTTP ${res.status}`);
      setForm({ ...EMPTY, ...data });
      setSaved(true);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  };

  return (
    <main className="main">
      <div className="top">
        <div className="crumb">docker <span>/</span> fnos <span>/</span> <b>设置</b></div>
      </div>

      <div className="h">
        <h1>设置</h1>
        <p>检测周期与优先名单，保存后下一轮检测生效</p>
      </div>

      <div className="form">
        <label className="field">
          <span>优先检测名单</span>
          <textarea
            rows={4}
            value={form.checkOnly}
            onChange={(e) => set('checkOnly', e.target.value)}
            placeholder="逗号分隔，例如 ghcr.io/smanx/deepseek-harness, calciumion/new-api"
            disabled={loading}
          />
          <em>名单里的镜像走快档周期，其余走慢档；留空表示全部按快档</em>
        </label>

        <div className="field-row">
          <label className="field">
            <span>快档周期</span>
            <input
              value={form.checkCron}
              onChange={(e) => set('checkCron', e.target.value)}
              placeholder="30 * * * *"
              disabled={loading}
            />
            <em>5 段 cron：分 时 日 月 周</em>
          </label>
          <label className="field">
            <span>慢档周期</span>
            <input
              value={form.checkCronOthers}
              onChange={(e) => set('checkCronOthers', e.target.value)}
              placeholder="30 4 * * *"
              disabled={loading}
            />
            <em>填 off 关闭慢档</em>
          </label>
        </div>

        <label className="check">
          <input
            type="checkbox"
            checked={form.cleanOldImage}
            onChange={(e) => set('cleanOldImage', e.target.checked)}
            disabled={loading}
          />
          <span>更新成功后自动清理被替换的旧镜像</span>
        </label>

        {error && <div className="err">{error}</div>}

        <div className="actions" style={{ marginTop: 6 }}>
          <button className="b go" onClick={save} disabled={saving || loading}>
            {saving ? '保存中…' : '保存'}
          </button>
          {saved && <span className="okhint">已保存</span>}
        </div>
      </div>

      <div className="band">
        <span className="lbl">当前版本 <b>dc-ts</b> · 设置存于 <b>data/settings.json</b></span>
      </div>
    </main>
  );
}
