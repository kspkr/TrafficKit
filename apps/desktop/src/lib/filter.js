// Quick filter: whitespace-separated terms that must all appear somewhere in
// the row (method, host, path, status, content type, error). A leading "-"
// excludes rows containing the term. Case-insensitive.
export function compileFilter(text) {
  const terms = text.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return null
  const include = []
  const exclude = []
  for (const t of terms) {
    if (t.length > 1 && t.startsWith('-')) exclude.push(t.slice(1))
    else include.push(t)
  }
  return (row) => {
    const hay = haystack(row)
    return include.every((t) => hay.includes(t)) && !exclude.some((t) => hay.includes(t))
  }
}

const cache = new WeakMap()

// Rows are replaced, not mutated, when they change, so caching per object
// is safe and saves rebuilding strings on every keystroke.
function haystack(row) {
  let s = cache.get(row)
  if (s === undefined) {
    s = [
      row.method,
      row.host + row.path,
      row.status ?? '',
      row.statusText ?? '',
      row.contentType ?? '',
      row.error ?? '',
      row.kind === 'tunnel' ? 'tunnel' : '',
    ]
      .join(' ')
      .toLowerCase()
    cache.set(row, s)
  }
  return s
}
