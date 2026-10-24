# Drops, from the rest-of-command output of a chained call, a block of lines identical to
# the final assistant message (the file named by -v last=): the answer is already shown
# by the Codex events and by the report, so repeating it is noise.
# A block is dropped only when ALL of its lines arrive in order and contiguously; a partial
# match is printed once the stream leaves it, so no other output is ever lost. The file is
# read afresh whenever a candidate block could start, because each codex invocation in a
# chain rewrites it. Trailing blank lines of the file do not count.
function load(   l, k) {
  k = 0; delete L
  if (last == "") return 0
  while ((getline l < last) > 0) L[++k] = l
  close(last)
  while (k > 0 && L[k] == "") k--
  return k
}
function flush(   i) { for (i = 1; i <= m; i++) print B[i]; m = 0; fflush() }
function take() { B[++m] = $0; if (m == n) m = 0 }
{
  if (m == 0) n = load()
  if (n > 0 && $0 == L[m + 1]) { take(); next }
  flush()
  if (n > 0 && $0 == L[1]) { take(); next }
  print; fflush()
}
END { flush() }
