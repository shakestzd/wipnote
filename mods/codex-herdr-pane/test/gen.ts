import { buildWrapper, scriptPath, stubFor } from '../hooks/wrap.ts'
import { writeFileSync } from 'node:fs'
// usage: gen.ts <command> <root> [layout: tile|fixed] [maxPanes]; fixed splits right
// GEN_CAP=<n>: maxReturnLines (unset: 0, no cap). GEN_STUB_DIR=<dir>: the script is written to
// <dir>/codex-herdr-run.<id>.sh as the hook writes it and the stub is printed, as the Bash call sees it.
const [cmd, root, layout, max] = process.argv.slice(2)
const script = buildWrapper(cmd!, {
  direction: 'right', layout: layout === 'fixed' ? 'fixed' : 'tile', maxPanes: max ? Number(max) : 6,
  closePane: 'always', closeDelay: 1, tailLines: 50, maxReturnLines: Number(process.env.GEN_CAP ?? 0), viewer: 'plain',
  leanProfile: false, leanProfileName: 'lean', root: root!, id: 'sim',
})
const dir = process.env.GEN_STUB_DIR
if (dir) {
  const path = scriptPath(dir, 'sim')
  writeFileSync(path, script)
  process.stdout.write(stubFor(path))
} else process.stdout.write(script)
