import type { On } from 'claude-code'
import { test, expect } from 'claude-code/testing'

import { register } from '../hooks/register'

// A managed (prepend-tier) plugin such as cc-plugin-sec-default continues every `classic.*`
// event past user-tier plugins, and the engine prints a transcript line for each hook it
// skips. The mod hooks none, so there is nothing to skip.

/** The patterns register() names, in order, read with a recording `on`. */
function hookedEvents(): string[] {
  const events: string[] = []
  const chain = { catch: () => chain }
  const on = ((pattern: string) => {
    events.push(pattern)
    return chain
  }) as unknown as On
  register(on, {} as never)
  return events
}

test('the mod registers no classic.* hook', async () => {
  const events = hookedEvents()
  expect(events.length).toBeGreaterThan(0)
  expect(events.filter(name => name.startsWith('classic.') || name === '*')).toEqual([])
})

test('the mod hooks no event the managed plugin continues past it', async () => {
  // cc-plugin-sec-default (2.1.310) sends these on past every user-tier plugin.
  const policy = ['prompt.section', 'prompt.context', 'prompt.attachment', 'prompt.compose', 'skill.prompt', 'session.compact']
  expect(hookedEvents().filter(name => policy.includes(name))).toEqual([])
})
