import type {
  CredentialBody,
  Credential,
  CredentialsView,
  QuotaView,
  SettingsView,
  SettingsDoc,
  SetupEntry,
  Sides,
  StateView,
  StreamEvent,
  SyncResult,
  SyncView,
  Item,
} from './types';

// The session token arrives once, in the #fragment of the address muster
// opened, and lives in this tab's session storage from then on.
const TOKEN_KEY = 'muster.token';

export function takeToken(): boolean {
  const m = /(?:^#|&)token=([0-9a-f]+)/.exec(location.hash);
  if (m) {
    sessionStorage.setItem(TOKEN_KEY, m[1]);
    history.replaceState(null, '', location.pathname + '#/quota');
  }
  return !!sessionStorage.getItem(TOKEN_KEY);
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

export const UNAUTHORIZED = 'muster:unauthorized';

function headers(): HeadersInit {
  return {
    Authorization: 'Bearer ' + (sessionStorage.getItem(TOKEN_KEY) ?? ''),
    'Content-Type': 'application/json',
  };
}

async function failure(res: Response): Promise<never> {
  let message = res.statusText;
  try {
    message = (await res.json()).error ?? message;
  } catch {
    /* not JSON */
  }
  if (res.status === 401) window.dispatchEvent(new Event(UNAUTHORIZED));
  throw new ApiError(res.status, message);
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { method, headers: headers(), body: body === undefined ? undefined : JSON.stringify(body) });
  } catch {
    throw new ApiError(0, 'muster is not running: start `muster web` again');
  }
  if (!res.ok) return failure(res);
  return res.json() as Promise<T>;
}

/** stream posts and hands every NDJSON event to onEvent as it arrives. */
async function stream(path: string, body: unknown, onEvent: (e: StreamEvent) => void): Promise<void> {
  const res = await fetch(path, { method: 'POST', headers: headers(), body: JSON.stringify(body) });
  if (!res.ok || !res.body) return failure(res);
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  let buf = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += value;
    let nl: number;
    while ((nl = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, nl).trim();
      buf = buf.slice(nl + 1);
      if (line) onEvent(JSON.parse(line) as StreamEvent);
    }
  }
}

const enc = encodeURIComponent;

export const api = {
  state: () => request<StateView>('GET', '/api/state'),

  quota: () => request<QuotaView>('GET', '/api/quota'),
  refreshQuota: () => request<QuotaView>('POST', '/api/quota/refresh'),

  credentials: () => request<CredentialsView>('GET', '/api/credentials'),
  addCredential: (b: CredentialBody) => request<{ credential: Credential }>('POST', '/api/credentials', b),
  importCredential: (name: string, provider: string) =>
    request<{ credential: Credential }>('POST', '/api/credentials/import', { name, provider }),
  editCredential: (name: string, b: CredentialBody) =>
    request<{ credential: Credential }>('PUT', '/api/credentials/' + enc(name), b),
  removeCredential: (name: string) => request<unknown>('DELETE', '/api/credentials/' + enc(name)),
  setDefault: (credential: string) => request<{ backup?: string }>('POST', '/api/default', { credential }),

  sync: () => request<SyncView>('GET', '/api/sync'),
  runSync: (prefer?: 'local' | 'remote') => request<SyncResult>('POST', '/api/sync', { prefer: prefer ?? '' }),
  sides: (it: Item) => request<Sides>('GET', `/api/sync/sides?kind=${enc(it.kind)}&name=${enc(it.name)}`),
  track: (path: string) => request<{ tracked: string[] }>('POST', '/api/tracked', { path }),
  untrack: (path: string) => request<{ tracked: string[] }>('DELETE', '/api/tracked', { path }),

  setup: () => request<{ platform: string; entries: SetupEntry[] }>('GET', '/api/setup'),
  addSetup: (e: Omit<SetupEntry, 'installed'>) => request<unknown>('POST', '/api/setup', e),
  removeSetup: (name: string) => request<unknown>('DELETE', '/api/setup/' + enc(name)),
  installSetup: (names: string[], force: boolean, onEvent: (e: StreamEvent) => void) =>
    stream('/api/setup/install', { names, force }, onEvent),
  updateSetup: (names: string[], onEvent: (e: StreamEvent) => void) => stream('/api/setup/update', { names }, onEvent),

  settings: (platform?: string) =>
    request<SettingsView>('GET', '/api/settings' + (platform ? '?platform=' + enc(platform) : '')),
  saveSettings: (platform: string, content: string, hash: string) =>
    request<{ doc: SettingsDoc }>('PUT', '/api/settings', { platform, content, hash }),
};
