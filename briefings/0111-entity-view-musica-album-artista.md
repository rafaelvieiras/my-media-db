# 0111 — Variação de view de entidade para música (álbum/artista)

> Adapta a tela de detalhe de entity (`RichDetailView` + `EntityDetailScreen`)
> pra se comportar bem com álbum (tracklist) e artista (discografia), sem
> tornar a tela exclusiva de música — o padrão genérico continua servindo
> filme/série normalmente.

| | |
|---|---|
| **ID** | 0111 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | done |
| **Docs relacionadas** | [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md) |
| **Depende de** | 0107 |

---

## 1. Objetivo

Desde 0105-0107, artista/álbum/faixa já são catalogados de verdade (ex.:
Duran Duran com 25 álbuns/251 faixas, validado em produção), mas a tela de
detalhe (`web/src/components/rich-detail-view.tsx` +
`web/src/routes/entity-detail.tsx`) não tem nenhuma seção pensada pra esses
tipos — hoje as seções compartilhadas (elenco, equipe, produtoras, vídeos)
simplesmente ficam vazias/ocultas por ausência de dado (`if (x.length === 0)
return null`), mas não existe tracklist de álbum nem discografia de artista.
Este briefing fecha esse buraco, seguindo o padrão de extensão **já
estabelecido** no próprio código (branch por `data.type` no caller
`EntityDetailScreen`, igual já existe pra `tv`/`season`/`movie`/`collection`)
— **sem** criar uma tela paralela exclusiva de música.

## 2. Jornada do usuário

1. O dono abre o detalhe de um **álbum** catalogado. Além do que já é
   genérico (título, capa, ano, gêneros), vê a **tracklist** — faixas em
   ordem (`track_number`, `disc_number` quando houver mais de um disco),
   cada uuma linkável pro seu próprio detalhe de faixa se fizer sentido (ou
   inline, decidir na implementação).
2. O dono abre o detalhe de um **artista**. Vê sua **discografia** — grid
   de álbuns (reaproveitando `PosterCard`, igual `SecaoSerie` já faz pra
   temporadas), navegável pro detalhe de cada álbum.
3. O dono abre o detalhe de uma **faixa** (chegando via tracklist do
   álbum, ou direto). Vê os metadados básicos (artista, álbum, número da
   faixa, duração se disponível) — sem exigir uma seção nova elaborada, o
   genérico já cobre boa parte.
4. O dono abre o detalhe de um **filme/série** normalmente — nenhuma
   mudança de comportamento, nenhuma seção de música aparece.
5. Hero visual: álbum/artista usam capa quadrada (1:1) em vez do pôster
   2:3 de filme/série, no mesmo padrão em que episódio já troca o visual
   do hero (`detail.type === 'episode'`, `rich-detail-view.tsx:1398-1426`).

## 3. Escopo

**Dentro:**
- `HeroImersivo` (`rich-detail-view.tsx`): novo branch
  `detail.type === 'album' || detail.type === 'artista'` trocando o bloco
  de imagem (mesmo padrão do branch de `episode` já existente) — capa
  quadrada/circular conforme o tipo, sem alterar o resto do hero (título,
  badges, ações, sinopse continuam genéricos).
- `EntityDetailScreen` (`entity-detail.tsx`): novos branches condicionais
  por `data.type`, seguindo o padrão já usado pra `tv`/`season`:
  - `data.type === 'album'` → nova seção `TracklistAlbum` (lista de
    faixas via `entity_relation` kind `music_album`, ordenada por
    disco/faixa).
  - `data.type === 'artista'` → nova seção `DiscografiaArtista` (grid de
    álbuns via `entity_relation` kind `music_artista_principal`,
    reaproveitando `PosterCard`).
  - `data.type === 'faixa'` → sem seção nova obrigatória; avaliar na
    implementação se vale um badge/link "faz parte do álbum X" (dado já
    disponível via relação reversa).
- Endpoint(s) para essas seções: reaproveitar o que já existe para
  listar filhos de uma entity (o que já alimenta `SecaoSerie`/
  `MembrosSomenteLeitura` para série) — **não** criar endpoint novo se o
  existente já cobrir `entity_relation` genérica por `kind`.
- Confirmar que as seções compartilhadas (elenco, vídeos, produtoras,
  similares) continuam se auto-ocultando sem dado pra música — validar,
  não é esperado precisar mudar nada nelas.

**Fora (explicitamente):**
- Tela/rota exclusiva de música (`/music/...` dedicado) — a mesma rota
  genérica de entity (`/entities/:id`) serve todos os tipos, requisito
  explícito do dono ("não pode ser exclusiva").
- Player de áudio embutido — fora de escopo, não pedido.
- Edição/formulário rico de álbum/artista (CRUD manual) — fica pra um
  briefing futuro se o dono pedir; aqui é só visualização.
- Metadados adicionais de faixa (letra, duração exibida se não vier do
  provider) — usar só o que já está catalogado.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration — reaproveita 100% do modelo de ADR-0027/0105.

### API
- Verificar se o endpoint que hoje alimenta `SecaoSerie` (filhos de uma
  entity por `entity_relation`) já é genérico o bastante pra servir álbum→
  faixa e artista→álbum trocando só o `kind` filtrado, ou se precisa de um
  parâmetro a mais. Preferir reaproveitar sobre criar endpoint novo.

### Backend (Go)
- Só se o levantamento acima mostrar que o endpoint de filhos não é
  genérico o suficiente — nesse caso, generalizar (não duplicar lógica por
  tipo).

### Frontend (Vite/React)
- `web/src/components/rich-detail-view.tsx`: branch de hero por tipo.
- `web/src/routes/entity-detail.tsx`: branches de seção por tipo.
- `web/src/components/tracklist-album.tsx` (novo).
- `web/src/components/discografia-artista.tsx` (novo, reaproveitando
  `PosterCard`).

### Arquivos afetados
- `web/src/components/rich-detail-view.tsx`
- `web/src/routes/entity-detail.tsx`
- `web/src/components/tracklist-album.tsx` (novo)
- `web/src/components/discografia-artista.tsx` (novo)
- Backend: só se necessário (ver acima) — registrar decisão nas notas.

## 5. Critérios de aceite

- [ ] Detalhe de álbum real (ex.: um álbum do Duran Duran já catalogado em
      produção) mostra a tracklist completa, ordenada corretamente
      (inclusive múltiplos discos).
- [ ] Detalhe de artista real mostra a discografia completa, navegável.
- [ ] Detalhe de filme/série não muda visualmente (teste de regressão
      visual/snapshot ou E2E cobrindo que as seções de música não
      aparecem fora do tipo certo).
- [ ] Hero de álbum/artista usa capa 1:1 em vez de pôster 2:3.
- [ ] `tsc -b`, `npm run lint`, `npm run test:coverage` verdes.
- [ ] `make gate-local` limpo antes do commit final.
- [ ] Validação manual em produção contra dado real (biblioteca de música
      já catalogada em Batocera).

## 6. Notas de implementação / decisões

- Seguir literalmente o padrão de extensão já estabelecido pelo próprio
  código (branch em `EntityDetailScreen`, não em `RichDetailView`) —
  é a decisão que já resolve "não pode ser exclusiva" sem esforço extra:
  `RichDetailView` continua cego a domínio.

## 7. Fora deste briefing / follow-ups

- CRUD de álbum/artista via formulário.
- Player de áudio.
- Metadados de faixa além do já catalogado (duração, letra).
- **Achado na implementação (backend)**: `kindEOrdemMembros`
  (`internal/server/entities.go`) era hardcoded por tipo de pai
  (série/temporada/default-coleção) — `album`/`artista` caíam no `default`
  (`kind="collection"`), então `GET /entities/{id}/members` devolvia sempre
  `[]` para música, mesmo com dado real catalogado (ex.: Duran Duran).
  Corrigido: novos cases `tipoEntAlbum` → `musicext.KindAlbum` (ordena por
  `sort_index`/track_number) e `tipoEntArtista` →
  `musicext.KindArtistaPrincipal` (ordena por ano, mesma lógica de coleção)
  — `TracklistAlbum`/`DiscografiaArtista` já consumiam o endpoint genérico
  certo, só precisavam do backend reconhecer o tipo. Cobertura:
  `TestMembersAlbumTracklistPorSortIndex`/`TestMembersArtistaDiscografiaPorAno`
  em `internal/server/members_test.go`.
- Validação em produção (tracklist/discografia reais do Duran Duran) adiada
  para a rodada de deploy conjunta com 0108/0109/0110.
