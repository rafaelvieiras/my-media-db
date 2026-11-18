import { describe, expect, it } from 'vitest'
import {
  aPartirDaGrade,
  fundir,
  idsAgrupados,
  mover,
  paraPayload,
  removerUnidade,
  renumerar,
  separar,
  type EpisodioDaGrade,
} from '@/lib/owner-order-model'

const grade: EpisodioDaGrade[] = [
  { id: 'a', title: 'A', season: 1, number: 1 },
  { id: 'b', title: 'B', season: 1, number: 2 },
  { id: 'c', title: 'C', season: 1, number: 3 },
  { id: 'd', title: 'D', season: 2, number: 1 },
]

describe('owner-order-model', () => {
  it('builds one unit per episode from the base order', () => {
    const unidades = aPartirDaGrade(grade)
    expect(unidades).toHaveLength(4)
    expect(unidades[0]).toEqual({ season: 1, number: 1, episodes: ['a'] })
    expect(unidades[3]).toEqual({ season: 2, number: 1, episodes: ['d'] })
  })

  it('merges the selected episodes into ONE unit, keeping the place and number of the first', () => {
    const unidades = fundir(aPartirDaGrade(grade), ['a', 'b', 'c'])
    expect(unidades).toHaveLength(2)
    // The unit takes the position and the numbering of "a" (S01E01) and keeps the order.
    expect(unidades[0]).toEqual({
      season: 1,
      number: 1,
      episodes: ['a', 'b', 'c'],
    })
    // And the episode that was not selected stays as its own unit, untouched.
    expect(unidades[1]).toEqual({ season: 2, number: 1, episodes: ['d'] })
  })

  it('merges across units, dropping the units that end up empty', () => {
    const unidades = fundir(aPartirDaGrade(grade), ['b', 'd'])
    expect(unidades.map((u) => u.episodes)).toEqual([['a'], ['b', 'd'], ['c']])
    // The resulting unit is placed where the first selected episode used to be, with its
    // numbering.
    expect(unidades[1]).toEqual({ season: 1, number: 2, episodes: ['b', 'd'] })
  })

  it('ignores an empty selection and ids that do not exist', () => {
    const base = aPartirDaGrade(grade)
    expect(fundir(base, [])).toBe(base)
    expect(fundir(base, ['zzz'])).toBe(base)
  })

  it('splits a merged unit into one unit per episode, numbering them in sequence', () => {
    const unidades = separar(fundir(aPartirDaGrade(grade), ['a', 'b', 'c']), 0)
    expect(unidades.slice(0, 3)).toEqual([
      { season: 1, number: 1, episodes: ['a'] },
      { season: 1, number: 2, episodes: ['b'] },
      { season: 1, number: 3, episodes: ['c'] },
    ])
  })

  it('does not split a single-episode unit', () => {
    const base = aPartirDaGrade(grade)
    expect(separar(base, 0)).toBe(base)
    expect(separar(base, 99)).toBe(base)
  })

  it('moves a unit up and down, ignoring the ends', () => {
    const base = aPartirDaGrade(grade)
    expect(mover(base, 1, -1).map((u) => u.episodes[0])).toEqual([
      'b',
      'a',
      'c',
      'd',
    ])
    expect(mover(base, 1, 1).map((u) => u.episodes[0])).toEqual([
      'a',
      'c',
      'b',
      'd',
    ])
    expect(mover(base, 0, -1)).toBe(base)
    expect(mover(base, 3, 1)).toBe(base)
    expect(mover(base, 9, 1)).toBe(base)
  })

  it('removes a unit', () => {
    const unidades = removerUnidade(aPartirDaGrade(grade), 1)
    expect(unidades.map((u) => u.episodes[0])).toEqual(['a', 'c', 'd'])
  })

  it('renumbers each season from 1 when the owner reorders', () => {
    const unidades = renumerar([
      { season: 5, number: 9, episodes: ['a'] },
      { season: 5, number: 4, episodes: ['b'] },
      { season: 6, number: 7, episodes: ['c'] },
      { season: 6, number: 7, episodes: ['d'] },
    ])
    expect(unidades.map((u) => [u.season, u.number])).toEqual([
      [5, 1],
      [5, 2],
      [6, 1],
      [6, 2],
    ])
  })

  it('lists the ids already grouped', () => {
    const agrupados = idsAgrupados(fundir(aPartirDaGrade(grade), ['a', 'b']))
    expect([...agrupados].sort()).toEqual(['a', 'b', 'c', 'd'])
  })

  it('builds the payload, trimming the name and dropping empty units', () => {
    const payload = paraPayload('  HBO 2004  ', 'official', [
      { season: 1, number: 1, title: 'Blackout', episodes: ['a', 'b'] },
      { season: 1, number: 2, episodes: [] },
    ])
    expect(payload).toEqual({
      name: 'HBO 2004',
      based_on: 'official',
      units: [
        { season: 1, number: 1, title: 'Blackout', episodes: ['a', 'b'] },
      ],
    })
  })
})
