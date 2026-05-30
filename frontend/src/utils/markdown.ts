import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkGfm from 'remark-gfm';
import remarkRehype from 'remark-rehype';
import rehypeStringify from 'rehype-stringify';

const processor = unified()
  .use(remarkParse)
  .use(remarkGfm)
  .use(remarkRehype)
  .use(rehypeStringify);

export function renderMarkdown(text: string): string {
  return processor.processSync(text).toString();
}

// ---------------------------------------------------------------------------
// Content parts
// ---------------------------------------------------------------------------

export interface ToolUsePart { type: 'tool_use'; name: string; args: Record<string, unknown> | null; result: string | null }
export interface TextPart { type: 'text'; content: string }
export interface ThoughtPart { type: 'thought'; title: string; content: string }
export type ContentPart = TextPart | ThoughtPart | ToolUsePart;

function parseToolUse(raw: string): ToolUsePart {
  try {
    const data = JSON.parse(raw);
    return { type: 'tool_use', name: data.name ?? raw.trim(), args: data.args ?? null, result: data.result ?? null };
  } catch {
    return { type: 'tool_use', name: raw.trim(), args: null, result: null };
  }
}

// Returns ranges [start, end) of code blocks and inline code spans.
// Tag matches inside these ranges should be ignored.
// Uses a line-by-line approach so unclosed fences (mid-stream) protect to EOF.
function codeRanges(content: string): Array<[number, number]> {
  const ranges: Array<[number, number]> = [];
  const lines = content.split('\n');
  let inFence = false;
  let fenceChar = '';
  let fenceMinLen = 0;
  let fenceStart = 0;
  let pos = 0;

  for (const line of lines) {
    const trimmed = line.trimStart();
    if (!inFence) {
      const fm = trimmed.match(/^(`{3,}|~{3,})/);
      if (fm?.[1]) {
        inFence = true;
        fenceChar = fm[1][0];
        fenceMinLen = fm[1].length;
        fenceStart = pos;
      }
    } else {
      const closingRe = new RegExp(`^${fenceChar === '`' ? '`' : '~'}{${fenceMinLen},}\\s*$`);
      if (closingRe.test(trimmed)) {
        ranges.push([fenceStart, pos + line.length + 1]);
        inFence = false;
      }
    }
    pos += line.length + 1; // +1 for the \n
  }

  // Unclosed fence (e.g. mid-stream) — protect to end of content
  if (inFence) ranges.push([fenceStart, content.length]);

  // Inline code spans
  const inline = /`+[^`\n]+`+/g;
  let m;
  while ((m = inline.exec(content)) !== null) {
    if (!ranges.some(([s, e]) => m!.index >= s && m!.index < e)) {
      ranges.push([m.index, m.index + m[0].length]);
    }
  }
  return ranges;
}

function inCode(index: number, ranges: Array<[number, number]>): boolean {
  return ranges.some(([s, e]) => index >= s && index < e);
}

// Find the next occurrence of `tag` starting at `from` that is not inside a code range.
function nextTagOutsideCode(content: string, tag: string, from: number, protected_: Array<[number, number]>): number {
  let i = from;
  while (i < content.length) {
    const idx = content.indexOf(tag, i);
    if (idx === -1) return -1;
    if (!inCode(idx, protected_)) return idx;
    i = idx + 1;
  }
  return -1;
}

export function parseContent(content: string, allowPartialThought = false): ContentPart[] {
  const parts: ContentPart[] = [];
  const protected_ = codeRanges(content);
  let pos = 0;

  while (pos < content.length) {
    // Find next opening tag outside code
    const thoughtOpen = nextTagOutsideCode(content, '<thought>', pos, protected_);
    const toolOpen = nextTagOutsideCode(content, '<tool_use>', pos, protected_);

    // Pick whichever comes first
    let nextOpen = -1;
    let tagType: 'thought' | 'tool_use' | null = null;
    if (thoughtOpen !== -1 && (toolOpen === -1 || thoughtOpen <= toolOpen)) {
      nextOpen = thoughtOpen; tagType = 'thought';
    } else if (toolOpen !== -1) {
      nextOpen = toolOpen; tagType = 'tool_use';
    }

    if (nextOpen === -1) break;

    const openTag = tagType === 'thought' ? '<thought>' : '<tool_use>';
    const closeTag = tagType === 'thought' ? '</thought>' : '</tool_use>';
    const closeIdx = nextTagOutsideCode(content, closeTag, nextOpen + openTag.length, protected_);

    if (closeIdx === -1) break; // no closing tag outside code — treat as remainder

    if (nextOpen > pos) parts.push({ type: 'text', content: content.slice(pos, nextOpen) });

    const inner = content.slice(nextOpen + openTag.length, closeIdx);
    if (tagType === 'thought') {
      parts.push({ type: 'thought', title: 'Thought', content: inner });
    } else {
      parts.push(parseToolUse(inner));
    }
    pos = closeIdx + closeTag.length;
  }

  const remainder = content.slice(pos);

  if (allowPartialThought) {
    // Check for an unclosed <thought> outside code in the remainder
    const openIdx = nextTagOutsideCode(content, '<thought>', pos, protected_);
    if (openIdx !== -1) {
      const beforeOpen = content.slice(pos, openIdx);
      if (beforeOpen.length > 0) parts.push({ type: 'text', content: beforeOpen });
      parts.push({ type: 'thought', title: 'Thinking...', content: content.slice(openIdx + '<thought>'.length) });
      return parts;
    }
  }

  if (remainder.length > 0) parts.push({ type: 'text', content: remainder });
  return parts;
}
