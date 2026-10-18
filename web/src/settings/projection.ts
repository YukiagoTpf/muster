// The structured editor is a projection over the settings JSON, which stays
// the canonical value. Four rules keep it honest:
//
// 1. Own path only. A control reads and writes exactly one path; a key no
//    control names is never read, never written, never dropped.
// 2. Lock what you cannot express. A value whose shape a control cannot show
//    (a string where a boolean belongs) locks the control, which then writes
//    nothing; the JSON view is the way out.
// 3. Absence is a value. Unset is a state of its own: booleans are tri-state,
//    and clearing a field removes the key instead of writing a default.
// 4. Don't churn what nobody edited. Values are written only when a control
//    changes, key order is kept, and a container the editor created is
//    removed again when it ends up empty — but one the file had stays, empty.

export type Json = null | boolean | number | string | Json[] | JsonObject;
export interface JsonObject {
  [k: string]: Json;
}
export type Path = (string | number)[];

export const isObj = (v: unknown): v is JsonObject => typeof v === 'object' && v !== null && !Array.isArray(v);

export function get(doc: Json | undefined, path: Path): Json | undefined {
  let cur: Json | undefined = doc;
  for (const seg of path) {
    if (typeof seg === 'number') {
      if (!Array.isArray(cur)) return undefined;
      cur = cur[seg];
    } else {
      if (!isObj(cur) || !Object.prototype.hasOwnProperty.call(cur, seg)) return undefined;
      cur = cur[seg];
    }
  }
  return cur;
}

export const has = (doc: Json | undefined, path: Path) => get(doc, path) !== undefined;

/** withKey returns a copy of obj with key set, in place if it was there. */
function withKey(obj: JsonObject, key: string, value: Json): JsonObject {
  const entries = Object.entries(obj);
  const i = entries.findIndex(([k]) => k === key);
  if (i >= 0) entries[i] = [key, value];
  else entries.push([key, value]);
  // fromEntries defines own properties, so even "__proto__" stays a plain key.
  return Object.fromEntries(entries) as JsonObject;
}

function withoutKey(obj: JsonObject, key: string): JsonObject {
  return Object.fromEntries(Object.entries(obj).filter(([k]) => k !== key)) as JsonObject;
}

/** set writes value at path, creating the objects on the way. */
export function set(doc: JsonObject, path: Path, value: Json): JsonObject {
  return setIn(doc, path, value) as JsonObject;
}

function setIn(node: Json | undefined, path: Path, value: Json): Json {
  if (path.length === 0) return value;
  const [seg, ...rest] = path;
  if (typeof seg === 'number') {
    const arr = Array.isArray(node) ? [...node] : [];
    arr[seg] = setIn(arr[seg], rest, value);
    return arr;
  }
  const obj = isObj(node) ? node : {};
  return withKey(obj, seg, setIn(obj[seg], rest, value));
}

/**
 * del removes the value at path. Parents left empty are removed too, unless
 * the original file had them (rule 4).
 */
export function del(doc: JsonObject, path: Path, original: JsonObject): JsonObject {
  return (delIn(doc, path, original, []) ?? {}) as JsonObject;
}

function delIn(node: Json | undefined, path: Path, original: JsonObject, at: Path): Json | undefined {
  if (path.length === 0) return undefined;
  const [seg, ...rest] = path;
  let next: Json;
  if (typeof seg === 'number') {
    if (!Array.isArray(node)) return node;
    const arr = [...node];
    const child = delIn(arr[seg], rest, original, [...at, seg]);
    if (child === undefined) arr.splice(seg, 1);
    else arr[seg] = child;
    next = arr;
    if (arr.length === 0 && at.length > 0 && !has(original, at)) return undefined;
    return next;
  }
  if (!isObj(node) || !Object.prototype.hasOwnProperty.call(node, seg)) return node;
  const child = delIn(node[seg], rest, original, [...at, seg]);
  const obj = child === undefined ? withoutKey(node, seg) : withKey(node, seg, child);
  if (Object.keys(obj).length === 0 && at.length > 0 && !has(original, at)) return undefined;
  return obj;
}

/** renameKey renames a key of the object at path, keeping its place. */
export function renameKey(doc: JsonObject, path: Path, from: string, to: string): JsonObject {
  const obj = get(doc, path);
  if (!isObj(obj)) return doc;
  const entries = Object.entries(obj).map(([k, v]) => [k === from ? to : k, v] as [string, Json]);
  return set(doc, path, Object.fromEntries(entries) as JsonObject);
}

export function stringify(doc: Json): string {
  return JSON.stringify(doc, null, 2) + '\n';
}

/** isProviderKey mirrors Go's claude.IsProviderEnvKey. */
export function isProviderKey(k: string): boolean {
  if (k.startsWith('ANTHROPIC_')) return true;
  return k.startsWith('CLAUDE_CODE_') && (/(^|_)(TOKEN|KEY|AUTH|SECRET|BEARER|CREDENTIAL)(_|$)/i.test(k) || /^CLAUDE_CODE_USE_(BEDROCK|VERTEX|FOUNDRY)$/.test(k));
}

export const looksSecret = (k: string) => /(^|_)(TOKEN|KEY|AUTH|SECRET|BEARER|CREDENTIAL)(_|$)/i.test(k);
