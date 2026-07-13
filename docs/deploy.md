# Deploy

Guia prático para colocar o MyMediaDB em produção: imagem Docker única,
publicada no GitHub Container Registry e mantida atualizada sozinha.

## 1. Visão geral

O MyMediaDB roda como uma imagem Docker única (Go + SPA embutido, `Dockerfile`
na raiz), publicada no GitHub Container Registry em
`ghcr.io/rafaelvieiras/my-media-db` — **privada** por enquanto (o repositório
pode virar OSS depois, mas o registry não acompanha automaticamente).

A imagem é **multi-arch**: `linux/amd64`, `linux/arm64` e `linux/arm/v7`. O
Docker escolhe sozinho a variante certa a partir do manifesto multi-arch, sem
nenhuma configuração extra no alvo.

Fluxo ponta a ponta:

1. Push no `master` (ou tag `v*`, ou disparo manual) → o workflow
   `.github/workflows/release.yml` builda a imagem para as 3 arquiteturas e
   publica no ghcr com a tag `:latest` (mais `sha` curto e, em tags, semver).
2. No alvo (este PC, um Raspberry Pi, etc.), o `compose.prod.yaml` sobe o
   app **e** um Watchtower que fica de olho no registry.
3. A cada 5 minutos o Watchtower checa se o `:latest` mudou; se mudou, puxa a
   imagem nova, recria o container e limpa a imagem antiga.

Não há passo manual de deploy no dia a dia: depois do setup inicial, um push
no master já chega no alvo sozinho.

## 2. Pré-requisitos

- Docker + Docker Compose (plugin `docker compose`) instalados no alvo.
- Um GitHub Personal Access Token (classic) com escopo `read:packages` — só
  leitura é suficiente para puxar a imagem privada (o CI usa o token
  automático do workflow para publicar, isso aqui é só para o alvo puxar).

## 3. Login no ghcr (uma vez por máquina)

```bash
echo <SEU_PAT> | docker login ghcr.io -u rafaelvieiras --password-stdin
```

Isso grava a credencial em `~/.docker/config.json`. Tanto o `docker compose
pull` quanto o container do Watchtower (que monta esse arquivo read-only)
dependem dela para autenticar contra o registry privado — sem login, ambos
recebem `denied`/`unauthorized`.

## 4. Subir neste PC (teste)

```bash
# 1) Modelo de credenciais opcionais (TMDB/fanart.tv/TVDB)
cp .env.prod.example .env
# ... edite .env e preencha o que tiver (pode deixar tudo vazio)

# 2) Puxa a imagem publicada
docker compose -f compose.prod.yaml pull

# 3) Sobe app + Watchtower
docker compose -f compose.prod.yaml up -d
```

Acesse em `http://localhost:8096` (mapeamento `8096:8080` já definido no
compose). Para acompanhar os logs do app:

```bash
docker compose -f compose.prod.yaml logs -f app
```

## 5. Auto-update

O serviço `watchtower` do `compose.prod.yaml` vigia containers com o label
`com.centurylinklabs.watchtower.enable=true` (só o `app` tem esse label) e,
a cada `WATCHTOWER_POLL_INTERVAL=300` segundos (5 min), verifica se a tag
`:latest` mudou no ghcr. Se mudou: puxa a imagem nova, recria o container
`mymediadb` com os mesmos volumes/env, e remove a imagem antiga
(`WATCHTOWER_CLEANUP=true`) para não acumular lixo em disco.

Para forçar uma atualização na hora, sem esperar o poll:

```bash
docker compose -f compose.prod.yaml pull && docker compose -f compose.prod.yaml up -d
```

Ou, alternativamente, reinicie só o Watchtower (ele checa assim que sobe):

```bash
docker compose -f compose.prod.yaml restart watchtower
```

## 6. Raspberry Pi (1GB RAM)

Os mesmos passos das seções 3–5 valem para o Pi: copie `compose.prod.yaml`,
`.env.prod.example` (vire `.env`), faça o `docker login ghcr.io` no próprio
Pi e suba com `docker compose -f compose.prod.yaml up -d`.

- O manifesto multi-arch faz o `:latest` resolver automaticamente para a
  arquitetura do Pi — **32-bit** → `linux/arm/v7`; **64-bit** → `linux/arm64`.
  Não precisa escolher nada manualmente.
- `WATCHTOWER_CLEANUP=true` é essencial aqui: sem ele, cada atualização deixa
  a imagem antiga no cartão SD, que é pequeno.
- O binário Go (estático, sem CGO) + SQLite puro-Go (modernc) rodam
  folgados em 1GB de RAM — não há JVM, Node, nem toolchain de build na
  imagem final (só o binário + ffmpeg).

## 7. Solução de problemas

| Sintoma | Causa provável | Ação |
|---|---|---|
| `denied` / `unauthorized` no `pull` | Login no ghcr ausente ou expirado | Refazer `docker login ghcr.io -u rafaelvieiras --password-stdin` |
| `exec format error` ao subir o container | Arch do manifesto não bate com o host | Não deveria ocorrer com os 3 alvos publicados (amd64/arm64/armv7); confira `docker version` → `Server.Arch` |
| Porta 8096 já em uso | Outro serviço ocupando a porta no host | Trocar o mapeamento em `compose.prod.yaml` (ex.: `"8091:8080"`) |
| App sobe mas busca externa (TMDB/fanart.tv/TVDB) não funciona | `.env` vazio ou sem as chaves | Preencher `.env` a partir de `.env.prod.example` — é opcional, mas sem chave a busca fica desligada |
