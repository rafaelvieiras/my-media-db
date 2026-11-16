// Provider logos: SIMPLE inline SVGs, monochromatic, without
// any network/CDN/binary asset dependency — recognizable, not pixel-perfect to the
// brand (same spirit as `web/public/icons.svg`, already used for social
// icons). `currentColor` inherits the color of the surrounding text — it works in
// light/dark without extra work. `aria-hidden` because the accessible text
// (provider label) lives in the parent element (badge/link), not here.

// LogoBadge is the template shared by the 3 logos below: a rounded
// rectangle with the provider label in uppercase inside — it avoids
// duplicating the same `<svg>` structure 3 times (jscpd).
function LogoBadge({
  label,
  className,
}: {
  label: string
  className?: string
}) {
  // The viewBox width grows with the label size, so the text never
  // gets squeezed (TMDB/IMDB have 4 letters, TVDB too — but the template stays
  // generic for bigger labels in the future; unknown sources do not
  // go through here, but the robustness costs nothing).
  const largura = Math.max(34, label.length * 8 + 10)
  return (
    <svg viewBox={`0 0 ${largura} 18`} aria-hidden="true" className={className}>
      <rect
        x="0.75"
        y="0.75"
        width={largura - 1.5}
        height="16.5"
        rx="3"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
      />
      <text
        x={largura / 2}
        y="13"
        textAnchor="middle"
        fill="currentColor"
        fontSize="9"
        fontWeight="700"
        fontFamily="ui-sans-serif, system-ui, sans-serif"
        letterSpacing="0.02em"
      >
        {label}
      </text>
    </svg>
  )
}

export function TmdbLogo({ className }: { className?: string }) {
  return <LogoBadge label="TMDB" className={className} />
}

export function ImdbLogo({ className }: { className?: string }) {
  return <LogoBadge label="IMDb" className={className} />
}

export function TvdbLogo({ className }: { className?: string }) {
  return <LogoBadge label="TVDB" className={className} />
}
