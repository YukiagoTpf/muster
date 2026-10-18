import { useState } from 'react';
import { PageHeader, Section } from '../app/App';
import { api } from '../lib/api';
import { hostOf } from '../lib/format';
import { useBusy, useResource } from '../lib/hooks';
import { useLang } from '../lib/i18n';
import type { Credential, CredentialsView, DefaultState } from '../lib/types';
import { Ripple } from '../motion/Ripple';
import { Badge, Button, Empty, Field, IconButton, Notice, Skeleton, TextInput } from '../ui/controls';
import { Icon } from '../ui/Icon';
import { Dialog, Menu, useConfirm, useToast } from '../ui/overlays';
import { CredentialDialog } from './CredentialDialog';

export function CredentialsPage() {
  const { L } = useLang();
  const toast = useToast();
  const creds = useResource(api.credentials);
  const [editing, setEditing] = useState<Credential | 'new' | null>(null);
  const [importing, setImporting] = useState(false);
  const [confirmNode, confirm] = useConfirm();

  const makeDefault = async (name: string) => {
    try {
      await api.setDefault(name);
      toast('ok', L(`默认凭据已切换为 ${name}`, `${name} is now the default credential`));
      creds.reload();
    } catch (e) {
      toast('error', (e as Error).message);
    }
  };

  const remove = async (c: Credential) => {
    const ok = await confirm({
      title: L(`删除凭据 ${c.name}？`, `Remove credential ${c.name}?`),
      body: L(
        '它的 key 会从这台机器上删掉。下次同步时，其他机器上的这个凭据也会被删除。',
        'Its key is deleted from this machine, and the next sync removes it from your other machines too.',
      ),
      confirm: L('删除', 'Remove'),
      danger: true,
    });
    if (!ok) return;
    try {
      await api.removeCredential(c.name);
      toast('ok', L(`已删除 ${c.name}`, `Removed ${c.name}`));
      creds.reload();
    } catch (e) {
      toast('error', (e as Error).message);
    }
  };

  const data = creds.data;
  return (
    <div className="content">
      <PageHeader
        title={L('凭据', 'Credentials')}
        sub={L('每个凭据是一个供应商的地址、key 和模型映射；启动 Claude Code 时选一个', 'A credential is a provider’s address, key and model mapping; every launch picks one')}
        actions={
          <Button variant="primary" icon="plus" onClick={() => setEditing('new')}>
            {L('添加凭据', 'Add credential')}
          </Button>
        }
      />
      {creds.error && (
        <Notice tone="danger" title={L('读不到凭据', "Couldn't read the credentials")}>
          {creds.error.message}
        </Notice>
      )}
      {!data && !creds.error && <CredentialsSkeleton />}
      {data && (
        <>
          <DefaultBanner data={data} onMakeDefault={makeDefault} onImport={() => setImporting(true)} />
          <Section title={L('已登记', 'Registered')} aside={data.credentials.length > 0 ? <span className="num">{data.credentials.length}</span> : undefined}>
            {data.credentials.length === 0 ? (
              <div className="card">
                <Empty icon="key" title={L('还没有凭据', 'No credentials yet')}>
                  {L('添加一个供应商的地址和 key，或者把 settings.json 里正在用的那个登记下来。', 'Add a provider’s address and key, or register the one settings.json already uses.')}
                </Empty>
              </div>
            ) : (
              <div className="creds stagger">
                {data.credentials.map((c) => (
                  <CredentialCard
                    key={c.name}
                    c={c}
                    isDefault={data.default.state === 'match' && data.default.credential === c.name}
                    onEdit={() => setEditing(c)}
                    onDefault={() => makeDefault(c.name)}
                    onRemove={() => remove(c)}
                  />
                ))}
              </div>
            )}
          </Section>
        </>
      )}
      <CredentialDialog
        open={editing !== null}
        editing={editing === 'new' ? null : editing}
        onClose={() => setEditing(null)}
        onSaved={(name, created) => {
          setEditing(null);
          toast('ok', created ? L(`已添加 ${name}`, `Added ${name}`) : L(`已保存 ${name}`, `Saved ${name}`));
          creds.reload();
        }}
      />
      <ImportDialog
        open={importing}
        host={data?.default.baseURL}
        onClose={() => setImporting(false)}
        onDone={(name) => {
          setImporting(false);
          toast('ok', L(`已登记 ${name}`, `Registered ${name}`));
          creds.reload();
        }}
      />
      {confirmNode}
    </div>
  );
}

function DefaultBanner({
  data,
  onMakeDefault,
  onImport,
}: {
  data: CredentialsView;
  onMakeDefault: (name: string) => void;
  onImport: () => void;
}) {
  const { L } = useLang();
  const d: DefaultState = data.default;
  const explain = L('直接敲 claude 和 IDE 插件用的就是默认凭据。', 'Plain `claude` and IDE plugins use the default credential.');
  switch (d.state) {
    case 'match':
      return (
        <div className="default-banner card">
          <span className="default-star">
            <Icon name="star" size={18} />
          </span>
          <div className="grow">
            <div className="default-title">
              {L('默认凭据', 'Default credential')} <strong>{d.credential}</strong>
            </div>
            <div className="faint default-text">{explain}</div>
          </div>
        </div>
      );
    case 'drift':
      return (
        <Notice
          tone="warn"
          title={L(`默认凭据是 ${d.credential}，但有几项和登记的不一致`, `The default is ${d.credential}, but some keys differ from it`)}
          action={
            <Button size="sm" onClick={() => onMakeDefault(d.credential!)}>
              {L('按登记的重设', 'Reset to it')}
            </Button>
          }
        >
          <span className="mono">{d.differentKeys?.join('  ')}</span>
        </Notice>
      );
    case 'unregistered':
      return (
        <Notice
          tone="info"
          title={L('settings.json 正在用一个还没登记的供应商', 'settings.json uses a provider that is not registered')}
          action={
            <Button size="sm" variant="primary" onClick={onImport}>
              {L('登记为凭据', 'Register it')}
            </Button>
          }
        >
          <span className="mono">{d.baseURL}</span>
        </Notice>
      );
    default:
      return (
        <Notice tone="info" title={L('还没有默认凭据', 'No default credential')}>
          {L('Claude Code 直接启动时会用它自己的登录。在下面任一凭据的菜单里可以把它设为默认。', 'Claude Code started directly uses its own login. Make any credential below the default from its menu.')}
        </Notice>
      );
  }
}

function CredentialCard({
  c,
  isDefault,
  onEdit,
  onDefault,
  onRemove,
}: {
  c: Credential;
  isDefault: boolean;
  onEdit: () => void;
  onDefault: () => void;
  onRemove: () => void;
}) {
  const { L } = useLang();
  const extra = Object.keys(c.env).filter((k) => !/^ANTHROPIC_DEFAULT_[A-Z0-9]+_MODEL$/.test(k));
  return (
    <div className={'card cred' + (isDefault ? ' is-default' : '')}>
      <button type="button" className="cred-main" onClick={onEdit}>
        <Ripple />
        <div className="cred-head">
          <span className="cred-name">{c.name}</span>
          {isDefault && (
            <Badge tone="accent" icon="star">
              {L('默认', 'default')}
            </Badge>
          )}
          <Badge>{c.provider}</Badge>
        </div>
        <div className="cred-host mono">{hostOf(c.baseURL)}</div>
        <div className="cred-models">
          {c.models.length === 0 ? (
            <span className="faint">{L('没有模型映射：模型名原样交给供应商', 'No model mapping: model names go to the provider as they are')}</span>
          ) : (
            c.models.map((m) => (
              <span key={m.alias} className="model-chip">
                <span className="model-alias">{m.alias}</span>
                <Icon name="arrowRight" size={12} />
                <span className="mono">{m.model}</span>
              </span>
            ))
          )}
        </div>
        <div className="cred-foot">
          <span className={'cred-key' + (c.hasKey ? '' : ' missing')}>
            <Icon name={c.hasKey ? 'lock' : 'alert'} size={13} />
            {c.hasKey ? L('key 已保存', 'key stored') : L('没有 key', 'no key')}
          </span>
          {extra.length > 0 && <span className="faint">{L(`另有 ${extra.length} 个 env`, `${extra.length} more env`)}</span>}
        </div>
      </button>
      <div className="cred-menu">
        <Menu
          trigger={(p) => <IconButton {...p} icon="more" label={L('更多', 'More')} />}
          items={[
            { label: L('编辑', 'Edit'), icon: 'edit', onSelect: onEdit },
            { label: L('设为默认凭据', 'Make default'), icon: 'star', onSelect: onDefault, disabled: isDefault || !c.hasKey },
            { label: L('删除', 'Remove'), icon: 'trash', danger: true, onSelect: onRemove },
          ]}
        />
      </div>
    </div>
  );
}

function ImportDialog({
  open,
  host,
  onClose,
  onDone,
}: {
  open: boolean;
  host?: string;
  onClose: () => void;
  onDone: (name: string) => void;
}) {
  const { L } = useLang();
  const [name, setName] = useState('');
  const [provider, setProvider] = useState('astergate');
  const [error, setErrorText] = useState('');
  const [errorSeq, setErrorSeq] = useState(0);
  const setError = (e: string) => {
    setErrorText(e);
    if (e) setErrorSeq((n) => n + 1);
  };
  const [busy, run] = useBusy();
  const submit = async () => {
    setError('');
    try {
      await run(api.importCredential(name.trim(), provider.trim()));
      onDone(name.trim());
      setName('');
    } catch (e) {
      setError((e as Error).message);
    }
  };
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={L('把正在用的供应商登记为凭据', 'Register the provider in use')}
      width={460}
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            {L('取消', 'Cancel')}
          </Button>
          <Button variant="primary" busy={busy} disabled={!name.trim()} onClick={submit}>
            {L('登记', 'Register')}
          </Button>
        </>
      }
    >
      <div className="form">
        <p className="muted" style={{ margin: 0 }}>
          {L('地址、key 和模型映射都从 settings.json 里读，', 'The address, key and model mapping come from settings.json')}
          {host && <span className="mono"> {hostOf(host)}</span>}
          {L('；你只需要起个名字。', ' — just give it a name.')}
        </p>
        <Field label={L('名称', 'Name')}>
          <TextInput value={name} onChange={(e) => setName(e.target.value)} placeholder="claude-pool" onKeyDown={(e) => e.key === 'Enter' && name.trim() && submit()} />
        </Field>
        <Field label={L('供应商', 'Provider')} hint={L('AsterGate 网关填 astergate，它的额度会出现在额度页', 'astergate for an AsterGate gateway — its quota shows on the quota page')}>
          <TextInput value={provider} onChange={(e) => setProvider(e.target.value)} />
        </Field>
        {error && (
          <div className="form-error shake" key={errorSeq}>
            {error}
          </div>
        )}
      </div>
    </Dialog>
  );
}

function CredentialsSkeleton() {
  return (
    <div className="creds" style={{ marginTop: 28 }}>
      {[0, 1].map((i) => (
        <div key={i} className="card cred" style={{ padding: 20, display: 'flex', flexDirection: 'column', gap: 12 }}>
          <Skeleton width={140} height={16} />
          <Skeleton width={200} height={12} />
          <Skeleton width="80%" height={22} />
        </div>
      ))}
    </div>
  );
}
