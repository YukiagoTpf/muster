import { useCallback, useEffect, useRef } from 'react';
import { PageHeader, Section, useApp } from '../app/App';
import { api } from '../lib/api';
import { ago, money, percent, planName, platformMeta, resetsIn, sourceTitle, windowName } from '../lib/format';
import { useBusy, useNow, useResource } from '../lib/hooks';
import { useLang } from '../lib/i18n';
import type { Account, QuotaView, Usage } from '../lib/types';
import { AnimatedText } from '../motion/AnimatedText';
import { Badge, Bar, Button, Empty, Notice, Skeleton } from '../ui/controls';
import { useToast } from '../ui/overlays';

const PLATFORM_ORDER = ['claude', 'codex', 'cursor', 'grok'];

/** pressure is how close an account is to a limit: its fullest live window. */
function pressure(a: Account, now: number): number {
  let p = 0;
  for (const w of a.windows) {
    if (w.resetsAt && Date.parse(w.resetsAt) <= now) continue;
    p = Math.max(p, w.used);
  }
  return p;
}

function sortAccounts(accounts: Account[], now: number): Account[] {
  const rank = (p: string) => {
    const i = PLATFORM_ORDER.indexOf(p);
    return i < 0 ? PLATFORM_ORDER.length : i;
  };
  return [...accounts].sort((a, b) => rank(a.platform) - rank(b.platform) || pressure(a, now) - pressure(b, now));
}

export function QuotaPage() {
  const { L } = useLang();
  const toast = useToast();
  const { navigate } = useApp();
  const now = useNow(20_000);
  const quota = useResource(api.quota);
  const [refreshing, track] = useBusy();
  const autoRefreshed = useRef(false);

  const refresh = useCallback(
    async (quiet: boolean) => {
      try {
        quota.set(await track(api.refreshQuota()));
        if (!quiet) toast('ok', L('额度已刷新', 'Quota refreshed'));
      } catch (e) {
        toast('error', (e as Error).message);
      }
    },
    [quota, track, toast, L],
  );

  // Show what the cache has at once; if it is old, fetch in the background.
  useEffect(() => {
    if (quota.data?.stale && !autoRefreshed.current) {
      autoRefreshed.current = true;
      refresh(true);
    }
  }, [quota.data, refresh]);

  const data = quota.data;
  return (
    <div className="content">
      <PageHeader
        title={L('额度', 'Quota')}
        sub={
          <>
            <span>{L('所有订阅账号还剩多少', 'How much every subscription account has left')}</span>
            {data?.fetchedAt && (
              <>
                <span className="dot" />
                <span className={'faint' + (refreshing ? ' pulse' : '')}>
                  {refreshing ? L('正在刷新…', 'Refreshing…') : L(`${ago(data.fetchedAt, now, L)}更新`, `updated ${ago(data.fetchedAt, now, L)}`)}
                </span>
              </>
            )}
          </>
        }
        actions={
          <Button icon="refresh" busy={refreshing} onClick={() => refresh(false)}>
            {L('刷新', 'Refresh')}
          </Button>
        }
      />
      {quota.error ? (
        <Notice tone="danger" title={L('读不到额度', "Couldn't read the quota")}>
          {quota.error.message}
        </Notice>
      ) : !data ? (
        <QuotaSkeleton />
      ) : data.accounts.length === 0 && data.errors.length === 0 && Object.keys(data.usage).length === 0 ? (
        refreshing ? (
          <QuotaSkeleton />
        ) : (
          <Empty icon="gauge" title={L('还没有额度来源', 'No quota sources yet')}>
            {L(
              '登记一个网关凭据，或者在这台机器上登录 Codex、Cursor、Grok 的客户端，它们的额度就会出现在这里。',
              'Register a gateway credential, or log in to the Codex, Cursor or Grok client on this machine, and their quota shows up here.',
            )}
            <div style={{ marginTop: 16 }}>
              <Button variant="primary" icon="key" onClick={() => navigate('credentials')}>
                {L('去登记凭据', 'Register a credential')}
              </Button>
            </div>
          </Empty>
        )
      ) : (
        <QuotaBody data={data} now={now} />
      )}
    </div>
  );
}

function QuotaBody({ data, now }: { data: QuotaView; now: number }) {
  const { L } = useLang();
  const accounts = sortAccounts(data.accounts, now);
  const tight = accounts.filter((a) => pressure(a, now) >= 0.85).length;
  const spend = Object.values(data.usage);
  const today = spend.reduce((s, u) => s + u.today.costUSD, 0);
  const month = spend.reduce((s, u) => s + u.month.costUSD, 0);
  return (
    <>
      <div className="tiles stagger">
        <Tile label={L('订阅账号', 'Subscription accounts')} value={String(accounts.length)} />
        <Tile label={L('接近上限', 'Near a limit')} value={String(tight)} tone={tight > 0 ? 'warn' : undefined} />
        <Tile label={L('今日花费', 'Spent today')} value={money(today)} />
        <Tile label={L('本月花费', 'Spent this month')} value={money(month)} />
      </div>

      {data.errors.length > 0 && (
        <div className="stack" style={{ marginTop: 18 }}>
          {data.errors.map((e) => (
            <Notice key={e.source} tone="warn" title={sourceTitle(e.source, L)}>
              {e.error}
            </Notice>
          ))}
        </div>
      )}

      {accounts.length > 0 && (
        <Section title={L('订阅账号', 'Subscription accounts')}>
          <div className="accounts stagger">
            {accounts.map((a) => (
              <AccountCard key={a.platform + ':' + (a.id ?? a.via?.join(','))} a={a} now={now} />
            ))}
          </div>
        </Section>
      )}

      {spend.length > 0 && (
        <Section title={L('凭据花费', 'What each credential spent')}>
          <div className="card spend stagger">
            {Object.entries(data.usage)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([name, u]) => (
                <SpendRow key={name} name={name} u={u} />
              ))}
          </div>
        </Section>
      )}
    </>
  );
}

function Tile({ label, value, tone }: { label: string; value: string; tone?: 'warn' }) {
  return (
    <div className={'tile card' + (tone ? ' tile-' + tone : '')}>
      <div className="tile-label">{label}</div>
      <div className="tile-value num">
        <AnimatedText value={value} />
      </div>
    </div>
  );
}

function AccountCard({ a, now }: { a: Account; now: number }) {
  const { L } = useLang();
  const meta = platformMeta(a.platform);
  const sub = [a.id ? a.id.slice(0, 8) : '', a.via?.length ? 'via ' + a.via.join(', ') : ''].filter(Boolean).join(' · ');
  return (
    <div className="card acct">
      <div className="acct-head">
        <span className="avatar" style={{ ['--c' as string]: meta.color }}>
          {meta.mark}
        </span>
        <div className="acct-title">
          <div className="acct-name">
            <span>{meta.name}</span>
            {a.plan && <span className="plan">{planName(a.plan)}</span>}
          </div>
          {sub && <div className="acct-sub ellipsis">{sub}</div>}
        </div>
        <div className="acct-badges">
          {a.current && <Badge tone="accent">{L('当前', 'current')}</Badge>}
          {a.state && <Badge tone="warn">{a.state}</Badge>}
        </div>
      </div>
      <div className="acct-windows">
        {a.windows.length === 0 && <div className="faint acct-none">{L('还没观察到额度窗口', 'No quota windows observed yet')}</div>}
        {a.windows.map((w) => {
          const expired = !!w.resetsAt && Date.parse(w.resetsAt) <= now;
          return (
            <div key={w.kind} className="win">
              <div className="win-top">
                <span className="win-name">{windowName(w.kind, L)}</span>
                <span className={'win-pct num' + (expired ? ' faint' : w.used >= 0.85 ? ' danger' : w.used >= 0.6 ? ' warn' : '')}>
                  {expired ? L('已重置', 'reset') : <AnimatedText value={percent(w.used)} />}
                </span>
              </div>
              <Bar value={expired ? 0 : w.used} />
              {!expired && w.resetsAt && <div className="win-reset">{resetsIn(w.resetsAt, now, L)}</div>}
            </div>
          );
        })}
      </div>
      {a.note && <div className="acct-note">{a.note}</div>}
      {a.checkedAt && <div className="acct-foot">{L(`${ago(a.checkedAt, now, L)}检查`, `checked ${ago(a.checkedAt, now, L)}`)}</div>}
    </div>
  );
}

function SpendRow({ name, u }: { name: string; u: Usage }) {
  const { L } = useLang();
  return (
    <div className="spend-row">
      <div className="spend-name">
        <span className="spend-dot" />
        {name}
      </div>
      <div className="spend-cells">
        <div className="spend-cell">
          <span className="spend-label">{L('今日', 'Today')}</span>
          <span className="spend-value num">
            <AnimatedText value={money(u.today.costUSD)} />
          </span>
          <span className="spend-req num">{L(`${u.today.requests} 次请求`, `${u.today.requests} requests`)}</span>
        </div>
        <div className="spend-cell">
          <span className="spend-label">{L('本周', 'Week')}</span>
          <span className="spend-value num">
            <AnimatedText value={money(u.week.costUSD)} />
          </span>
        </div>
        <div className="spend-cell">
          <span className="spend-label">{L('本月', 'Month')}</span>
          <span className="spend-value num">
            <AnimatedText value={money(u.month.costUSD)} />
          </span>
        </div>
      </div>
    </div>
  );
}

function QuotaSkeleton() {
  return (
    <>
      <div className="tiles">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="tile card">
            <Skeleton width={90} height={12} />
            <Skeleton width={60} height={26} />
          </div>
        ))}
      </div>
      <div className="accounts" style={{ marginTop: 40 }}>
        {[0, 1, 2].map((i) => (
          <div key={i} className="card acct">
            <div className="acct-head">
              <Skeleton width={40} height={40} round />
              <div className="acct-title" style={{ gap: 8, display: 'flex', flexDirection: 'column' }}>
                <Skeleton width={120} height={14} />
                <Skeleton width={80} height={11} />
              </div>
            </div>
            <div className="acct-windows">
              <Skeleton width="100%" height={8} />
              <Skeleton width="100%" height={8} />
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
