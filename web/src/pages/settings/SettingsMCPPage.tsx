import { useEffect, useRef, useState } from 'react';
import { apiClient, getApiErrorMessage } from '../../api/client';
import type { Credential, IssuedCredential, Restrictions } from '../../api/generated';
import { useAuth } from '../../auth/AuthProvider';
import { useI18n } from '../../i18n/LocaleProvider';
import { useSettings } from '../../features/settings-editor';
import { ConfirmDialog } from '../../components/ui/ConfirmDialog';
import { McpDocsDialog } from './McpDocsDialog';
import { McpServerSettings } from './McpServerSettings';
import { McpSetupGuide } from './McpSetupGuide';
import { SettingsPageIntro, inputClassName, sectionClassName } from './shared';

const capabilities = ['read', 'metadata_write', 'scan_control', 'internal_llm', 'cleanup'];
const buttonClass = 'rounded-lg border border-gray-700 px-4 py-2 text-sm text-white hover:bg-gray-800 disabled:opacity-50';
type Choice = { id: number; name: string };

export function SettingsMCPPage() {
  const { isAdmin, user } = useAuth();
  return isAdmin && user ? <CredentialManager key={user.id} /> : null;
}

function CredentialManager() {
  const { t } = useI18n();
  const { config } = useSettings();
  const alive = useRef(false);
  const [docsOpen, setDocsOpen] = useState(false);
  const [rows, setRows] = useState<Credential[]>([]);
  const [users, setUsers] = useState<Choice[]>([]);
  const [libraries, setLibraries] = useState<Choice[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [secret, setSecret] = useState('');
  const [editing, setEditing] = useState<Credential | null>(null);
  const [revoke, setRevoke] = useState<Credential | null>(null);
  const [form, setForm] = useState<Restrictions>({ user_id: 0, name: '', library_ids: [], capabilities: ['read'] });

  useEffect(() => {
    alive.current = true;
    let active = true;
    Promise.all([
      apiClient.get<Credential[]>('/api/mcp/credentials'),
      apiClient.get<{ id: number; username: string }[]>('/api/users'),
      apiClient.get<Choice[]>('/api/libraries'),
    ]).then(([credentials, accounts, libs]) => {
      if (!active) return;
      setRows(credentials.data);
      setUsers(accounts.data.map((u) => ({ id: u.id, name: u.username })));
      setLibraries(libs.data);
      setForm((current) => ({ ...current, user_id: accounts.data[0]?.id || 0 }));
    }).catch((err) => { if (active) setError(getApiErrorMessage(err, t('mcp.failed'))); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; alive.current = false; };
  }, [t]);

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    if (busy) return;
    setBusy(true); setError(''); setSecret('');
    try {
      if (editing) {
        const { data } = await apiClient.patch<Credential>(`/api/mcp/credentials/${editing.id}`, form);
        if (!alive.current) return;
        setRows((current) => current.map((row) => row.id === data.id ? data : row));
        setEditing(null);
      } else {
        const { data } = await apiClient.post<IssuedCredential>('/api/mcp/credentials', form);
        if (!alive.current) return;
        const { token, ...credential } = data;
        setRows((current) => [credential, ...current]); setSecret(token);
      }
      setForm({ user_id: users[0]?.id || 0, name: '', library_ids: [], capabilities: ['read'] });
    } catch (err) { if (alive.current) setError(getApiErrorMessage(err, t('mcp.failed'))); }
    finally { if (alive.current) setBusy(false); }
  };
  const revokeCredential = async () => {
    if (!revoke || busy) return;
    setBusy(true); setError('');
    try {
      await apiClient.delete(`/api/mcp/credentials/${revoke.id}`);
      if (!alive.current) return;
      setRows((current) => current.map((row) => row.id === revoke.id ? { ...row, revoked: true } : row));
      setRevoke(null); setSecret('');
    } catch (err) { if (alive.current) setError(getApiErrorMessage(err, t('mcp.failed'))); }
    finally { if (alive.current) setBusy(false); }
  };
  const toggleLibrary = (id: number) => setForm((current) => ({ ...current, library_ids: current.library_ids.includes(id) ? current.library_ids.filter((v) => v !== id) : [...current.library_ids, id] }));
  const toggleCapability = (cap: string) => setForm((current) => ({ ...current, capabilities: current.capabilities.includes(cap) ? current.capabilities.filter((v) => v !== cap) : [...current.capabilities, cap] }));
  if (loading) return <p>{t('mcp.loading')}</p>;
  return <div className="space-y-6">
    <SettingsPageIntro title={t('mcp.title')} description={t('mcp.description')} />
    <p className="text-sm leading-6 text-amber-200">{t('mcp.transport')}</p>
    <McpSetupGuide publicUrl={config?.mcp.public_url || ''} onOpenDocs={() => setDocsOpen(true)} />
    <McpServerSettings />
    {error && <p role="alert" className="text-red-300">{error}</p>}
    {secret && <section className={sectionClassName}>
      <p>{t('mcp.secretHint')}</p><code className="block break-all select-all text-amber-200">{secret}</code>
      <button className={buttonClass} onClick={() => setSecret('')}>{t('mcp.dismiss')}</button>
    </section>}
    <form onSubmit={(event) => { void save(event); }} className={sectionClassName}>
      <h3 className="text-lg font-semibold">{editing ? t('mcp.narrow') : t('mcp.issue')}</h3>
      <label className="block">{t('mcp.user')}<select className={inputClassName} disabled={!!editing || busy} value={form.user_id} onChange={(e) => setForm({ ...form, user_id: Number(e.target.value) })}>{users.map((u) => <option key={u.id} value={u.id}>{u.name}</option>)}</select></label>
      <label className="block">{t('mcp.name')}<input className={inputClassName} required maxLength={100} disabled={!!editing || busy} value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} /></label>
      <fieldset disabled={busy}><legend className="mb-2 font-medium">{t('mcp.libraries')}</legend><div className="flex flex-wrap gap-4">{libraries.filter((lib) => !editing || editing.library_ids.includes(lib.id)).map((lib) => <label key={lib.id} className="flex items-center gap-2"><input type="checkbox" checked={form.library_ids.includes(lib.id)} onChange={() => toggleLibrary(lib.id)} />{lib.name}</label>)}</div></fieldset>
      <fieldset disabled={busy}><legend className="mb-2 font-medium">{t('mcp.capabilities')}</legend><div className="flex flex-wrap gap-4">{capabilities.filter((cap) => !editing || editing.capabilities.includes(cap)).map((cap) => <label key={cap} className="flex items-center gap-2"><input type="checkbox" checked={form.capabilities.includes(cap)} onChange={() => toggleCapability(cap)} />{t(`mcp.${cap}`)}</label>)}</div></fieldset>
      <p className="text-sm text-gray-400">{t('mcp.bounds')}</p>
      <button className={buttonClass} disabled={busy || !form.user_id} type="submit">{editing ? t('mcp.save') : t('mcp.issue')}</button>
      {editing && <button className={`${buttonClass} ml-2`} disabled={busy} type="button" onClick={() => { setEditing(null); setForm({ user_id: users[0]?.id || 0, name: '', library_ids: [], capabilities: ['read'] }); }}>{t('mcp.cancel')}</button>}
    </form>
    <section className={sectionClassName}>
      <h3 className="text-lg font-semibold">{t('mcp.credentials')}</h3>
      {!rows.length && <p className="text-gray-400">{t('mcp.empty')}</p>}
      {rows.map((row) => <article key={row.id} className="space-y-2 border-t border-gray-800 pt-4">
        <h4 className="font-semibold break-words">{row.name}</h4>
        <p className="text-sm text-gray-400">{users.find((u) => u.id === row.user_id)?.name || row.user_id}</p>
        <p className="text-sm">{t('mcp.libraries')}: {row.library_ids.map((id) => libraries.find((lib) => lib.id === id)?.name || id).join(', ') || t('mcp.none')}</p>
        <p className="text-sm">{t('mcp.capabilities')}: {row.capabilities.map((cap) => t(`mcp.${cap}`)).join(', ') || t('mcp.none')}</p>
        {row.revoked ? <p className="text-red-300">{t('mcp.revoked')}</p> : <div className="flex flex-wrap gap-2">
          <button className={buttonClass} disabled={busy || !!editing} onClick={() => { setSecret(''); setEditing(row); setForm({ user_id: row.user_id, name: row.name, library_ids: [...row.library_ids], capabilities: [...row.capabilities] }); }}>{t('mcp.narrow')}</button>
          <button className={buttonClass} disabled={busy || !!editing} onClick={() => setRevoke(row)}>{t('mcp.revoke')}</button>
        </div>}
      </article>)}
    </section>
    <ConfirmDialog open={!!revoke} title={t('mcp.revoke')} description={t('mcp.revokeHint')} confirmLabel={t('mcp.confirmRevoke')} loading={busy} tone="danger" onClose={() => setRevoke(null)} onConfirm={() => { void revokeCredential(); }} />
    <McpDocsDialog open={docsOpen} onClose={() => setDocsOpen(false)} />
  </div>;
}
