import { Fragment, useMemo } from 'react'

const TOKEN_CLASS = {
  key: 'text-[var(--m-get)]',
  string: 'text-[var(--m-post)]',
  number: 'text-[var(--m-put)]',
  literal: 'text-[var(--m-patch)]',
  punct: 'text-muted',
}

// A line-numbered gutter costs two DOM nodes per line; past this many lines
// the body is shown as a plain block instead.
const MAX_GUTTER_LINES = 5000

// Splits text (or highlight tokens) into lines of React nodes.
function toLines(text, tokens) {
  if (!tokens) return text.split('\n')
  const lines = [[]]
  tokens.forEach((tok, i) => {
    const parts = tok.v.split('\n')
    parts.forEach((part, j) => {
      if (j > 0) lines.push([])
      if (part) {
        lines[lines.length - 1].push(
          <span key={`${i}.${j}`} className={TOKEN_CLASS[tok.t]}>
            {part}
          </span>,
        )
      }
    })
  })
  return lines
}

// Read-only code block with line numbers. Captured content is rendered as
// text nodes only.
export function CodeView({ text, tokens }) {
  const lines = useMemo(() => toLines(text, tokens), [text, tokens])
  if (lines.length > MAX_GUTTER_LINES) {
    return (
      <pre className="px-4 py-3 font-mono text-[12px] leading-5 whitespace-pre-wrap text-text [overflow-wrap:anywhere] select-text">
        {text}
      </pre>
    )
  }
  return (
    <div className="grid grid-cols-[auto_minmax(0,1fr)] py-2 font-mono text-[12px] leading-5 select-text">
      {lines.map((line, i) => (
        <Fragment key={i}>
          <span className="tabular pr-3 pl-4 text-right text-faint/70 select-none">{i + 1}</span>
          <span className="min-h-5 pr-4 whitespace-pre-wrap text-text [overflow-wrap:anywhere]">{line}</span>
        </Fragment>
      ))}
    </div>
  )
}
