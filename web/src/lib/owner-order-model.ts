// Pure model of the OWNER ORDER editor (ADR-0033). The editor keeps a list of numbered
// units and the user groups episodes into them; every operation here is pure and total so
// the component stays a thin view and the rules are cheap to test.
//
// The invariants that mirror the backend (which refuses anything else):
//   - a unit has at least one episode;
//   - no episode appears in two units;
//   - no two units share the same (season, number).
import type { OwnerOrder, OwnerOrderUnit } from '@/lib/api'

// EpisodioDaGrade is the minimal episode shape the editor needs: the id (the anchor) and
// the numbering in the base order, which pre-fills the unit numbers.
export interface EpisodioDaGrade {
  id: string
  title: string
  season: number
  number: number
}

// aPartirDaGrade builds the starting point: one unit per episode, in the order the base
// order shows them. Episodes are copied, never referenced, so later edits are local.
export function aPartirDaGrade(eps: EpisodioDaGrade[]): OwnerOrderUnit[] {
  return eps.map((ep) => ({
    season: ep.season,
    number: ep.number,
    episodes: [ep.id],
  }))
}

// fundir merges the selected episode ids into ONE unit. The unit takes the place and the
// numbering of the first selected episode (by position in `unidades`), and the ids keep
// the order they had in the selection list. Units left empty disappear. Selecting nothing
// is a no-op; selecting ids that are already together in one unit is a no-op too.
export function fundir(
  unidades: OwnerOrderUnit[],
  ids: string[],
): OwnerOrderUnit[] {
  const selecionados = new Set(ids)
  if (selecionados.size === 0) {
    return unidades
  }
  let posicao = -1
  let season = 0
  let number = 0
  const restantes: OwnerOrderUnit[] = []
  for (const unidade of unidades) {
    const dentro = unidade.episodes.filter((id) => selecionados.has(id))
    if (dentro.length === 0) {
      restantes.push(unidade)
      continue
    }
    if (posicao === -1) {
      posicao = restantes.length
      season = unidade.season
      number = unidade.number
    }
    const fora = unidade.episodes.filter((id) => !selecionados.has(id))
    if (fora.length > 0) {
      restantes.push({ ...unidade, episodes: fora })
    }
  }
  if (posicao === -1) {
    return unidades
  }
  const nova: OwnerOrderUnit = { season, number, episodes: [...ids] }
  return [...restantes.slice(0, posicao), nova, ...restantes.slice(posicao)]
}

// separar splits a merged unit into one unit per episode, keeping the numbering of the
// first and numbering the rest by +1. A unit with a single episode is left untouched.
export function separar(
  unidades: OwnerOrderUnit[],
  indice: number,
): OwnerOrderUnit[] {
  const unidade = unidades[indice]
  if (!unidade || unidade.episodes.length < 2) {
    return unidades
  }
  const separadas = unidade.episodes.map((id, i) => ({
    season: unidade.season,
    number: unidade.number + i,
    episodes: [id],
  }))
  return [
    ...unidades.slice(0, indice),
    ...separadas,
    ...unidades.slice(indice + 1),
  ]
}

// mover shifts a unit one position up (-1) or down (+1), ignoring the ends.
export function mover(
  unidades: OwnerOrderUnit[],
  indice: number,
  delta: -1 | 1,
): OwnerOrderUnit[] {
  const destino = indice + delta
  if (indice < 0 || indice >= unidades.length) {
    return unidades
  }
  if (destino < 0 || destino >= unidades.length) {
    return unidades
  }
  const out = [...unidades]
  const [movida] = out.splice(indice, 1)
  out.splice(destino, 0, movida)
  return out
}

// removerUnidade drops a unit; the episodes it held go back to "not grouped".
export function removerUnidade(
  unidades: OwnerOrderUnit[],
  indice: number,
): OwnerOrderUnit[] {
  return unidades.filter((_, i) => i !== indice)
}

// renumerar walks the units in order and rewrites (season, number) so the sequence is
// continuous: a new season starts when the unit before it is in another season.
export function renumerar(unidades: OwnerOrderUnit[]): OwnerOrderUnit[] {
  let seasonAnterior: number | null = null
  let numero = 0
  return unidades.map((unidade) => {
    if (unidade.season !== seasonAnterior) {
      seasonAnterior = unidade.season
      numero = 1
    } else {
      numero += 1
    }
    return { ...unidade, number: numero }
  })
}

// idsAgrupados returns the ids that already belong to a unit, so the episode list can
// mark them and the "merge" action knows what is still free.
export function idsAgrupados(unidades: OwnerOrderUnit[]): Set<string> {
  const out = new Set<string>()
  for (const unidade of unidades) {
    for (const id of unidade.episodes) {
      out.add(id)
    }
  }
  return out
}

// paraPayload builds the request body. Units without episodes are dropped defensively
// (the UI never creates one, the backend refuses one).
export function paraPayload(
  name: string,
  basedOn: string,
  unidades: OwnerOrderUnit[],
): OwnerOrder {
  return {
    name: name.trim(),
    based_on: basedOn,
    units: unidades
      .filter((u) => u.episodes.length > 0)
      .map((u) => ({
        season: u.season,
        number: u.number,
        ...(u.title ? { title: u.title } : {}),
        episodes: [...u.episodes],
      })),
  }
}
