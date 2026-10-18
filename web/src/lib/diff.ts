// A line diff for showing two versions side by side. Files here are small
// (settings, scripts), so a plain LCS table is fast enough.

export interface Cell {
  n: number;
  text: string;
}

export interface DiffRow {
  left?: Cell;
  right?: Cell;
  kind: 'same' | 'change' | 'del' | 'add';
}

export function sideBySide(a: string, b: string): DiffRow[] {
  const x = a === '' ? [] : a.replace(/\n$/, '').split('\n');
  const y = b === '' ? [] : b.replace(/\n$/, '').split('\n');
  const n = x.length;
  const m = y.length;
  // lcs[i][j]: length of the LCS of x[i:] and y[j:]
  const lcs: Uint32Array[] = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i][j] = x[i] === y[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  const rows: DiffRow[] = [];
  let dels: Cell[] = [];
  let adds: Cell[] = [];
  const flush = () => {
    const k = Math.max(dels.length, adds.length);
    for (let t = 0; t < k; t++) {
      const left = dels[t];
      const right = adds[t];
      rows.push({ left, right, kind: left && right ? 'change' : left ? 'del' : 'add' });
    }
    dels = [];
    adds = [];
  };
  let i = 0;
  let j = 0;
  while (i < n || j < m) {
    if (i < n && j < m && x[i] === y[j]) {
      flush();
      rows.push({ left: { n: i + 1, text: x[i] }, right: { n: j + 1, text: y[j] }, kind: 'same' });
      i++;
      j++;
    } else if (j < m && (i === n || lcs[i][j + 1] >= lcs[i + 1][j])) {
      adds.push({ n: j + 1, text: y[j] });
      j++;
    } else {
      dels.push({ n: i + 1, text: x[i] });
      i++;
    }
  }
  flush();
  return rows;
}

/**
 * fold keeps changed rows and `context` rows around them; longer runs of
 * unchanged rows collapse into a single gap marker (null).
 */
export function fold(rows: DiffRow[], context = 3): (DiffRow | null)[] {
  const keep = rows.map(() => false);
  rows.forEach((r, i) => {
    if (r.kind === 'same') return;
    for (let k = Math.max(0, i - context); k <= Math.min(rows.length - 1, i + context); k++) keep[k] = true;
  });
  const out: (DiffRow | null)[] = [];
  rows.forEach((r, i) => {
    if (keep[i]) out.push(r);
    else if (out.length === 0 || out[out.length - 1] !== null) out.push(null);
  });
  return out;
}
