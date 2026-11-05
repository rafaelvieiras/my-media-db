# 0130 — Healthcheck da imagem respeita a porta configurada

> Container com `PORT` diferente de 8080 para de ficar eternamente `unhealthy`.

| | |
|---|---|
| **ID** | 0130 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 1 — Esqueleto do container único (empacotamento) |
| **Status** | in-progress |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md) |
| **Depende de** | — |

---

## 1. Objetivo

O `HEALTHCHECK` do `Dockerfile` testa `http://localhost:8080/api/v1/health`
fixo, mas a porta é configurável por `PORT` — e o ambiente de produção da sala
roda com `PORT=8097` (8096 é do Jellyfin). Com isso o Docker marca o container
como `unhealthy` para sempre, mesmo com a aplicação saudável: em 05/nov/2026 o
`FailingStreak` estava em **52.436** tentativas, `/api/v1/health` respondendo
`200 {"status":"ok","db":{"ok":true,"schema":27}}`.

O efeito prático é ruído que esconde falha de verdade: quem olha `docker ps`
perde a única indicação barata de que o serviço caiu, e qualquer supervisão que
use o estado de saúde (Arcane, compose `depends_on: condition`) fica inútil.

## 2. Jornada do usuário

1. O dono sobe o container com `PORT=8097` (ou qualquer porta livre).
2. O Docker roda o healthcheck da própria imagem.
3. **Antes:** o check batia em 8080, nada escutava ali, `unhealthy` eterno.
4. **Depois:** o check usa a mesma porta em que a aplicação escuta; `healthy`
   em poucos segundos, e `unhealthy` volta a significar problema real.

## 3. Escopo

**Dentro:**
- `HEALTHCHECK` do `Dockerfile` passa a usar `${PORT:-8080}`.
- Comentário do bloco explicando a expansão (o default 8080 continua valendo
  para quem não define `PORT`).

**Fora (explicitamente):**
- Mudar a forma do endpoint `/api/v1/health` ou seu conteúdo.
- Adicionar healthcheck nos `compose*.yaml` (eles herdam o da imagem de
  propósito, ver `compose.prod.yaml`).
- Mexer na porta padrão da aplicação.

## 4. Contrato técnico

### Backend (Go)
- Nenhuma mudança de código Go.

### Empacotamento
- `Dockerfile`:
  ```
  HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
      CMD wget -q --spider "http://localhost:${PORT:-8080}/api/v1/health" || exit 1
  ```
  A forma shell (`CMD` sem array JSON) é o que permite a expansão de variável;
  o `wget` do busybox já está na imagem final (alpine).

### Arquivos afetados
- `Dockerfile`

## 5. Critérios de aceite

- [ ] Com `PORT=8097`, o container fica `healthy` sem esperar o
      `--start-period` vencer por completo.
- [ ] Sem `PORT`, o check continua batendo em 8080 (default da aplicação).
- [ ] `docker inspect --format '{{.Config.Healthcheck}}'` mostra a expansão.

## 6. Notas de implementação / decisões

- **Por que `${PORT:-8080}` e não o `PORT` obrigatório**: a aplicação mantém
  8080 como default quando `PORT` não vem do ambiente; o check segue a mesma
  regra para não introduzir um segundo default divergente.
- **Por que não trocar por `CMD-SHELL` explícito**: a forma shell do
  `HEALTHCHECK CMD` já é executada por `/bin/sh -c`, então `CMD` puro basta e
  fica igual ao resto do arquivo.

## 7. Fora deste briefing / follow-ups

- Nenhum.
