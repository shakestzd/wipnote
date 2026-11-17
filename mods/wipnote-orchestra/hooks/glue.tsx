import type { AgentRow, Mode, PromptInfo, WorkCard } from '../types'
import type { Hint } from './agents'
import type { BandRoom } from './band'
import type { Elements } from './register'
import { baseName, isLive, keepFor, modelLabel, parseGitStatus, shortDir, workItemIn } from './model'
import type { parseTrackText } from './model'

// The plain data work behind register.tsx's hooks. Pure: every `$` call stays in register.tsx.

type Ran = { exitCode: number; stdout: string } | undefined

/** The prompt line's facts from what the session, git and the env answered. */
export function promptInfo(a: {
  cwd: string
  home: string | undefined
  usage: { context: { percent: number }; rateLimits: ReadonlyArray<{ kind: string; percentUsed: number; resetsAt?: string }> }
  git: Ran
  top: Ran
  model: string | undefined
}): PromptInfo {
  const root = a.top?.exitCode === 0 ? a.top.stdout.trim() : ''
  return {
    dir: root !== '' ? baseName(root) : shortDir(a.cwd, a.home),
    dirty: 0,
    ahead: 0,
    behind: 0,
    ...(a.git?.exitCode === 0 ? parseGitStatus(a.git.stdout) : {}),
    model: a.model !== undefined ? modelLabel(a.model) : undefined,
    ctxPercent: a.usage.context.percent,
    limits: a.usage.rateLimits.map(({ kind, percentUsed, resetsAt }) => ({ kind, percentUsed, resetsAt })),
  }
}

/** The tracks the found cards belong to. */
export const trackIds = (found: readonly WorkCard[]): string[] => [
  ...new Set(found.map(card => card.trackId).filter((id): id is string => id !== undefined)),
]

/** The cards with their track's title and progress, where the track was read. */
export const withTracks = (found: readonly WorkCard[], tracks: ReadonlyMap<string, ReturnType<typeof parseTrackText>>): WorkCard[] =>
  found.map(card => {
    const track = card.trackId === undefined ? undefined : tracks.get(card.trackId)
    return track === undefined ? card : { ...card, trackTitle: track.title, done: track.done, total: track.total }
  })

/** Whether an agent is live or only just ended, so the band's clock needs to run. */
export const isBusy = (rows: readonly AgentRow[], at: number, keepDoneMs: number): boolean =>
  rows.some(row => isLive(row.status) || at - (row.endedAt ?? 0) < keepFor(row.status, keepDoneMs))

/** /orchestra's argument as a mode; none or an unknown one toggles off and full. */
export const wantedMode = (arg: string, current: Mode): Mode =>
  arg === 'full' || arg === 'compact' || arg === 'off' ? arg : current === 'off' ? 'full' : 'off'

/** What a spawn says about its agent: the work item its prompt names and its model. */
export const hintOf = (prompt: string, model: string | undefined): Hint => ({
  workItem: workItemIn(prompt),
  model: model !== undefined ? modelLabel(model) : undefined,
})

/** The engine's own hint line, with the band under it shifted left past the mode pill. */
export function withHint({ Box, Text }: Elements, tree: unknown, hint: string, indent: number) {
  return (
    <Box flexDirection="column">
      <Text key="hint" dimColor wrap="truncate-end">
        {hint}
      </Text>
      <Box key="band" marginLeft={-indent}>
        {tree as never}
      </Box>
    </Box>
  )
}

/** The room the band has above the prompt. */
export const aboveRoom = (
  props: { bodyColumns: number; maxRows: number; isWorking: boolean },
  shown: Mode,
): BandRoom => ({ width: props.bodyColumns, maxRows: props.maxRows, isWorking: props.isWorking, isCompact: shown === 'compact' })

/** The room the band has below the prompt's hint line. */
export const belowRoom = (
  viewport: { columns?: number; rows?: number } | undefined,
  isWorking: boolean,
  shown: Mode,
): BandRoom => ({
  width: Math.max(20, (viewport?.columns ?? 100) - 2),
  maxRows: Math.max(2, Math.floor((viewport?.rows ?? 30) / 4) - 1),
  isWorking,
  isCompact: shown === 'compact',
})
