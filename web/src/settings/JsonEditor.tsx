import { useMemo, useRef, type KeyboardEvent } from 'react';
import { useLang } from '../lib/i18n';
import { Icon } from '../ui/Icon';

// A plain textarea with a highlighted copy of its text drawn underneath:
// the textarea's own text is transparent, so typing, selection, undo and
// IME all stay native.

const TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)|([{}[\],])/g;

function escape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;');
}

function highlight(text: string): string {
  let out = '';
  let last = 0;
  for (const m of text.matchAll(TOKEN)) {
    out += escape(text.slice(last, m.index));
    if (m[1]) out += `<span class="${m[2] ? 'j-key' : 'j-str'}">${escape(m[1])}</span>${m[2] ?? ''}`;
    else if (m[3]) out += `<span class="j-lit">${m[3]}</span>`;
    else if (m[4]) out += `<span class="j-num">${m[4]}</span>`;
    else out += `<span class="j-punct">${m[5]}</span>`;
    last = m.index! + m[0].length;
  }
  return out + escape(text.slice(last)) + '\n';
}

/** parseError says where JSON.parse gave up, as a line, or "" when it parses to an object. */
export function parseError(text: string, L: (zh: string, en: string) => string): string {
  try {
    const v = JSON.parse(text);
    if (typeof v !== 'object' || v === null || Array.isArray(v)) return L('最外层必须是一个对象 { … }', 'The top level must be an object { … }');
    return '';
  } catch (e) {
    const msg = (e as Error).message;
    const pos = /position (\d+)/.exec(msg);
    if (pos) {
      const line = text.slice(0, Number(pos[1])).split('\n').length;
      return L(`第 ${line} 行：`, `Line ${line}: `) + msg.replace(/ in JSON at position \d+.*/, '');
    }
    return msg;
  }
}

export function JsonEditor({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  const { L } = useLang();
  const area = useRef<HTMLTextAreaElement>(null);
  const under = useRef<HTMLPreElement>(null);
  const gutter = useRef<HTMLDivElement>(null);
  const html = useMemo(() => highlight(value), [value]);
  const lines = useMemo(() => value.split('\n').length, [value]);
  const error = useMemo(() => parseError(value, L), [value, L]);

  const sync = () => {
    const a = area.current!;
    under.current!.style.transform = `translate(${-a.scrollLeft}px, ${-a.scrollTop}px)`;
    gutter.current!.style.transform = `translateY(${-a.scrollTop}px)`;
  };

  // Tab indents by two spaces instead of leaving the editor.
  const key = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== 'Tab') return;
    e.preventDefault();
    const a = e.currentTarget;
    const { selectionStart: s, selectionEnd: t } = a;
    document.execCommand('insertText', false, '  ');
    if (s === t) a.setSelectionRange(s + 2, s + 2);
  };

  return (
    <div className={'json-editor' + (error ? ' has-error' : '')}>
      <div className="json-frame">
        <div className="json-gutter" aria-hidden>
          <div ref={gutter}>
            {Array.from({ length: lines }, (_, i) => (
              <div key={i}>{i + 1}</div>
            ))}
          </div>
        </div>
        <div className="json-area">
          <pre ref={under} className="json-under" aria-hidden dangerouslySetInnerHTML={{ __html: html }} />
          <textarea
            ref={area}
            className="json-input"
            value={value}
            spellCheck={false}
            autoCapitalize="off"
            autoComplete="off"
            wrap="off"
            onChange={(e) => onChange(e.target.value)}
            onScroll={sync}
            onKeyDown={key}
          />
        </div>
      </div>
      <div className={'json-status' + (error ? ' bad' : ' good')}>
        <Icon name={error ? 'alert' : 'check'} size={14} strokeWidth={2.2} />
        {error || L('JSON 格式正确', 'Valid JSON')}
      </div>
    </div>
  );
}
