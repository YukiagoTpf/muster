import type { Item } from './types';

type L = (zh: string, en: string) => string;

/** "2 分钟前" / "2 min ago" for a past time. */
export function ago(iso: string | undefined, now: number, L: L): string {
  if (!iso) return L('从未', 'never');
  const s = Math.max(0, Math.round((now - Date.parse(iso)) / 1000));
  if (s < 45) return L('刚刚', 'just now');
  const m = Math.round(s / 60);
  if (m < 60) return L(`${m} 分钟前`, `${m} min ago`);
  const h = Math.round(m / 60);
  if (h < 24) return L(`${h} 小时前`, `${h} h ago`);
  const d = Math.round(h / 24);
  return L(`${d} 天前`, `${d} d ago`);
}

const WEEKDAYS_ZH = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
const WEEKDAYS_EN = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

function hm(d: Date): string {
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/**
 * When a window resets: a countdown within a day ("3 小时 12 分后"), a weekday
 * and time within a week, a date beyond.
 */
export function resetsIn(iso: string | undefined, now: number, L: L): string {
  if (!iso) return '';
  const t = Date.parse(iso);
  const mins = Math.round((t - now) / 60000);
  if (mins <= 0) return L('已重置', 'reset');
  if (mins < 60) return L(`${mins} 分钟后重置`, `resets in ${mins}m`);
  if (mins < 24 * 60) {
    const h = Math.floor(mins / 60);
    const m = mins % 60;
    return L(`${h} 小时${m ? ` ${m} 分` : ''}后重置`, `resets in ${h}h${m ? ` ${m}m` : ''}`);
  }
  const d = new Date(t);
  if (mins < 7 * 24 * 60) return L(`${WEEKDAYS_ZH[d.getDay()]} ${hm(d)} 重置`, `resets ${WEEKDAYS_EN[d.getDay()]} ${hm(d)}`);
  return L(`${d.getMonth() + 1}月${d.getDate()}日重置`, `resets ${d.toLocaleDateString('en', { month: 'short', day: 'numeric' })}`);
}

export function money(usd: number): string {
  return '$' + usd.toFixed(2);
}

export function percent(used: number): string {
  return Math.round(Math.min(1, Math.max(0, used)) * 100) + '%';
}

/** How a quota window is named. */
export function windowName(kind: string, L: L): string {
  switch (kind) {
    case '5h':
      return L('5 小时', '5 hours');
    case 'week':
      return L('本周', 'Week');
    case 'month':
      return L('本月', 'Month');
  }
  return kind;
}

export interface PlatformMeta {
  name: string;
  mark: string;
  color: string;
}

const PLATFORMS: Record<string, PlatformMeta> = {
  claude: { name: 'Claude', mark: 'C', color: '#d97757' },
  codex: { name: 'Codex', mark: 'Cx', color: '#10a37f' },
  cursor: { name: 'Cursor', mark: 'Cu', color: '#7b61ff' },
  grok: { name: 'Grok', mark: 'G', color: '#5b6476' },
};

export function platformMeta(p: string): PlatformMeta {
  return PLATFORMS[p] ?? { name: p.charAt(0).toUpperCase() + p.slice(1), mark: p.charAt(0).toUpperCase(), color: '#6b7280' };
}

export function planName(plan?: string): string {
  if (!plan) return '';
  return plan.charAt(0).toUpperCase() + plan.slice(1);
}

/** A sync source's title: "AsterGate · claude-pool" or "Codex · 本机登录". */
export function sourceTitle(source: string, L: L): string {
  const [kind, name] = source.split(':');
  if (kind === 'astergate') return 'AsterGate · ' + name;
  return platformMeta(kind).name + ' · ' + L('本机登录', 'local login');
}

export function platformName(p: string): string {
  return { windows: 'Windows', macos: 'macOS', linux: 'Linux' }[p] ?? p;
}

export function itemLabel(it: Item, L: L): string {
  switch (it.kind) {
    case 'settings':
      return L(`共享设置（${platformName(it.name)}）`, `Shared settings (${platformName(it.name)})`);
    case 'setup':
      return L(`装机清单（${platformName(it.name)}）`, `Setup list (${platformName(it.name)})`);
    case 'credential':
      return L(`凭据 ${it.name}`, `Credential ${it.name}`);
  }
  return it.name;
}

export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}
