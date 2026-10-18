// The API's JSON, as the Go side writes it.

export interface QuotaWindow {
  kind: string;
  used: number;
  resetsAt?: string;
}

export interface Account {
  platform: string;
  id?: string;
  plan?: string;
  state?: string;
  windows: QuotaWindow[];
  checkedAt?: string;
  current?: boolean;
  via?: string[];
  note?: string;
}

export interface Spend {
  requests: number;
  costUSD: number;
}

export interface Usage {
  today: Spend;
  week: Spend;
  month: Spend;
}

export interface QuotaView {
  accounts: Account[];
  usage: Record<string, Usage>;
  errors: { source: string; error: string }[];
  fetchedAt?: string;
  stale: boolean;
}

export interface ModelAlias {
  alias: string;
  model: string;
}

export interface Credential {
  name: string;
  provider: string;
  baseURL: string;
  env: Record<string, string>;
  models: ModelAlias[];
  hasKey: boolean;
}

export interface DefaultState {
  state: 'match' | 'drift' | 'unregistered' | 'none';
  credential?: string;
  differentKeys?: string[];
  baseURL?: string;
}

export interface CredentialsView {
  credentials: Credential[];
  default: DefaultState;
  settings: string;
}

export interface CredentialBody {
  name?: string;
  provider: string;
  baseURL: string;
  env: Record<string, string>;
  key: string;
}

export type ItemKind = 'settings' | 'setup' | 'credential' | 'file';

export interface Item {
  kind: ItemKind;
  name: string;
}

export interface Change extends Item {
  change: 'added' | 'modified' | 'removed';
}

export type SyncView =
  | { setUp: false; platform: string }
  | { setUp: true; repo: string; platform: string; pending: Change[]; unpushed: boolean; tracked: string[] };

export interface SyncResult {
  sent: Item[];
  received: Item[];
  conflicts: Item[];
  notInstalled: string[];
}

export interface Sides extends Item {
  here: string;
  hereExists: boolean;
  there: string;
  thereExists: boolean;
}

export interface SetupEntry {
  name: string;
  install: string;
  update?: string;
  check?: string;
  installed: boolean;
}

export interface SetupResult {
  name: string;
  action: 'installed' | 'present' | 'updated' | 'failed' | 'skipped';
  exitCode?: number;
  reason?: string;
}

export interface StreamEvent {
  type: 'output' | 'result' | 'done';
  text?: string;
  result?: SetupResult;
  error?: string;
}

export interface SettingsDoc {
  platform: string;
  local: boolean;
  exists: boolean;
  path: string;
  content: string;
  hash: string;
  secretEnv: string[];
}

export interface SettingsView {
  doc: SettingsDoc;
  platforms: { platform: string; local: boolean }[];
}

export interface StateView {
  muster: string;
  platform: string;
  settings: string;
  synced: boolean;
  repo?: string;
}
