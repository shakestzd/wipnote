import { expect, test } from 'claude-code/testing'

import { clip, parseItemJson, parseTrackText, showArgv } from '../hooks/model'

// Real `wipnote` output, captured 2026-10-18 (the test kit has no filesystem, so they are inlined).
const ITEM = JSON.stringify({
  id: 'feat-720972d1',
  title: 'commit-msg: strip agent co-author trailers and generated-with lines',
  status: 'in-progress',
  edges: { part_of: [{ target_id: 'trk-e301f245', relationship: 'part_of' }] },
})
const TRACK = [
  '────────────────────────────────────────────────────────────',
  '  Raw-data RCA agents',
  '────────────────────────────────────────────────────────────',
  '  ID        trk-9b502265',
  '  Type      track',
  '  Status    todo (1/46 done)',
].join('\n')

test('reads a work item title, status and track', async () => {
  expect(parseItemJson(ITEM)).toEqual({
    title: 'commit-msg: strip agent co-author trailers and generated-with lines',
    status: 'in-progress',
    trackId: 'trk-e301f245',
  })
  expect(parseItemJson('not json')).toBeUndefined()
})

test('reads a track title and progress from its text form', async () => {
  expect(parseTrackText(TRACK)).toEqual({ title: 'Raw-data RCA agents', done: 1, total: 46 })
})

test('builds the show command per kind and clips long titles', async () => {
  expect(showArgv('feat-720972d1')).toEqual(['wipnote', 'feature', 'show', 'feat-720972d1', '--format', 'json'])
  expect(showArgv('spk-1')?.[1]).toBe('spike')
  expect(showArgv('trk-1')).toBeUndefined()
  expect(clip('Routine setup script: install everything', 22)).toBe('Routine setup script:…')
})
