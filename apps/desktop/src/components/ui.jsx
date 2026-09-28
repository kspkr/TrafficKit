import { useCallback, useEffect, useRef, useState } from 'react'

export function Button({ variant = 'default', className = '', ...props }) {
  const styles = {
    default: 'border-line-strong bg-raised hover:bg-hover text-text',
    primary: 'border-transparent bg-accent text-[#1a1204] hover:brightness-110',
    ghost: 'border-transparent bg-transparent hover:bg-hover text-muted hover:text-text',
    danger: 'border-line-strong bg-raised hover:bg-hover text-server-err',
  }
  return (
    <button
      type="button"
      className={`inline-flex h-7 items-center gap-1.5 rounded border px-2.5 text-[12px] font-medium whitespace-nowrap disabled:cursor-default disabled:opacity-50 ${styles[variant]} ${className}`}
      {...props}
    />
  )
}

export function IconButton({ label, className = '', children, ...props }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      className={`inline-flex h-7 w-7 items-center justify-center rounded text-muted hover:bg-hover hover:text-text disabled:opacity-40 ${className}`}
      {...props}
    >
      {children}
    </button>
  )
}

export function Kbd({ children }) {
  return (
    <kbd className="rounded border border-line-strong bg-raised px-1 py-px font-mono text-[11px] text-muted">
      {children}
    </kbd>
  )
}

// Underlined tab strip. `tabs` is [{id, label, badge?}].
export function Tabs({ tabs, value, onChange, className = '' }) {
  return (
    <div role="tablist" className={`flex h-10 shrink-0 items-stretch gap-5 border-b border-line px-4 ${className}`}>
      {tabs.map((t) => {
        const active = t.id === value
        return (
          <button
            key={t.id}
            role="tab"
            type="button"
            aria-selected={active}
            onClick={() => onChange(t.id)}
            className={`-mb-px flex items-center gap-1.5 border-b-2 text-[12.5px] font-medium ${
              active ? 'border-accent text-text' : 'border-transparent text-muted hover:text-text'
            }`}
          >
            {t.label}
            {t.badge != null && <span className="tabular text-[11px] font-normal text-faint">{t.badge}</span>}
          </button>
        )
      })}
    </div>
  )
}

// Second-level tabs, drawn as a row of chips so they read as a different
// level from the main tabs above them.
export function SubTabs({ tabs, value, onChange }) {
  return (
    <div role="tablist" className="flex flex-wrap items-center gap-1 border-b border-line px-3 py-2">
      {tabs.map((t) => {
        const active = t.id === value
        return (
          <button
            key={t.id}
            role="tab"
            type="button"
            aria-selected={active}
            onClick={() => onChange(t.id)}
            className={`flex h-[26px] items-center gap-1.5 rounded-md px-2.5 text-[12px] ${
              active ? 'bg-hover text-text ring-1 ring-line-strong ring-inset' : 'text-muted hover:bg-hover/60 hover:text-text'
            }`}
          >
            {t.label}
            {t.badge != null && <span className="tabular text-[11px] text-faint">{t.badge}</span>}
          </button>
        )
      })}
    </div>
  )
}

// Segmented control for small mode switches.
export function Segmented({ options, value, onChange }) {
  return (
    <div className="inline-flex shrink-0 rounded-md bg-bg p-0.5 ring-1 ring-line-strong ring-inset">
      {options.map((o) => (
        <button
          key={o.id}
          type="button"
          onClick={() => onChange(o.id)}
          disabled={o.disabled}
          className={`rounded-[5px] px-2.5 py-0.5 text-[11.5px] font-medium disabled:opacity-40 ${
            o.id === value ? 'bg-raised text-text shadow-sm ring-1 ring-line-strong' : 'text-muted hover:text-text'
          }`}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function useCopy(timeout = 1200) {
  const [copied, setCopied] = useState(false)
  const timer = useRef()
  useEffect(() => () => clearTimeout(timer.current), [])
  const copy = useCallback(
    async (text) => {
      try {
        await navigator.clipboard.writeText(text)
        setCopied(true)
        clearTimeout(timer.current)
        timer.current = setTimeout(() => setCopied(false), timeout)
      } catch {
        setCopied(false)
      }
    },
    [timeout],
  )
  return [copied, copy]
}
