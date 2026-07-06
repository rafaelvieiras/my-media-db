# 06 — Biblioteca e scan

> **Status desta doc:** stub escopado. Decisões travadas listadas; detalhamento
> em briefings da Fase 1.

## Princípios

- **Datasources** são pastas do servidor apontadas pelo dono. Um datasource tem
  um `type` (ex.: filmes, séries) e pode conter várias pastas.
- **Scan não-destrutivo.** A varredura descobre arquivos e extrai metadados;
  **nunca** renomeia nem move nada sem ordem explícita. A verdade é o banco, não
  o nome do arquivo ([cenário E](01-visao-e-casos-de-uso.md)).
- **Mediainfo por arquivo.** Para cada arquivo de vídeo, extraem-se os fatos
  técnicos (resolução, codecs, HDR, faixas de áudio/legenda, duração real).
- **Dedup por conteúdo.** Cada arquivo tem um `content_hash` estável, para não
  duplicar registro ao renomear/mover.
- **Matching.** O arquivo é associado a uma `Entity` (e, opcionalmente, a um id
  externo). Estratégia de matching a detalhar.

## Referência reaproveitável (v1)

Preservado em scratchpad da sessão de planejamento — reescrever em Go, não portar:

- **Extensões de vídeo válidas:** `.mp4 .mkv .avi .mov .wmv .flv .webm` (ponto de
  partida; ampliar).
- **Shape de mediainfo** (general/video/audio) já mapeado na v1 — bom contrato de
  referência para o que extrair.

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- **Ferramenta de extração: `ffprobe` (inclinação) vs `mediainfo-cli`.**
- Scan incremental (mtime/hash) vs full; watching de filesystem vs sob demanda.
- Estratégia de matching (por nome/ano, por hash, por id embutido em NFO existente).
