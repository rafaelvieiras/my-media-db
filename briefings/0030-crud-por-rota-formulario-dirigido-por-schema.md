# 0030 — CRUD por rota + formulário dirigido por schema

> Tira a criação e a edição de itens/coleções dos formulários inline e leva para
> **rotas dedicadas**, e faz o **formulário de campos editoriais ser gerado a partir do
> registro declarativo** (R1/ADR-0001) em vez de repetir a lista de campos à mão no front.

| | |
|---|---|
| **ID** | 0030 |
| **Tipo** | refactor/feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (dívida de manutenção; prepara plugins) |
| **Status** | ready |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [05-api](../docs/05-api.md), [11-decisoes-abertas](../docs/11-decisoes-abertas.md) (plugins), [ADR-0001](../docs/adr/0001-resolver-guiado-por-registro.md) |
| **Depende de** | [0016](0016-editor-override-ui.md) (editor de override), [0025](0025-colecao-entity-interna.md)/[0027](0027-similar-search-ids-item-manual.md) (criar coleção/item manual), [0024](0024-paridade-detalhe-entity-rico.md) (RichDetailView) |

---

## 1. Objetivo

Hoje **criar** e **editar** acontecem em formulários **inline**: criar item/coleção é um
`CreateToggle` que revela um form na home; editar é um `toggle` que injeta o
`OverrideEditor` **dentro** da página de detalhe. Funciona, mas tem duas dores:

1. **Usabilidade / crescimento da tela.** À medida que a superfície editável cresce
   (elenco/equipe do MD6, verticais, plugins), o editor inline **incha a página de
   detalhe** e o fluxo de criação vira um form gigante escondido atrás de um botão. Não
   há deep-link (não dá para compartilhar/abrir "editar item X"), e testar o fluxo exige
   dirigir a UI a partir de outra tela.

2. **Manutenção / extensibilidade — o problema real.** O backend **já tem fonte única de
   campos editáveis**: `resolver.CamposEditaveis()`, o registro declarativo
   (`campo → {Kind, Traduzivel, soLeitura}`) que **valida** o override em `validarPatch`.
   Campo novo entra editando **um** mapa, sem migração. **Mas o front não consome esse
   registro** — ele **duplica a lista de campos à mão em três lugares**:
   - `web/src/components/override-editor.tsx` — um `useState` + um bloco JSX por campo;
   - `web/src/components/item-create.tsx` — idem, campo a campo;
   - `web/src/lib/override-fields.ts` — detecção de "editado" por campo.

   Resultado: **todo campo novo** (`local_rating` ontem, MD6 amanhã, qualquer campo de
   plugin depois) obriga editar **Go + 3 arquivos de front à mão**. Esse fan-out é
   exatamente o que **não escala** quando os esquemas crescem via plugin — a preocupação
   que originou este briefing.

Este briefing centraliza o CRUD em **rotas** e faz o **formulário editorial ser dirigido
por um descritor de campos servido pelo backend**, de modo que **adicionar um campo passe
a ser uma linha no registro** — a mesma promessa que o resolver e o override já cumprem no
backend, estendida ao front.

## 2. Jornada do usuário

**Criar item (filme/série):**
1. Na home, o dono clica em "Novo item" → navega para **`/new/item`** (página própria).
2. Preenche tipo/título (+ opcionais); confirma.
3. Sucesso → navega para `/entities/{id}` do item recém-criado. Erro de validação (título
   vazio, tipo inválido) aparece **inline** com a `message` pt-BR (422).

**Criar coleção:**
1. "Nova coleção" → **`/new/collection`** → título (+ descrição) → cria → `/entities/{id}`.

**Editar item:**
1. Em `/entities/{id}`, o dono clica em "Editar" → navega para **`/entities/{id}/edit`**.
2. A página de edição lista os campos editáveis (por idioma para os traduzíveis; neutros à
   parte), a identidade externa (`external_ids`) e a arte. O dono altera, usa "Limpar"
   para voltar a herdar, salva.
3. Sucesso → volta para `/entities/{id}` com os valores atualizados. Cancelar volta sem
   gravar. Id inexistente → estado de erro 404 ("Não foi possível carregar o item").

## 3. Escopo

**Dentro:**
- **Rotas dedicadas:** `/new/item`, `/new/collection`, `/entities/$id/edit`.
- **Endpoint de descritor de formulário:** `GET /api/v1/schema/entity-fields`, derivado de
  `resolver.CamposEditaveis()` + metadados de apresentação.
- **Adoção de biblioteca de formulário + validação** (hoje inexistente — ver §6):
  **`@tanstack/react-form`** (coerência com `@tanstack/react-router`/`react-query` já no
  projeto) para estado/submissão e **`zod`** para o schema de validação de cliente.
- **Renderizador genérico** no front (`<SchemaForm>`) que monta os inputs **e o schema Zod**
  a partir do descritor (widget e regras por `Kind`/`Traduzivel`, com override explícito).
- **Migração** do `OverrideEditor` (→ corpo da rota de edição, dirigido por descritor), do
  `ItemCreate`/`CollectionCreate` (→ corpos das rotas de criação) e do `override-fields.ts`
  (detecção de "editado" dirigida por descritor). Realocar o `ExternalIdsEditor` para a
  rota de edição.
- **Atualização dos testes E2E** (specs de edição/criação) e unit para o fluxo por rota —
  governança do [0028](0028-testes-e2e.md) §7.1 (toda feature voltada ao usuário mantém a
  jornada presa por teste).

**Fora (explicitamente):**
- **Edição de elenco/equipe (MD6, [0029](0029-md6-elenco-equipe-person-credit-company.md))** —
  mas o descritor **deve acomodá-la** sem mudança de forma.
- **Maquinário de plugin** (manifest/registry/sandbox — [docs/11](../docs/11-decisoes-abertas.md)):
  aqui só deixamos o **hook** pronto (o descritor é a superfície que um plugin contribuiria).
- Reordenação de membros de coleção e upload de arte (têm fluxos próprios; a arte segue
  disponível na rota de edição, sem redesenho).
- Multi-idioma da **própria UI** (labels ficam pt-BR fixos por ora — ver §6).

## 4. Contrato técnico

### Dados / modelo
Nenhuma migração. A composição continua guiada por `registroOrigem` (resolver). O descritor
de apresentação é **metadado de UI**, não toca o schema nem a resolução.

### Descritor de campo (o coração)
Cada campo editável ganha um **descritor** servido pelo backend:

```jsonc
// FieldDescriptor
{
  "name": "local_rating",     // chave do campo (= chave do override / registro)
  "label": "Sua nota",        // rótulo pt-BR (UI é mono-idioma hoje; ver §6)
  "group": "neutro",          // "traduzivel" | "neutro" (seção do form)
  "widget": "number",         // "text" | "textarea" | "number" | "date" | "tags"
  "translatable": false,      // = registroOrigem[campo].traduzivel
  "kind": "numero",           // = Kind público do resolver (escalar|lista|numero|extensao)
  "order": 60,                // ordem estável de renderização
  "creatable": false,         // aparece no form de CRIAR item?
  "min": 0, "max": 10, "step": 0.1, // só p/ widget number
  "placeholder": null         // opcional
}
```

- **Fonte da lista:** o backend **itera `resolver.CamposEditaveis()`** e casa cada campo
  com seu descritor de apresentação. Um campo editável **sem** descritor é erro de
  programação — um **teste falha** (§5), forçando o par "campo novo ⇒ descritor". Essa é a
  **catraca da extensibilidade**: é impossível adicionar um campo editável e esquecer o
  front.
- **Widget** tem default derivável (`escalar`→`text`, `lista`→`tags`, `numero`→`number`),
  com **override explícito** onde faz sentido (`description`→`textarea`,
  `local_rating`→`number` 0–10).
- **`creatable`** separa o subconjunto que aparece ao **criar** um item (campos-base
  planos, sem operações por idioma) do conjunto completo que aparece ao **editar**.

### API
- `GET /api/v1/schema/entity-fields` → `{ "fields": [FieldDescriptor, ...] }`, **ordenado
  por `order`**. Sem autenticação (metadado estático). Cacheável (imutável entre deploys).
- Os endpoints de mutação **não mudam**: criação segue `POST /api/v1/entities/`
  (item) e `POST /api/v1/collections` (coleção); edição segue
  `PATCH /api/v1/entities/{id}/override` (por idioma + ops de lista + `local_rating`) e
  `PUT /api/v1/entities/{id}/external-ids`. O descritor só descreve **como renderizar**; a
  **forma do submit** continua a de cada endpoint.

### Backend (Go)
- Novo `internal/server/formschema.go`: o **registro de apresentação** (mapa
  `campo → apresentação`), a função que combina com `resolver.CamposEditaveis()` e o handler
  do endpoint. Mantém a separação de responsabilidades: **resolver = composição (puro);
  server = apresentação (UI)**. Um futuro plugin contribui para **ambos** os registros via
  seu manifest — por isso ficam declarativos e iteráveis, não `switch` por nome.
- `internal/server/server.go`: registrar a rota GET.
- `internal/server/formschema_test.go`: (a) todo campo de `CamposEditaveis()` tem
  descritor; (b) ordem estável; (c) widget coerente com o Kind.

### Frontend (Vite/React)
- **Stack de formulário (nova):** `@tanstack/react-form` + `zod`. O `<SchemaForm>` cria um
  `useForm` cujos `validators.onChange` recebem um **schema Zod montado a partir dos
  descritores** (Zod é *Standard Schema* — o TanStack Form v1 aceita direto, **sem
  adapter**). Cada campo vira um `form.Field name=...`; o descritor determina o widget e a
  regra Zod (ex.: `numero` com `min/max` → `z.number().min(min).max(max)`; escalar
  obrigatório → `z.string().min(1, 'Título é obrigatório')`). A validação de cliente
  **complementa** a autoritativa do servidor (o 422 continua sendo a verdade; o Zod só evita
  ida à API para erros triviais e melhora o feedback).
- `web/src/router.tsx`: três rotas novas (`/new/item`, `/new/collection`,
  `/entities/$id/edit`), code-based, irmãs das existentes.
- `web/src/routes/item-new.tsx`, `collection-new.tsx`, `entity-edit.tsx` (novos): corpos das
  páginas. O `entity-edit` carrega a entity + o blob de override + os descritores e monta o
  form; salva e navega de volta.
- `web/src/components/schema-form.tsx` (novo): renderizador genérico
  `(descritores, valores) → form` sobre o `useForm`, agrupando inputs por `group`/`widget`.
  Encapsula a semântica por-idioma (semente/placeholder herdado/"Limpar" vs "definir vazio")
  já existente no `OverrideEditor`, agora **dirigida por dados**.
- `web/src/lib/field-schema.ts` (novo): funções puras `descritores → schema Zod` e
  `descritores → defaultValues`, reutilizadas pelas três rotas e testáveis isoladamente.
- `web/src/lib/override-fields.ts`: a detecção de "editado" passa a **iterar os descritores**
  em vez de citar campos.
- `web/src/routes/entity-detail.tsx`: "Editar" vira **navegação** para `/entities/$id/edit`;
  remove o bloco `editando` inline (`OverrideEditor`/`ExternalIdsEditor`).
- `web/src/routes/index.tsx`: "Novo item"/"Nova coleção" viram navegação para as rotas.
- `web/src/lib/queries.ts` + `api.ts` + `api-types.ts`: query dos descritores + tipos
  gerados (`openapi.yaml` → `gen:types`).
- Aposentar/absorver `item-create.tsx`, `collection-create.tsx`, `inline-create-form.tsx`,
  `override-editor.tsx` conforme forem substituídos pelas rotas + `schema-form`.

### Arquivos afetados (previsão)
- **Backend:** `openapi.yaml`, `internal/server/server.go`, `internal/server/formschema.go`
  (novo), `internal/server/formschema_test.go` (novo).
- **Front (novos):** `routes/item-new.tsx`, `routes/collection-new.tsx`,
  `routes/entity-edit.tsx`, `components/schema-form.tsx`, `lib/field-schema.ts`.
- **Front (alterados):** `router.tsx`, `routes/index.tsx`, `routes/entity-detail.tsx`,
  `lib/override-fields.ts`, `lib/queries.ts`, `lib/api.ts`, `lib/api-types.ts`,
  **`package.json` + `package-lock.json`** (novas deps — ver a pegadinha do lockfile em §6).
- **Front (aposentados/absorvidos):** `components/item-create.tsx`,
  `components/collection-create.tsx`, `components/inline-create-form.tsx`,
  `components/override-editor.tsx`.
- **Testes:** `e2e/tests/editing.spec.ts`, `manual.spec.ts`, `library.spec.ts` (fluxo por
  rota); unit do `schema-form` e das rotas; `entity-detail-override.test.tsx` (novo local).

## 5. Critérios de aceite

- [ ] `GET /api/v1/schema/entity-fields` devolve descritores ordenados cobrindo **todos** os
  campos de `resolver.CamposEditaveis()`; **teste falha** se um campo editável não tiver
  descritor.
- [ ] Criar item e coleção acontecem em `/new/item` e `/new/collection`; sucesso navega para
  `/entities/{id}`; erro 422 aparece inline com `message` pt-BR.
- [ ] Editar acontece em `/entities/{id}/edit`; salva via `PATCH override` (por idioma) +
  `PUT external-ids`; "Limpar" volta a herdar; Cancelar volta sem gravar; id inexistente →
  estado 404.
- [ ] O form de edição é **renderizado a partir dos descritores** — adicionar um campo
  editável no backend (registro do resolver + descritor de apresentação) faz o input
  **aparecer sem tocar em JSX de campo**. (Verificável por um teste que injeta um campo e
  confere que o `schema-form` o renderiza.)
- [ ] **Nenhuma lista de campos hardcoded** permanece no front (`override-editor`/
  `item-create`/`override-fields` deixam de enumerar campos à mão).
- [ ] Os formulários usam **`@tanstack/react-form` + `zod`**; a validação de cliente
  (obrigatoriedade, faixa 0–10 do `local_rating`) roda **antes** do submit, e o schema Zod é
  **derivado dos descritores** (não redigitado). `web/package-lock.json` regenerado com
  npm ≥ 11.16 e `npm ci` verde.
- [ ] Catraca **verde** (Go + TS), baseline mantido/atualizado; specs E2E atualizados e
  verdes; `tsc -b` limpo.

## 6. Notas de implementação / decisões

- **Biblioteca de formulário (decisão + pesquisa).** Auditoria do front (2026-07-11):
  **não existe hoje nenhuma lib de formulário nem de validação** — `react-hook-form`,
  `formik`, `zod`, `yup`, `valibot`: nenhuma presente. Os forms são `useState` na mão e a
  única validação é a do **servidor** (422 renderizado inline); o cliente só faz guardas
  triviais (`title.trim() === ''`). Como este briefing reescreve os formulários e vai
  **gerá-los dinamicamente** a partir de descritores, é o momento certo de adotar uma lib.
  Decisão: **`@tanstack/react-form`** — mesma família do `@tanstack/react-router` e
  `@tanstack/react-query` que o projeto já usa (curva/ergonomia consistentes) — para
  estado/submissão, e **`zod`** para validação. A doc atual (TanStack Form **v1**, via
  context7) confirma que **Zod é *Standard Schema* e é aceito direto** em
  `validators.onChange`/`onDynamic` **sem adapter**; há `fieldApi.parseValueWithSchema` para
  validação por-campo. Sinergia central com este briefing: **o descritor gera tanto o input
  quanto a regra Zod** — o `local_rating` sai do descritor como `number` widget **e**
  `z.number().min(0).max(10)`, a mesma faixa que o backend valida em `formaNumero`. Fonte
  única de render + validação. A validação de cliente **não substitui** a do servidor — o
  422 continua autoritativo (regras que só o backend conhece).
- **Pegadinha do lockfile ao instalar as deps.** Adicionar `@tanstack/react-form`/`zod` mexe
  em `web/package-lock.json`, do qual o `npm ci` (CI/Docker) depende. O npm **local
  (11.6.2) < 11.16** corromperia o lockfile — ele **precisa ser gerado com npm ≥ 11.16**
  (ex.: `docker run --rm -v "$PWD/web":/w -w /w node:lts npm install`), nunca com o npm
  local. Regenerar, conferir que `npm ci` roda, e commitar `package.json` + `package-lock.json`
  juntos. (Regra do projeto para `web/`; o `e2e/` usa `npm install` e é gitignored.)
- **Rota, não modal.** O projeto não tem primitivo `Dialog` (decisão registrada nos 0025/
  0027). Rotas dão **deep-link**, **testabilidade** (casa com o 0028) e **não incham** o
  detalhe quando MD6/verticais chegarem. Alinha com a decisão do dono neste briefing.
- **Dois registros declarativos, não um.** Composição (`registroOrigem`, resolver) fica
  **pura**, sem concern de UI; apresentação (widget/label/ordem/faixa) vive no server. Um
  **teste os amarra** (todo editável tem descritor). Assim o resolver não vira dependente de
  UI e ainda garantimos que nunca haja campo editável sem front. Um plugin, no futuro,
  contribui para os dois via manifest — por isso ambos são **iterados**, não `switch`.
- **Widget derivável + override.** Default por `Kind`; override explícito para
  `description` (textarea) e `local_rating` (number 0–10). A faixa 0–10 do `local_rating`
  já é validada no backend (`formaNumero`); o descritor só a **espelha** para o input.
- **Criar × editar.** Mesma renderização de inputs, **submits distintos**: criação é `POST`
  de corpo plano (idioma principal, sem ops); edição é `PATCH` por idioma + `{add,remove}`
  de tags + `local_rating` + "Limpar". O flag `creatable` recorta o subconjunto da criação.
- **Labels pt-BR no descritor.** A UI é mono-idioma (pt-BR) hoje; label literal é aceitável.
  Quando a UI ganhar multi-idioma, o `label` vira **chave** de i18n — mudança localizada no
  descritor. Relaciona-se com [[multi-idioma-requisito-fundacional]].
- **Candidato a subir para `docs/`.** Se o "descritor de campo" se firmar como contrato de
  extensão (o ponto onde plugins declaram campos + UI), vira **ADR** e entra em
  [docs/04](../docs/04-modelo-de-dados.md)/[docs/11](../docs/11-decisoes-abertas.md).

## 7. Fora deste briefing / follow-ups

- **MD6 ([0029](0029-md6-elenco-equipe-person-credit-company.md))** plugará elenco/equipe no
  descritor (widget de créditos) — primeiro consumidor real da extensibilidade.
- **Maquinário de plugin** contribuindo descritores dinamicamente ([docs/11](../docs/11-decisoes-abertas.md)).
- **`external_ids` como parte do descritor genérico** (hoje é editor à parte na rota de edição).
- **Multi-idioma da UI** (labels como chaves de i18n).
</content>
</invoke>
