import { parseSetCookie } from '../../lib/http.js'

export function Empty({ children }) {
  return <p className="px-4 py-6 text-[12.5px] text-faint">{children}</p>
}

// Name/value pairs: headers, query parameters, cookies.
export function KeyValueTable({ rows, empty = 'None' }) {
  if (!rows?.length) return <Empty>{empty}</Empty>
  return (
    <table className="w-full border-collapse text-[12.5px]">
      <tbody>
        {rows.map((r, i) => (
          <tr key={i} className="border-b border-line/50 align-top hover:bg-hover/60">
            <td className="w-[1%] py-1.5 pr-5 pl-4 font-medium whitespace-nowrap text-muted">
              {r.name || <span className="text-faint italic">(no name)</span>}
            </td>
            <td className="py-1.5 pr-4 font-mono text-[12px] text-text [overflow-wrap:anywhere] select-text">{r.value}</td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}

const COOKIE_FLAGS = ['secure', 'httponly', 'partitioned']

export function SetCookieTable({ values }) {
  if (!values.length) return <Empty>The response sets no cookies.</Empty>
  const cookies = values.map(parseSetCookie)
  return (
    <table className="w-full border-collapse font-mono text-[12px]">
      <thead>
        <tr className="border-b border-line text-left text-[11px] text-faint">
          {['Name', 'Value', 'Domain', 'Path', 'Expires', 'SameSite', 'Flags'].map((h) => (
            <th key={h} className="py-1.5 pr-4 pl-4 font-medium first:pl-4">
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {cookies.map((c, i) => {
          const a = c.attrs
          const expires = a['max-age'] != null ? `max-age ${a['max-age']}` : a.expires
          return (
            <tr key={i} className="border-b border-line/60 align-top hover:bg-hover">
              <td className="py-1 pr-4 pl-4 font-medium whitespace-nowrap text-muted">{c.name}</td>
              <td className="max-w-[28ch] py-1 pr-4 pl-4 break-all">{c.value}</td>
              <td className="py-1 pr-4 pl-4 text-muted">{a.domain}</td>
              <td className="py-1 pr-4 pl-4 text-muted">{a.path}</td>
              <td className="py-1 pr-4 pl-4 text-muted">{expires}</td>
              <td className="py-1 pr-4 pl-4 text-muted">{a.samesite}</td>
              <td className="py-1 pr-4 pl-4 text-muted">
                {COOKIE_FLAGS.filter((f) => a[f]).join(' ')}
              </td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}
