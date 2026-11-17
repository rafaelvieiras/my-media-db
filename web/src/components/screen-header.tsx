// The screen header: title, one-sentence subtitle and, when the screen has them,
// the primary actions on the right. Every route above a list repeats the same
// three lines, so it lives here once — the copy still comes from the catalog, this
// component only owns the layout.
export function ScreenHeader({
  title,
  subtitle,
  actions,
}: {
  title: string
  subtitle?: string
  actions?: React.ReactNode
}) {
  return (
    <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
      <div className="flex flex-col gap-1">
        <h1 className="font-heading text-xl font-semibold">{title}</h1>
        {subtitle ? (
          <p className="text-sm text-muted-foreground">{subtitle}</p>
        ) : null}
      </div>
      {actions ? (
        <div className="flex flex-wrap items-start gap-2">{actions}</div>
      ) : null}
    </div>
  )
}
