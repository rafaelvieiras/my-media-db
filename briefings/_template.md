# NNNN — Título do briefing

> Uma frase: o que este briefing entrega.

| | |
|---|---|
| **ID** | NNNN |
| **Tipo** | feature \| fix \| chore \| spike |
| **Fase (roadmap)** | ex.: Fase 1 — Esqueleto do container único |
| **Status** | draft \| ready \| in-progress \| done \| blocked \| dropped |
| **Docs relacionadas** | ex.: [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Depende de** | ID(s) de outro(s) briefing(s), ou — |

---

## 1. Objetivo

O que se quer alcançar e **por quê**. Qual dor do usuário ou necessidade técnica
isso resolve. Uma pessoa que nunca viu o projeto deveria entender a motivação.

## 2. Jornada do usuário

O fluxo do ponto de vista de **quem usa** — passo a passo, do gatilho ao
resultado. Inclua o caminho feliz e os desvios que importam (erros, estados
vazios, casos de borda visíveis ao usuário).

> Ex.:
> 1. O usuário abre a tela de Bibliotecas e clica em "Adicionar datasource".
> 2. Informa um caminho; o sistema valida se existe e é legível.
> 3. Ao confirmar, um scan inicia em background com indicador de progresso.
> 4. Ao terminar, os itens detectados aparecem no grid marcados como "não enriquecidos".

## 3. Escopo

**Dentro:**
- …

**Fora (explicitamente):**
- … *(o que este briefing NÃO cobre, para evitar arrasto de escopo)*

## 4. Contrato técnico

Detalhamento para implementar. Preencher o que se aplica; remover o que não.

### Dados / modelo
- Tabelas/campos afetados, migrações, shape de `extension_data` se houver.

### API
- Endpoints (método, rota, request, response), namespace (`/api/v1` ou `/api/tmdb`).
- Erros e status codes relevantes.

### Backend (Go)
- Pacotes/serviços tocados, subprocessos (ex.: ffprobe), jobs em background.

### Frontend (Vite/React)
- Telas/rotas, componentes, estado, chamadas de API, estados de loading/erro/vazio.

### Arquivos afetados
- Lista (ou previsão) dos arquivos a criar/alterar.

## 5. Critérios de aceite

Lista verificável. Cada item deve ser testável (idealmente vira teste).

- [ ] …
- [ ] …

## 6. Notas de implementação / decisões

Escolhas técnicas feitas aqui, alternativas descartadas e o porquê. Se algo virar
decisão estrutural, subir para a `docs/` correspondente e linkar.

## 7. Fora deste briefing / follow-ups

Ideias e trabalhos que surgiram mas ficam para outro briefing (com link quando
criado).
