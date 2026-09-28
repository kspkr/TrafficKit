export function Section({ title, children, note }) {
  return (
    <section className="border-b border-line py-6">
      <h2 className="text-[13px] font-semibold">{title}</h2>
      {note && <p className="mt-1 text-[12.5px] text-muted">{note}</p>}
      <div className="mt-4">{children}</div>
    </section>
  )
}
