import type { On } from 'claude-code'

/**
 * An in-memory file system answering the plugin's `$.fs` reads from a map of
 * `path → text` (the Go-written fixture), with a log of every fs op the
 * plugin raised. `fs.write` is answered (and logged) so a write would show,
 * never land.
 */
export type MemFs = {
  files: Map<string, string>
  ops: { op: string; path: string }[]
}

const dirOf = (path: string) => path.slice(0, path.lastIndexOf('/')) || '/'

function childrenOf(files: ReadonlyMap<string, string>, dir: string) {
  const out = new Map<string, { kind: 'file' | 'dir'; size: number }>()
  const prefix = dir.endsWith('/') ? dir : `${dir}/`
  for (const [path, text] of files) {
    if (!path.startsWith(prefix)) continue
    const [name = '', ...rest] = path.slice(prefix.length).split('/')
    out.set(name, rest.length > 0 ? { kind: 'dir', size: 0 } : { kind: 'file', size: text.length })
  }
  return out
}

const isDir = (files: ReadonlyMap<string, string>, path: string) => childrenOf(files, path).size > 0

const missing = (path: string) => Object.assign(new Error(`ENOENT: ${path}`), { code: 'ENOENT' })

/** Hooks fs.list, fs.read, fs.exists, fs.stat and fs.write on `on` over `files`, at `mtimeMs`. */
export function memFs(on: On, root: string, rel: Record<string, string>, mtimeMs: number): MemFs {
  const files = new Map(Object.entries(rel).map(([path, text]) => [`${root}/${path}`, text]))
  const fs: MemFs = { files, ops: [] }
  const log = (op: string, path: string) => fs.ops.push({ op, path })
  on('fs.list', async (_$, e) => {
    log('fs.list', e.path)
    if (!isDir(files, e.path)) throw missing(e.path)
    const entries = [...childrenOf(files, e.path)].map(([name, it]) => ({
      name, kind: it.kind, size: it.size, mtimeMs: it.kind === 'file' ? mtimeMs : 0, isLink: false,
    }))
    return { value: entries }
  })
  on('fs.read', async (_$, e) => {
    log('fs.read', e.path)
    const text = files.get(e.path)
    if (text === undefined) throw missing(e.path)
    return { value: text }
  })
  on('fs.exists', async (_$, e) => {
    log('fs.exists', e.path)
    return { value: files.has(e.path) || isDir(files, e.path) }
  })
  on('fs.stat', async (_$, e) => {
    log('fs.stat', e.path)
    const text = files.get(e.path)
    if (text === undefined && !isDir(files, e.path)) throw missing(e.path)
    return { value: { kind: text === undefined ? 'dir' : 'file', size: text?.length ?? 0, mtimeMs, isLink: false } }
  })
  on('fs.write', async (_$, e) => {
    log('fs.write', e.path)
    return { value: undefined }
  })
  return fs
}

/** The paths under `dir` in `rel`, for adding or removing one run folder. */
export function under(rel: Record<string, string>, dir: string): string[] {
  return Object.keys(rel).filter(path => dirOf(path).startsWith(dir))
}
