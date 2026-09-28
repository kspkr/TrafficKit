// Small stroke icons, 16px grid. Kept inline to avoid an icon font.
function Svg({ children, size = 16, className = '' }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.4"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      {children}
    </svg>
  )
}

export const TrafficIcon = (p) => (
  <Svg {...p}>
    <path d="M2.5 5.5h9l-2.5-2.5M13.5 10.5h-9l2.5 2.5" />
  </Svg>
)

export const SettingsIcon = (p) => (
  <Svg {...p}>
    <circle cx="8" cy="8" r="2" />
    <path d="M8 1.8v1.6M8 12.6v1.6M1.8 8h1.6M12.6 8h1.6M3.6 3.6l1.1 1.1M11.3 11.3l1.1 1.1M3.6 12.4l1.1-1.1M11.3 4.7l1.1-1.1" />
  </Svg>
)

export const TrashIcon = (p) => (
  <Svg {...p}>
    <path d="M3 4.5h10M6.5 4.5V3h3v1.5M4.5 4.5l.6 8.5h5.8l.6-8.5" />
  </Svg>
)

export const CopyIcon = (p) => (
  <Svg {...p}>
    <rect x="5.5" y="5.5" width="7.5" height="7.5" rx="1" />
    <path d="M10.5 3.5V3H3v7.5h.5" />
  </Svg>
)

export const SearchIcon = (p) => (
  <Svg {...p}>
    <circle cx="7" cy="7" r="4" />
    <path d="M10 10l3.5 3.5" />
  </Svg>
)

export const CloseIcon = (p) => (
  <Svg {...p}>
    <path d="M4 4l8 8M12 4l-8 8" />
  </Svg>
)

export const LockIcon = (p) => (
  <Svg {...p}>
    <rect x="3.5" y="7" width="9" height="6.5" rx="1" />
    <path d="M5.5 7V5a2.5 2.5 0 015 0v2" />
  </Svg>
)

export const AlertIcon = (p) => (
  <Svg {...p}>
    <path d="M8 2.2l6 11H2z" />
    <path d="M8 6.5v3M8 11.3v.2" />
  </Svg>
)

export const PlugIcon = (p) => (
  <Svg {...p}>
    <path d="M6 2.5v3M10 2.5v3M4.5 5.5h7v2.5a3.5 3.5 0 01-7 0zM8 11.5v2" />
  </Svg>
)

export const TerminalIcon = (p) => (
  <Svg {...p}>
    <rect x="2" y="3" width="12" height="10" rx="1.5" />
    <path d="M4.8 6.3L6.8 8l-2 1.7M8.5 10h2.7" />
  </Svg>
)

export const GlobeIcon = (p) => (
  <Svg {...p}>
    <circle cx="8" cy="8" r="5.5" />
    <path d="M2.5 8h11M8 2.5c1.6 1.6 2.3 3.4 2.3 5.5S9.6 11.9 8 13.5M8 2.5C6.4 4.1 5.7 5.9 5.7 8s.7 3.9 2.3 5.5" />
  </Svg>
)

export const WrenchIcon = (p) => (
  <Svg {...p}>
    <path d="M10.5 2.8a3 3 0 00-3.3 4.1L2.8 11.3a1.2 1.2 0 001.7 1.7L8.9 8.6a3 3 0 004.1-3.3l-1.8 1.8-1.7-.3-.3-1.7z" />
  </Svg>
)

export function Logo({ size = 18 }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
      <path
        d="M7 11h13l-3.5-3.5M25 21H12l3.5 3.5"
        fill="none"
        stroke="var(--accent)"
        strokeWidth="2.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="24" cy="11" r="2.2" fill="var(--accent)" />
      <circle cx="8" cy="21" r="2.2" fill="var(--accent)" />
    </svg>
  )
}
