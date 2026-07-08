# 10 — Usuários e permissões

> **Status desta doc:** stub escopado. Detalhamento em briefings da Fase 4.

## Princípios

- **O dono define o limite.** Não há cota imposta pelo sistema — o inverso da API
  pública do TMDB ([cenário H](01-visao-e-casos-de-uso.md)).
- **Usuários e tokens.** O dono cria usuários e emite tokens de acesso à API.
- **Permissões graduáveis.** Ex.: `read`, `read+write`, `admin`. Escopos exatos a
  definir.

## Escopo (a detalhar)

- Modelo `user`, `token`, `permission`.
- Autenticação da UI (sessão) vs da API (token) — provável esquema unificado.
- Permissão por recurso? (ex.: um usuário só enxerga certas bibliotecas) — avaliar.

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- Esquema de token (JWT vs opaco + tabela), expiração, rotação.
- Primeiro-boot: criação do admin inicial.
