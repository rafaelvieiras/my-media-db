import { describe, expect, it } from 'vitest'
import {
  OBRIGATORIO_NA_CRIACAO,
  construirPatchEdicao,
  criaveis,
  ordenados,
  schemaZod,
  valoresDeCriacao,
  valoresDeEdicao,
} from '@/lib/field-schema'
import { baseEntity, descritoresPadrao } from '@/test/entity-fixtures'

// Tests the PURE core of the schema-driven form: descriptors →
// (seeds, Zod schema, patch). No JSX — extensibility is born here.

describe('field-schema — ordenação e recorte', () => {
  it('ordenados respeita `order` mesmo com entrada embaralhada', () => {
    const embaralhado = [...descritoresPadrao].reverse()
    expect(ordenados(embaralhado).map((d) => d.name)).toEqual([
      'title',
      'description',
      'tags',
      'original_title',
      'release_date',
      'local_rating',
    ])
  })

  it('criaveis mantém só os campos creatable', () => {
    expect(criaveis(descritoresPadrao).map((d) => d.name)).toEqual([
      'title',
      'description',
      'original_title',
      'release_date',
    ])
  })
})

describe('field-schema — sementes', () => {
  it('valoresDeCriacao começa vazio só nos creatable', () => {
    expect(valoresDeCriacao(descritoresPadrao)).toEqual({
      title: '',
      description: '',
      original_title: '',
      release_date: '',
    })
  })

  it('valoresDeEdicao semeia traduzível do idioma, neutro escalar, número e tags', () => {
    const entity = baseEntity({ tags: ['Ação'], original_title: 'Dune' })
    const blob = {
      title: { 'pt-BR': 'Duna', en: 'Dune' },
      original_title: 'Dune',
      local_rating: 8,
    }
    const seeds = valoresDeEdicao(descritoresPadrao, blob, 'pt-BR', entity)
    expect(seeds.title).toBe('Duna') // idioma corrente
    expect(seeds.description).toBe('') // sem override → vazio (placeholder herda)
    expect(seeds.original_title).toBe('Dune') // neutro escalar
    expect(seeds.local_rating).toBe('8') // número vira texto do input
    expect(seeds.tags).toEqual(['Ação']) // tags do resolvido
  })
})

describe('field-schema — schema Zod', () => {
  it('criação exige título e aceita o resto vazio', () => {
    const schema = schemaZod(descritoresPadrao, 'criacao')
    expect(
      schema.safeParse({
        title: '',
        description: '',
        original_title: '',
        release_date: '',
      }).success,
    ).toBe(false)
    expect(
      schema.safeParse({
        title: 'Duna',
        description: '',
        original_title: '',
        release_date: '',
      }).success,
    ).toBe(true)
  })

  it('edição permite tudo vazio (definir vazio ≠ inválido)', () => {
    const schema = schemaZod(descritoresPadrao, 'edicao')
    const vazio = {
      title: '',
      description: '',
      tags: [],
      original_title: '',
      release_date: '',
      local_rating: '',
    }
    expect(schema.safeParse(vazio).success).toBe(true)
  })

  it('a nota fora de 0–10 falha; vazia ou dentro passa', () => {
    const schema = schemaZod(descritoresPadrao, 'edicao')
    const base = {
      title: '',
      description: '',
      tags: [],
      original_title: '',
      release_date: '',
    }
    expect(schema.safeParse({ ...base, local_rating: '11' }).success).toBe(
      false,
    )
    expect(schema.safeParse({ ...base, local_rating: '9.5' }).success).toBe(
      true,
    )
    expect(schema.safeParse({ ...base, local_rating: '' }).success).toBe(true)
  })
})

describe('field-schema — construirPatchEdicao', () => {
  const entity = baseEntity()

  it('só inclui o que mudou; vazio vira null (definir vazio)', () => {
    const seeds = valoresDeEdicao(descritoresPadrao, {}, 'pt-BR', entity)
    const valores = { ...seeds, title: 'Duna', description: '' }
    // title changed; description stays empty (unchanged) → it does not enter.
    const patch = construirPatchEdicao(descritoresPadrao, valores, seeds)
    expect(patch).toEqual({ title: 'Duna' })
  })

  it('esvaziar um campo semeado manda null', () => {
    const blob = { description: { 'pt-BR': 'Sinopse' } }
    const seeds = valoresDeEdicao(descritoresPadrao, blob, 'pt-BR', entity)
    const valores = { ...seeds, description: '' }
    expect(construirPatchEdicao(descritoresPadrao, valores, seeds)).toEqual({
      description: null,
    })
  })

  it('número vira Number; vazio semeado vira null', () => {
    const seeds = valoresDeEdicao(descritoresPadrao, {}, 'pt-BR', entity)
    expect(
      construirPatchEdicao(
        descritoresPadrao,
        { ...seeds, local_rating: '9.5' },
        seeds,
      ),
    ).toEqual({ local_rating: 9.5 })

    const seedsComNota = valoresDeEdicao(
      descritoresPadrao,
      { local_rating: 7 },
      'pt-BR',
      entity,
    )
    expect(
      construirPatchEdicao(
        descritoresPadrao,
        { ...seedsComNota, local_rating: '' },
        seedsComNota,
      ),
    ).toEqual({ local_rating: null })
  })

  it('tags viram operações {add, remove} relativas ao resolvido', () => {
    const seeds = valoresDeEdicao(
      descritoresPadrao,
      {},
      'pt-BR',
      baseEntity({ tags: ['Ação', 'Drama'] }),
    )
    const valores = { ...seeds, tags: ['Drama', 'Clássico'] }
    expect(construirPatchEdicao(descritoresPadrao, valores, seeds)).toEqual({
      tags: { add: ['Clássico'], remove: ['Ação'] },
    })
  })

  it('sem mudanças devolve patch vazio', () => {
    const seeds = valoresDeEdicao(descritoresPadrao, {}, 'pt-BR', entity)
    expect(
      construirPatchEdicao(descritoresPadrao, { ...seeds }, seeds),
    ).toEqual({})
  })
})

describe('field-schema — data e mensagens localizadas', () => {
  const campos = descritoresPadrao

  it('recusa uma data que não existe no calendário', () => {
    const schema = schemaZod(campos, 'edicao')
    const base = {
      title: '',
      description: '',
      tags: [],
      original_title: '',
      release_date: '32/13/2026',
      local_rating: '',
    }
    expect(schema.safeParse(base).success).toBe(false)
    expect(
      schema.safeParse({ ...base, release_date: '2026-02-31' }).success,
    ).toBe(false)
    expect(
      schema.safeParse({ ...base, release_date: '2026-02-28' }).success,
    ).toBe(true)
    // Empty keeps meaning "inherit/set empty".
    expect(schema.safeParse({ ...base, release_date: '' }).success).toBe(true)
  })

  it('usa as mensagens do catálogo quando recebe o tradutor', () => {
    const t = ((chave: string, params?: Record<string, unknown>) =>
      params ? `${chave}:${String(params.field ?? params)}` : chave) as never
    const schema = schemaZod(campos, 'criacao', t)
    const resultado = schema.safeParse({
      title: '',
      description: '',
      original_title: '',
      release_date: 'ontem',
    })
    expect(resultado.success).toBe(false)
    const mensagens = resultado.error!.issues.map((i) => i.message)
    expect(mensagens).toContain('entity.edit.requiredField:Título')
    expect(mensagens).toContain('entity.edit.invalidDate')
  })

  it('OBRIGATORIO_NA_CRIACAO é a fonte única do asterisco e do aria-required', () => {
    expect([...OBRIGATORIO_NA_CRIACAO]).toEqual(['title'])
  })
})
