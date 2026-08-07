// Logos de provedor (briefing 0086): SVGs inline SIMPLES, monocromáticos, sem
// dependência de rede/CDN/asset binário — reconhecíveis, não pixel-perfect da
// marca (mesmo espírito de `web/public/icons.svg`, já usado para ícones
// sociais). `currentColor` herda a cor do texto ao redor — funciona em
// claro/escuro sem trabalho extra. `aria-hidden` porque o texto acessível
// (rótulo do provedor) vive no elemento pai (badge/link), não aqui.

// LogoBadge é o molde compartilhado pelos 3 logos abaixo: um retângulo
// arredondado com o rótulo do provedor em maiúsculas dentro — evita
// duplicar a mesma estrutura de `<svg>` 3 vezes (jscpd).
function LogoBadge({
  label,
  className,
}: {
  label: string
  className?: string
}) {
  // Largura do viewBox cresce com o tamanho do rótulo, para o texto nunca
  // ficar espremido (TMDB/IMDB têm 4 letras, TVDB também — mas o molde fica
  // genérico para rótulos maiores no futuro, ex. fontes desconhecidas não
  // passam por aqui, mas a robustez não custa nada).
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
