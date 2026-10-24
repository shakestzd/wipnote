// Pure writing lint for chat replies: AI tells and STE sentence limits.
// Rules are ported from luladoc styles/Zinsser/*.yml (see feat-de13460b).
// No engine or Node imports: runs inside the mod sandbox and in plain tests.

export type Finding = { rule: string; match: string; index: number; fix?: string; n?: number; para?: number }
export type Report = { findings: Finding[]; counts: Record<string, number>; longSentences: number; score: string }
export type Mode = 'strict' | '80' | 'off'

const VOCAB = new RegExp(
  '\\b(?:delve|delves|delving|pivotal|testament|tapestry|underscore|underscores|underscoring|showcase|showcases|showcasing|vibrant|crucial|fostering|intricate|intricacies|enduring|garner|garners|garnering|resonate|resonates|resonating|invaluable|ignited|igniting|embark|embarks|embarking|realm|beacon|honing|cutting-edge|seamless|holistic|esteemed|renowned|passion|passionate|empower|empowers|empowering|transformative|comprehensive understanding|requisite skills|high-impact|make a difference in people[\'’]s lives|perfect blend|well-equipped|uniquely positioned|game-changer)\\b' +
    // Context-sensitive entries: only the AI-tell sense, not literal uses.
    '|(?:the |a |an )?(?:competitive|educational|academic|professional|digital|technological|regulatory|political|economic|healthcare|corporate|research|media|cultural)\\s+landscape\\b' +
    '|(?:academic|personal|professional|educational|career|life|spiritual|entrepreneurial|leadership|research|learning|growth)\\s+journey\\b' +
    '|journey (?:to become|towards|toward|of self-discovery|of growth|of learning)' +
    '|dynamic\\s+(?:individual|leader|team|environment|person|professional|thinker|duo|place to work)\\b' +
    '|leverag(?:ing|ed)\\b|leverage\\s+(?:my|our|your|this|that|these|those|the)\\b',
  'gi',
)
const OPENER_PHRASE = /^(?:great question|certainly!|absolutely!)/i
const FILLER = /\b(?:in order to|due to the fact that|it is important to note that|in conclusion)\b/gi
const FILLER_FIX: Record<string, string> = {
  'in order to': 'to',
  'due to the fact that': 'because',
  'it is important to note that': '(delete)',
  'in conclusion': '(delete)',
}
const PUNCT = /\s–|–\s|[A-Za-z]–[A-Za-z]|[‘’“”]/g
const NEGATOR = /\b(?:does not|do not|did not|is not|are not|was not|were not|cannot|will not|never did|no longer)\b/i
// Any negation at all: a sentence with one is not "affirmative" for negclose.
const NEGATIVE = /\b(?:not|no|never|cannot|none|nothing)\b|n['’]t\b/i
const LIST_LINE = /^(\s*)(?:\d+[.)]|[-*•])\s+/
const LABEL = /^\*\*[^*\n]+\*\*:?\s*/
const TELL_RULES = ['vocab', 'filler', 'opener', 'negclose', 'punct']

// Blank out code, URLs and table rows. Same length, so indices stay valid.
function blank(s: string): string {
  return s.replace(/[^\n]/g, ' ')
}
function strip(text: string): string {
  let fenced: string | null = null
  const lines = text.split('\n').map((line) => {
    const m = /^\s*(```+|~~~+)/.exec(line)
    if (fenced) {
      if (m && m[1][0] === fenced[0] && m[1].length >= fenced.length) fenced = null
      return blank(line)
    }
    if (m) {
      fenced = m[1]
      return blank(line)
    }
    return /^\s*\|/.test(line) ? blank(line) : line
  })
  return lines.join('\n').replace(/`[^`\n]*`/g, blank).replace(/https?:\/\/\S+/g, blank)
}

type Seg = { text: string; start: number; list: boolean }
type Sentence = { text: string; index: number; list: boolean }

// Split a paragraph into prose runs and single list lines.
function segments(par: string, base: number): Seg[] {
  const segs: Seg[] = []
  let pos = 0
  for (const line of par.split('\n')) {
    const list = LIST_LINE.test(line)
    const prev = segs[segs.length - 1]
    if (!list && prev && !prev.list) prev.text += ' ' + line
    else segs.push({ text: line, start: base + pos, list })
    pos += line.length + 1
  }
  return segs
}

function sentences(seg: Seg): Sentence[] {
  const marker = seg.list ? LIST_LINE.exec(seg.text) : null
  const skip = marker ? marker[0].length : 0
  let body = seg.text.slice(skip)
  // A list item that opens with a bold label (`**Label.** text`) is prose under
  // a heading, not a step: blank the label (same length, so indices hold) so
  // its period does not split a sentence, and judge the rest as prose.
  const label = seg.list ? LABEL.exec(body) : null
  const list = label ? false : seg.list
  if (label) body = blank(label[0]) + body.slice(label[0].length)
  // Mask abbreviation dots for splitting only; same length.
  const masked = body.replace(/\b(e\.g|i\.e|etc|vs|approx)\./gi, (m) => m.replace(/\./g, '_'))
  const out: Sentence[] = []
  for (const m of masked.matchAll(/[^.!?]*[.!?]+(?=\s|$)|[^.!?]+$/g)) {
    const raw = body.slice(m.index, m.index + m[0].length)
    const lead = raw.length - raw.trimStart().length
    const text = raw.trim()
    if (text) out.push({ text, index: seg.start + skip + m.index! + lead, list })
  }
  return out
}

const words = (s: string) => (s.match(/[A-Za-z0-9][A-Za-z0-9'’_.\-/]*/g) || []).length

type Add = (rule: string, match: string, index: number, extra?: Partial<Finding>) => void

// The sentence-level rules of one paragraph: long sentences (every mode), and in
// strict mode the repeated opener and the negating closer. Returns the long count.
function lintParagraph(par: string, base: number, strict: boolean, add: Add): number {
  let long = 0
  let run: Sentence[] = []
  for (const seg of segments(par, base)) {
    const ss = sentences(seg)
    for (const s of ss) {
      const n = words(s.text)
      if (n > (s.list ? 20 : 25)) {
        long++
        add('long', s.text.split(/\s+/).slice(0, 5).join(' ') + '…', s.index, { n })
      }
    }
    if (!strict) continue
    if (seg.list) {
      run = []
      continue
    }
    for (const s of ss) {
      const w = /[A-Za-z']+/.exec(s.text.replace(/^[*_#>\s]+/, ''))?.[0]
      if (w && run.length && /^[A-Za-z']+/.exec(run[0].text.replace(/^[*_#>\s]+/, ''))?.[0] === w) run.push(s)
      else run = w ? [s] : []
      if (run.length === 3) add('opener', w!, s.index)
    }
    // A short (6 words or fewer) negating sentence that closes the paragraph
    // right after an affirmative one: the rhetorical "X does. Y does not."
    // A negated fact after another negated fact is plain prose.
    const last = ss[ss.length - 1]
    const prev = ss[ss.length - 2]
    if (last && prev && words(last.text) <= 6 && NEGATOR.test(last.text) && !NEGATIVE.test(prev.text)) {
      add('negclose', last.text, last.index)
    }
  }
  return long
}

export function lintReply(text: string, opts?: { mode?: Mode }): Report {
  const mode = opts?.mode ?? 'strict'
  const findings: Finding[] = []
  let longSentences = 0
  if (mode !== 'off' && text && text.trim()) {
    const t = strip(text)
    const strict = mode === 'strict'
    const add: Add = (rule, match, index, extra = {}) => findings.push({ rule, match, index, ...extra })
    for (const m of t.matchAll(VOCAB)) add('vocab', m[0], m.index!)
    for (const m of t.matchAll(FILLER)) add('filler', m[0], m.index!, { fix: FILLER_FIX[m[0].toLowerCase()] })
    if (strict) for (const m of t.matchAll(PUNCT)) add('punct', m[0].trim() || m[0], m.index!)
    let paraNo = 0
    for (const pm of t.matchAll(/[^\n]+(?:\n(?![ \t]*\n)[^\n]*)*/g)) {
      const par = pm[0]
      if (!par.trim()) continue
      paraNo++
      const lead = par.length - par.trimStart().length
      if (strict && OPENER_PHRASE.test(par.trimStart())) add('vocab', par.trimStart().split(/[!,]/)[0], pm.index! + lead)
      const dashes = (par.match(/—/g) || []).length
      if (dashes > 1) add('emdash', '—', pm.index! + par.indexOf('—'), { n: dashes, para: paraNo })
      longSentences += lintParagraph(par, pm.index!, strict, add)
    }
  }
  findings.sort((a, b) => a.index - b.index)
  const counts: Record<string, number> = {}
  for (const f of findings) counts[f.rule] = (counts[f.rule] || 0) + 1
  const tells = TELL_RULES.reduce((a, r) => a + (counts[r] || 0), 0)
  const em = findings.filter((f) => f.rule === 'emdash').reduce((a, f) => a + (f.n || 0), 0)
  return { findings, counts, longSentences, score: `tells ${tells} · long ${longSentences} · em— ${em}` }
}

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

// One short line for the next prompt; '' when the reply was clean. Items are
// ranked by severity, not by position: banned words and openers first (the
// clearest tells), then filler, em dashes, long sentences, and the softer
// rules last. The first three ranked items make the line.
export function feedbackLine(report: Report): string {
  const f = report.findings
  if (!f.length) return ''
  const parts: string[] = []
  const uniq = (rule: string) => [...new Set(f.filter((x) => x.rule === rule).map((x) => x.match.toLowerCase()))]
  for (const w of uniq('vocab').slice(0, 2)) parts.push(`"${w}"`)
  if (report.counts.opener) parts.push('3 sentences with the same opener')
  for (const w of uniq('filler').slice(0, 1)) parts.push(`"${w}"`)
  for (const d of f.filter((x) => x.rule === 'emdash').slice(0, 1)) parts.push(`${d.n} em dashes in ¶${d.para}`)
  if (report.longSentences) parts.push(`${plural(report.longSentences, 'sentence', 'sentences')} >25 words`)
  if (report.counts.negclose) parts.push('a short negating closer')
  if (report.counts.punct) parts.push('curly quotes or en dash')
  return `Last reply: ${parts.slice(0, 3).join(', ')}.`
}

/** The rule a finding list breaks worst, for a one-word summary. */
export const SEVERITY: readonly string[] = ['vocab', 'opener', 'filler', 'emdash', 'long', 'negclose', 'punct']
export function topRule(report: Report): string {
  return SEVERITY.find((r) => (report.counts[r] || 0) > 0) ?? ''
}
