// Offline false-positive check for hooks/lint.ts: runs lintReply over saved
// replies and prints, per rule, hit count, hit rate per reply and sample hits.
// No model calls.
//
//   bun eval/lintcheck.ts [transcript.jsonl ...]
//
// Inputs: every eval/out/*_N.txt reply, plus the assistant text blocks of each
// Claude Code transcript given (replies under 25 words are skipped).
import { readdirSync, readFileSync } from 'fs'
import { lintReply } from '../hooks/lint'
import type { Finding } from '../hooks/lint'

const OUT = import.meta.dir + '/out'
const MIN_WORDS = 25
const SAMPLES = 5

type Reply = { src: string; text: string }

function evalReplies(): Reply[] {
  return readdirSync(OUT)
    .filter(f => /^rule(style)?_.*_\d+\.txt$/.test(f))
    .sort()
    .map(f => ({ src: `eval/${f}`, text: readFileSync(`${OUT}/${f}`, 'utf8') }))
}

function transcriptReplies(path: string): Reply[] {
  const out: Reply[] = []
  for (const line of readFileSync(path, 'utf8').split('\n')) {
    if (line === '') continue
    let row: { type?: string; message?: { content?: unknown }; uuid?: string }
    try {
      row = JSON.parse(line)
    } catch {
      continue
    }
    if (row.type !== 'assistant' || !Array.isArray(row.message?.content)) continue
    const text = (row.message!.content as { type?: string; text?: string }[])
      .filter(b => b.type === 'text' && typeof b.text === 'string')
      .map(b => b.text as string)
      .join('\n')
    if (text.split(/\s+/).filter(Boolean).length >= MIN_WORDS) out.push({ src: `transcript ${row.uuid?.slice(0, 8) ?? '?'}`, text })
  }
  return out
}

/** Forty characters around the match, one line, for the sample table. */
function around(text: string, f: Finding): string {
  const s = Math.max(0, f.index - 40)
  return text.slice(s, f.index + f.match.length + 40).replace(/\s+/g, ' ').trim()
}

function report(name: string, replies: Reply[]): void {
  const byRule = new Map<string, { hits: number; replies: Set<string>; samples: string[] }>()
  let longHits = 0
  for (const r of replies) {
    const rep = lintReply(r.text, { mode: 'strict' })
    longHits += rep.longSentences
    for (const f of rep.findings) {
      const row = byRule.get(f.rule) ?? { hits: 0, replies: new Set(), samples: [] }
      row.hits += 1
      row.replies.add(r.src)
      if (row.samples.length < SAMPLES) row.samples.push(`[${r.src}] …${around(r.text, f)}…  (${f.match})`)
      byRule.set(f.rule, row)
    }
  }
  console.log(`\n## ${name}: ${replies.length} replies\n`)
  console.log('| rule | hits | replies hit | rate per reply |\n|---|---|---|---|')
  for (const [rule, row] of [...byRule.entries()].sort((a, b) => b[1].hits - a[1].hits)) {
    console.log(`| ${rule} | ${row.hits} | ${row.replies.size} | ${(row.hits / replies.length).toFixed(2)} |`)
  }
  console.log(`\n(long sentences counted: ${longHits})\n`)
  for (const [rule, row] of byRule) {
    console.log(`### ${rule}`)
    for (const s of row.samples) console.log(`- ${s}`)
    console.log('')
  }
}

report('eval replies', evalReplies())
for (const path of process.argv.slice(2)) report(`transcript ${path.split('/').pop()}`, transcriptReplies(path))
