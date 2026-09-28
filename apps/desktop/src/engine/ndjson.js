// Yields one parsed object per line of a newline-delimited JSON stream.
// Chunk boundaries can fall anywhere, including inside a UTF-8 sequence,
// so decoding is streamed and partial lines are carried over.
export async function* readNdjson(stream) {
  const reader = stream.pipeThrough(new TextDecoderStream()).getReader()
  let carry = ''
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      carry += value
      let nl
      while ((nl = carry.indexOf('\n')) >= 0) {
        const line = carry.slice(0, nl).trim()
        carry = carry.slice(nl + 1)
        if (line) yield JSON.parse(line)
      }
    }
    const rest = carry.trim()
    if (rest) yield JSON.parse(rest)
  } finally {
    reader.releaseLock()
  }
}
