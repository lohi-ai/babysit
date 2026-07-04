// Hand-written markdown subset. Output is trusted HTML — input is escaped first,
// then inline transforms run on already-escaped text. Supports: # ## ### ####
// headings, fenced code, inline code, *em*, **strong**, [link](url), unordered
// + ordered lists with one level of nesting, paragraphs, blockquotes, hr,
// GitHub-flavored pipe tables.

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

const URL_OK = /^(https?:|mailto:|#|\.?\.?\/)/;

function inline(s: string): string {
  let out = escapeHtml(s);
  out = out.replace(/`([^`]+?)`/g, (_m, c) => `<code>${c}</code>`);
  out = out.replace(/\[([^\]]+?)\]\(([^)\s]+)\)/g, (_m, t, u) => {
    const safe = URL_OK.test(u) ? u : '#';
    return `<a href="${safe}">${t}</a>`;
  });
  out = out.replace(/\*\*([^*\n]+?)\*\*/g, '<strong>$1</strong>');
  out = out.replace(/\*([^*\n]+?)\*/g, '<em>$1</em>');
  return out;
}

const BLOCK_START = /^(#{1,4}\s|>|```|\s*[-*+]\s|\s*\d+\.\s|\||-{3,}\s*$|\*{3,}\s*$|_{3,}\s*$)/;

function splitRow(line: string): string[] {
  return line.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map(c => c.trim());
}

function renderList(items: { level: number; ol: boolean; content: string }[]): string {
  let html = '';
  const outerTag = items[0].ol ? 'ol' : 'ul';
  let nestedTag = '';
  let curLevel = 0;
  const liOpen = [false, false];

  html += `<${outerTag}>`;
  for (const it of items) {
    if (it.level === 0) {
      if (curLevel === 1) {
        if (liOpen[1]) { html += '</li>'; liOpen[1] = false; }
        html += `</${nestedTag}>`;
        curLevel = 0;
      }
      if (liOpen[0]) { html += '</li>'; liOpen[0] = false; }
      html += `<li>${inline(it.content)}`;
      liOpen[0] = true;
    } else {
      if (curLevel === 0) {
        nestedTag = it.ol ? 'ol' : 'ul';
        html += `<${nestedTag}>`;
        curLevel = 1;
      } else if (liOpen[1]) {
        html += '</li>'; liOpen[1] = false;
      }
      html += `<li>${inline(it.content)}`;
      liOpen[1] = true;
    }
  }
  if (liOpen[1]) html += '</li>';
  if (curLevel === 1) html += `</${nestedTag}>`;
  if (liOpen[0]) html += '</li>';
  html += `</${outerTag}>`;
  return html;
}

export function renderMarkdown(md: string): string {
  const lines = md.replace(/\r\n?/g, '\n').split('\n');
  const out: string[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    if (/^\s*$/.test(line)) { i++; continue; }

    const h = /^(#{1,4})\s+(.+?)\s*$/.exec(line);
    if (h) {
      out.push(`<h${h[1].length}>${inline(h[2])}</h${h[1].length}>`);
      i++;
      continue;
    }

    if (/^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
      out.push('<hr />');
      i++;
      continue;
    }

    const fence = /^```\s*([\w+-]*)\s*$/.exec(line);
    if (fence) {
      const lang = fence[1];
      i++;
      const buf: string[] = [];
      while (i < lines.length && !/^```\s*$/.test(lines[i])) {
        buf.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++;
      const cls = lang ? ` class="language-${escapeHtml(lang)}"` : '';
      out.push(`<pre><code${cls}>${escapeHtml(buf.join('\n'))}</code></pre>`);
      continue;
    }

    if (/^>\s?/.test(line)) {
      const buf: string[] = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) {
        buf.push(lines[i].replace(/^>\s?/, ''));
        i++;
      }
      out.push(`<blockquote>${buf.map(inline).join('<br />')}</blockquote>`);
      continue;
    }

    if (/^\|.*\|\s*$/.test(line)
        && i + 1 < lines.length
        && /^\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$/.test(lines[i + 1])) {
      const headers = splitRow(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && /^\|.*\|\s*$/.test(lines[i])) {
        rows.push(splitRow(lines[i]));
        i++;
      }
      const thead = `<thead><tr>${headers.map(c => `<th>${inline(c)}</th>`).join('')}</tr></thead>`;
      const tbody = `<tbody>${rows.map(r => `<tr>${r.map(c => `<td>${inline(c)}</td>`).join('')}</tr>`).join('')}</tbody>`;
      out.push(`<table>${thead}${tbody}</table>`);
      continue;
    }

    const listM = /^(\s*)([-*+]|\d+\.)\s+(.+)$/.exec(line);
    if (listM) {
      const items: { level: number; ol: boolean; content: string }[] = [];
      const baseIndent = listM[1].length;
      while (i < lines.length) {
        const m = /^(\s*)([-*+]|\d+\.)\s+(.+)$/.exec(lines[i]);
        if (!m) break;
        items.push({
          level: m[1].length > baseIndent ? 1 : 0,
          ol: /\d+\./.test(m[2]),
          content: m[3],
        });
        i++;
      }
      out.push(renderList(items));
      continue;
    }

    const buf: string[] = [line];
    i++;
    while (i < lines.length && !/^\s*$/.test(lines[i]) && !BLOCK_START.test(lines[i])) {
      buf.push(lines[i]);
      i++;
    }
    out.push(`<p>${buf.map(inline).join(' ')}</p>`);
  }

  return out.join('\n');
}
