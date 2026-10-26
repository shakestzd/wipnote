import type { EngineInterface } from 'claude-code'

import type { CiView, Snapshot } from '../types'
import {
  COLOR, ICON, baseLines, branchLine, cut, interval, prLine, releaseLine, reviewLine, sortChecks, span, staleLines,
} from './format'

type UI = ReturnType<EngineInterface['ui']['resolve']>

function Idle({ Box, Text }: UI) {
  return (
    <Box flexDirection="column">
      <Text dimColor>No pull request is followed.</Text>
      <Text dimColor>/ci follows the pull request of this branch.</Text>
      <Text dimColor>/ci 85 follows PR 85, /ci &lt;branch&gt; that branch's, /ci stop ends it.</Text>
    </Box>
  )
}

function Head({ Box, Text }: UI, s: Snapshot, width: number) {
  if (!s.available || s.pr === undefined) {
    return (
      <Box flexDirection="column">
        <Text weight="bold">{s.available ? 'No pull request' : 'CI status not available'}</Text>
        <Text dimColor wrap="truncate-end">{s.reason ?? ''}</Text>
      </Box>
    )
  }
  const n = s.counts
  return (
    <Box flexDirection="column">
      <Text weight="bold" wrap="truncate-end">{`PR #${s.pr.number} ${cut(s.pr.title, width - 10)}`}</Text>
      <Text dimColor wrap="truncate-end">{prLine(s)}</Text>
      <Text>
        <Text color="success">{`${n.pass} passed`}</Text>
        {' · '}
        <Text color={n.fail > 0 ? 'error' : 'inactive'}>{`${n.fail} failed`}</Text>
        {' · '}
        <Text color={n.pending > 0 ? 'warning' : 'inactive'}>{`${n.pending} running`}</Text>
        {n.other > 0 ? <Text dimColor>{` · ${n.other} skipped`}</Text> : null}
      </Text>
    </Box>
  )
}

// Every check goes in the tree: the pane's body scrolls a taller one.
function Checks({ Box, Text }: UI, s: Snapshot, now: number, width: number) {
  if (s.pr === undefined) return null
  const sorted = sortChecks(s.checks)
  if (sorted.length === 0) return <Text dimColor>No checks reported yet.</Text>
  return (
    <Box flexDirection="column">
      {sorted.map((c, i) => {
        const took = span(c, now)
        return (
          <Box key={`c${i}`} flexDirection="column">
            <Text color={COLOR[c.bucket] ?? 'inactive'} wrap="truncate-end">
              {`${ICON[c.bucket] ?? '?'} ${cut(c.name, width - 10)}`}
              {took !== '' ? <Text dimColor tabular>{` ${took}`}</Text> : null}
            </Text>
            {c.bucket === 'fail' && c.link !== '' ? <Text dimColor wrap="truncate-end">{c.link}</Text> : null}
          </Box>
        )
      })}
    </Box>
  )
}

/** Review, branch, stale worktrees, base, release and notes: one dim line each. */
function Sections({ Box, Text }: UI, s: Snapshot) {
  const lines: { text: string; bad: boolean }[] = []
  if (s.review) lines.push({ text: reviewLine(s.review), bad: false })
  if (s.branch) lines.push({ text: branchLine(s.branch), bad: false })
  for (const t of staleLines(s.worktrees)) lines.push({ text: t, bad: false })
  if (s.base) lines.push(...baseLines(s.base))
  if (s.release) lines.push({ text: releaseLine(s.release), bad: false })
  for (const n of s.notes ?? []) lines.push({ text: `note: ${n}`, bad: false })
  if (lines.length === 0) return null
  return (
    <Box flexDirection="column">
      {lines.map((l, i) => (
        <Text key={`l${i}`} wrap="truncate-end" color={l.bad ? 'error' : undefined} dimColor={!l.bad}>{l.text}</Text>
      ))}
    </Box>
  )
}

function footer(v: CiView, s: Snapshot): string {
  const at = v.fetchedAt === 0 ? 'not yet' : new Date(v.fetchedAt).toTimeString().slice(0, 8)
  if (v.arg === null) return `stopped · read ${at} · /ci starts it again`
  if (v.done) return `${s.pr === undefined ? 'nothing to follow' : 'finished'} · read ${at}`
  return `live, every ${interval(s) / 1000} s · read ${at}`
}

/** The pane's tree for the view, drawn on the surface's elements. */
export function paneTree(ui: UI, v: CiView, now: number, width: number) {
  const { Box, Text } = ui
  const s = v.snap
  if (s === null) return Idle(ui)
  return (
    <Box flexDirection="column">
      {Head(ui, s, width)}
      <Text> </Text>
      {Checks(ui, s, now, width)}
      <Text> </Text>
      {Sections(ui, s)}
      <Text> </Text>
      <Text dimColor>{footer(v, s)}</Text>
      {v.error !== '' ? <Text color="error" wrap="truncate-end">{`last read failed: ${v.error}`}</Text> : null}
    </Box>
  )
}
