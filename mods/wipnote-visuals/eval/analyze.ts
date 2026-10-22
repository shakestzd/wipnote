// Summarise eval/out: block types emitted per prompt, parse success, steps marked done, lint tells.
// Run after eval/run.sh:  bun eval/analyze.ts
import { readdirSync, readFileSync } from 'fs'
import { parseSteps, splitBlocks } from '../hooks/blocks'
import { lintReply } from '../hooks/lint'

const OUT = import.meta.dir + '/out'
type Row = { cond: string; key: string; fences: string[]; parsed: number; words: number; tells: number; rules: Record<string, number>; repeats: boolean; done: number; steps: number }
const rows: Row[] = []

for (const f of readdirSync(OUT).filter(f => /^rule(style)?_.*\.txt$/.test(f)).sort()) {
  const [cond, key] = f.replace('.txt', '').split('_')
  const text = readFileSync(`${OUT}/${f}`, 'utf8')
  const fences = [...text.matchAll(/```wip:(\w+)/g)].map(m => m[1])
  const parsed = splitBlocks(text).filter(s => s.kind === 'block').length
  const report = lintReply(text, { mode: 'strict' })
  let done = 0
  let steps = 0
  for (const m of text.matchAll(/```wip:steps\n([\s\S]*?)```/g)) {
    const parsedSteps = parseSteps(m[1]) ?? []
    steps += parsedSteps.length
    done += parsedSteps.filter(s => s.state === 'done').length
  }
  const prose = text.replace(/```[\s\S]*?```/g, '')
  rows.push({
    cond, key, fences, parsed, steps, done,
    words: text.split(/\s+/).filter(Boolean).length,
    tells: report.findings.length,
    rules: report.counts,
    repeats: fences.includes('steps') && /^\s*(\*\*)?\d+\./m.test(prose),
  })
}

for (const cond of ['rule', 'rulestyle']) {
  const mine = rows.filter(r => r.cond === cond)
  if (mine.length === 0) continue
  console.log(`\n== ${cond} ==`)
  for (const key of [...new Set(mine.map(r => r.key))]) {
    const a = mine.filter(r => r.key === key)
    console.log(
      key.padEnd(8), 'fences:', a.map(r => r.fences.join('+') || '-').join(' | ').padEnd(34),
      'words:', a.map(r => r.words).join('/'), ' tells:', a.map(r => r.tells).join('/'),
      a.some(r => r.repeats) ? ' REPEATS-IN-PROSE' : '',
    )
  }
  const total: Record<string, number> = {}
  for (const r of mine) for (const [k, v] of Object.entries(r.rules)) total[k] = (total[k] ?? 0) + v
  const sum = (pick: (r: Row) => number) => mine.reduce((s, r) => s + pick(r), 0)
  console.log(
    'avg words', Math.round(sum(r => r.words) / mine.length),
    '| avg tells', (sum(r => r.tells) / mine.length).toFixed(1),
    '| steps marked done:', sum(r => r.done), 'of', sum(r => r.steps),
    '| unparsed fences:', sum(r => r.fences.length - r.parsed),
    '| rules fired:', JSON.stringify(total),
  )
}
