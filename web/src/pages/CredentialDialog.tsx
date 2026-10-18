import { useEffect, useState } from 'react';
import { api } from '../lib/api';
import { useBusy } from '../lib/hooks';
import { useLang } from '../lib/i18n';
import type { Credential } from '../lib/types';
import { Button, Field, IconButton, TextInput } from '../ui/controls';
import { Icon } from '../ui/Icon';
import { Dialog } from '../ui/overlays';

const MODEL_KEY = /^ANTHROPIC_DEFAULT_([A-Z0-9]+)_MODEL$/;
const COMMON_ALIASES = ['opus', 'fable', 'sonnet', 'haiku'];

interface Row {
  id: number;
  a: string;
  b: string;
}

let rowSeq = 0;
const row = (a = '', b = ''): Row => ({ id: ++rowSeq, a, b });

/** isProviderKey mirrors the Go rule: what a credential may carry. */
function isProviderKey(k: string): boolean {
  if (k.startsWith('ANTHROPIC_')) return true;
  return k.startsWith('CLAUDE_CODE_') && (/(^|_)(TOKEN|KEY|AUTH|SECRET|BEARER|CREDENTIAL)(_|$)/i.test(k) || /^CLAUDE_CODE_USE_(BEDROCK|VERTEX|FOUNDRY)$/.test(k));
}

const looksSecret = (k: string) => /(^|_)(TOKEN|KEY|AUTH|SECRET|BEARER|CREDENTIAL)(_|$)/i.test(k);

export function CredentialDialog({
  open,
  editing,
  onClose,
  onSaved,
}: {
  open: boolean;
  editing: Credential | null;
  onClose: () => void;
  onSaved: (name: string, created: boolean) => void;
}) {
  const { L } = useLang();
  const [name, setName] = useState('');
  const [provider, setProvider] = useState('astergate');
  const [baseURL, setBaseURL] = useState('');
  const [key, setKey] = useState('');
  const [showKey, setShowKey] = useState(false);
  const [models, setModels] = useState<Row[]>([]);
  const [extra, setExtra] = useState<Row[]>([]);
  const [error, setErrorText] = useState('');
  const [errorSeq, setErrorSeq] = useState(0);
  const setError = (e: string) => {
    setErrorText(e);
    if (e) setErrorSeq((n) => n + 1);
  };
  const [busy, run] = useBusy();

  useEffect(() => {
    if (!open) return;
    setError('');
    setKey('');
    setShowKey(false);
    if (editing) {
      setName(editing.name);
      setProvider(editing.provider);
      setBaseURL(editing.baseURL);
      setModels(editing.models.map((m) => row(m.alias, m.model)));
      setExtra(
        Object.entries(editing.env)
          .filter(([k]) => !MODEL_KEY.test(k))
          .map(([k, v]) => row(k, v)),
      );
    } else {
      setName('');
      setProvider('astergate');
      setBaseURL('');
      setModels([row('opus'), row('sonnet'), row('haiku')]);
      setExtra([]);
    }
  }, [open, editing]);

  const [bad, setBad] = useState<'name' | 'baseURL' | 'key' | null>(null);
  const problems = (): string => {
    const missing = (field: 'name' | 'baseURL' | 'key', msg: string) => {
      setBad(field);
      return msg;
    };
    setBad(null);
    if (!editing && !name.trim()) return missing('name', L('起个名字', 'Give it a name'));
    if (!baseURL.trim()) return missing('baseURL', L('填上供应商的地址', 'Fill in the provider’s address'));
    if (!editing && !key.trim()) return missing('key', L('填上 key', 'Fill in the key'));
    for (const m of models) {
      if (!m.a.trim() !== !m.b.trim()) return L('每条模型映射都要填别名和模型', 'Every model mapping needs an alias and a model');
    }
    for (const r of extra) {
      const k = r.a.trim();
      if (!k) continue;
      if (!isProviderKey(k)) return L(`${k} 不属于供应商：它该写在共享设置里`, `${k} is not provider env: it belongs in the shared settings`);
      if (looksSecret(k)) return L(`${k} 像是秘密：凭据唯一的秘密是 key`, `${k} looks like a secret: a credential’s only secret is its key`);
    }
    return '';
  };

  const save = async () => {
    const p = problems();
    if (p) {
      setError(p);
      return;
    }
    const env: Record<string, string> = {};
    for (const m of models) if (m.a.trim()) env[`ANTHROPIC_DEFAULT_${m.a.trim().toUpperCase()}_MODEL`] = m.b.trim();
    for (const r of extra) if (r.a.trim()) env[r.a.trim()] = r.b;
    const body = { provider: provider.trim(), baseURL: baseURL.trim(), env, key: key.trim() };
    try {
      if (editing) await run(api.editCredential(editing.name, body));
      else await run(api.addCredential({ ...body, name: name.trim() }));
      onSaved(editing?.name ?? name.trim(), !editing);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const missingAliases = COMMON_ALIASES.filter((a) => !models.some((m) => m.a.trim().toLowerCase() === a));

  return (
    <Dialog
      open={open}
      onClose={onClose}
      dismissable={!busy}
      width={600}
      title={editing ? L(`编辑 ${editing.name}`, `Edit ${editing.name}`) : L('添加凭据', 'Add a credential')}
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            {L('取消', 'Cancel')}
          </Button>
          <Button variant="primary" busy={busy} onClick={save}>
            {editing ? L('保存', 'Save') : L('添加', 'Add')}
          </Button>
        </>
      }
    >
      <div className="form">
        <div className="form-grid">
          {!editing && (
            <Field label={L('名称', 'Name')} hint={L('字母、数字、. _ -', 'Letters, digits, . _ -')}>
              <TextInput value={name} invalid={bad === 'name'} onChange={(e) => setName(e.target.value)} placeholder="claude-pool" />
            </Field>
          )}
          <Field label={L('供应商', 'Provider')} hint={L('AsterGate 网关填 astergate', 'astergate for an AsterGate gateway')}>
            <TextInput value={provider} onChange={(e) => setProvider(e.target.value)} placeholder="astergate" />
          </Field>
        </div>
        <Field label={L('地址', 'Base URL')}>
          <TextInput value={baseURL} invalid={bad === 'baseURL'} onChange={(e) => setBaseURL(e.target.value)} placeholder="https://gw.example.com" mono />
        </Field>
        <Field
          label="Key"
          hint={editing ? L('已保存在本机。留空就保持不变', 'Stored on this machine. Leave empty to keep it') : L('只保存在本机（同步时加密）', 'Stored on this machine only (encrypted when synced)')}
        >
          <div className="input-with-action">
            <TextInput
              type={showKey ? 'text' : 'password'}
              invalid={bad === 'key'}
              value={key}
              onChange={(e) => setKey(e.target.value)}
              placeholder={editing ? '••••••••••••' : 'sk-…'}
              mono
            />
            <IconButton icon={showKey ? 'eyeOff' : 'eye'} label={showKey ? L('隐藏', 'Hide') : L('显示', 'Show')} onClick={() => setShowKey((s) => !s)} />
          </div>
        </Field>

        <div className="form-section">
          <div className="form-section-head">
            <span>{L('模型映射', 'Model mapping')}</span>
            <span className="faint">{L('别名 → 供应商那边的模型', 'alias → the provider’s model')}</span>
          </div>
          <div className="rows">
            {models.map((m) => (
              <div key={m.id} className="kv-row">
                <TextInput
                  className="kv-key"
                  value={m.a}
                  placeholder={L('别名', 'alias')}
                  onChange={(e) => setModels((ms) => ms.map((x) => (x.id === m.id ? { ...x, a: e.target.value } : x)))}
                />
                <Icon name="arrowRight" size={14} className="faint" />
                <TextInput
                  mono
                  value={m.b}
                  placeholder="claude-opus-5-5"
                  onChange={(e) => setModels((ms) => ms.map((x) => (x.id === m.id ? { ...x, b: e.target.value } : x)))}
                />
                <IconButton icon="x" size={16} label={L('删除', 'Remove')} onClick={() => setModels((ms) => ms.filter((x) => x.id !== m.id))} />
              </div>
            ))}
          </div>
          <div className="chips-add">
            {missingAliases.map((a) => (
              <button key={a} type="button" className="chip-add" onClick={() => setModels((ms) => [...ms, row(a)])}>
                <Icon name="plus" size={13} />
                {a}
              </button>
            ))}
            <button type="button" className="chip-add" onClick={() => setModels((ms) => [...ms, row()])}>
              <Icon name="plus" size={13} />
              {L('其他别名', 'other alias')}
            </button>
          </div>
        </div>

        <div className="form-section">
          <div className="form-section-head">
            <span>{L('其他供应商 env', 'Other provider env')}</span>
            <span className="faint">{L('比如模型显示名、自定义请求头', 'e.g. model display names, custom headers')}</span>
          </div>
          {extra.length > 0 && (
            <div className="rows">
              {extra.map((r) => (
                <div key={r.id} className="kv-row">
                  <TextInput
                    mono
                    className="kv-key wide"
                    value={r.a}
                    placeholder="ANTHROPIC_…"
                    onChange={(e) => setExtra((xs) => xs.map((x) => (x.id === r.id ? { ...x, a: e.target.value } : x)))}
                  />
                  <span className="faint">=</span>
                  <TextInput mono value={r.b} onChange={(e) => setExtra((xs) => xs.map((x) => (x.id === r.id ? { ...x, b: e.target.value } : x)))} />
                  <IconButton icon="x" size={16} label={L('删除', 'Remove')} onClick={() => setExtra((xs) => xs.filter((x) => x.id !== r.id))} />
                </div>
              ))}
            </div>
          )}
          <div className="chips-add">
            <button type="button" className="chip-add" onClick={() => setExtra((xs) => [...xs, row()])}>
              <Icon name="plus" size={13} />
              {L('添加 env', 'Add env')}
            </button>
          </div>
        </div>

        {error && (
          <div className="form-error shake" key={errorSeq}>
            <Icon name="alert" size={15} />
            {error}
          </div>
        )}
      </div>
    </Dialog>
  );
}
