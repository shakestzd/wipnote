// Draws parsed wip:* blocks. `ui` is the element table from $.ui.resolve(e),
// so the same tree works on terminal and desktop surfaces.
import type { Block, CalloutKind } from './blocks'

type Ui = { Box: any; Text: any } // the resolved element table; props are surface-checked at draw time

const KIND_COLOR: Record<CalloutKind, string> = { decision: 'suggestion', warning: 'warning', note: 'subtle' }
const KIND_LABEL: Record<CalloutKind, string> = { decision: 'Decision', warning: 'Warning', note: 'Note' }
const MARK = { done: '✓', next: '→', todo: '·' } as const
const MARK_COLOR = { done: 'success', next: 'suggestion', todo: 'subtle' } as const

export function Steps({ ui, steps }: { ui: Ui; steps: { state: 'done' | 'next' | 'todo'; text: string }[] }) {
  const { Box, Text } = ui
  return (
    <Box flexDirection="column">
      {steps.map((s, i) => (
        <Text key={i}>
          <Text color={MARK_COLOR[s.state]}>{MARK[s.state]}</Text>
          <Text dimColor>{` ${i + 1}. `}</Text>
          <Text bold={s.state === 'next'} dimColor={s.state === 'done'}>{s.text}</Text>
        </Text>
      ))}
    </Box>
  )
}

export function Compare({ ui, options }: { ui: Ui; options: { name: string; recommended: boolean; pros: string[]; cons: string[] }[] }) {
  const { Box, Text } = ui
  return (
    <Box flexDirection="row" gap={1}>
      {options.map((o, i) => (
        <Box key={i} flexDirection="column" flexGrow={1} paddingX={1}
          borderStyle="round" borderColor={o.recommended ? 'success' : 'subtle'}>
          <Text bold color={o.recommended ? 'success' : undefined}>{o.name}{o.recommended ? ' ★ recommended' : ''}</Text>
          {o.pros.map((p, j) => <Text key={`p${j}`}><Text color="success">+ </Text>{p}</Text>)}
          {o.cons.map((c, j) => <Text key={`c${j}`}><Text color="error">- </Text>{c}</Text>)}
        </Box>
      ))}
    </Box>
  )
}

export function Callout({ ui, kind, text }: { ui: Ui; kind: CalloutKind; text: string }) {
  const { Box, Text } = ui
  return (
    <Box flexDirection="column" borderStyle="round" borderColor={KIND_COLOR[kind]} paddingX={1}>
      <Text bold color={KIND_COLOR[kind]}>{KIND_LABEL[kind]}</Text>
      <Text>{text}</Text>
    </Box>
  )
}

export function renderBlock(ui: Ui, block: Block) {
  if (block.kind === 'steps') return <Steps ui={ui} steps={block.steps} />
  if (block.kind === 'compare') return <Compare ui={ui} options={block.options} />
  return <Callout ui={ui} kind={block.callout.kind} text={block.callout.text} />
}
