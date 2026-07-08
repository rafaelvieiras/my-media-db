# ADR-0008 — Identidade e matching pela convenção Kodi/Jellyfin

- **Status:** Aceita
- **Data:** 2026-07-07
- **Agenda:** [MD1](../00-agenda-de-decisoes.md) · **Spec:** [06](../06-biblioteca-e-scan.md), [04](../04-modelo-de-dados.md)

## Contexto

Faltava definir como um arquivo vira/associa a uma `Entity` e a um id externo, e
como modelar um filme com múltiplos arquivos (edições/qualidades). Inventar regra
própria seria reinventar a roda de um problema já resolvido pelo ecossistema.

## Decisão

Adotar a **convenção de nomenclatura do Kodi/Jellyfin** (padrão de-facto; Emby,
Plex e o stack \*arr na mesma família). Consequências: **grão 1:N plano** (1 obra =
1 `Entity`; qualidades/edições/partes = `media_file`), **edição = rótulo no
arquivo**, **id externo embutido no nome = chave determinística**, *stacking* de
partes. Todo match carrega um **score de confiança**: alta casa sozinho; baixa casa
com o melhor palpite **+ alerta de revisão** (*"mapeado com 60% de certeza. Remover?"*);
zero resultados vira `draft` não-identificado. Música segue o mesmo princípio via
MusicBrainz + tags. Verticais fora do padrão (videoclipe→IMVDB, quadrinhos→ComicInfo)
trazem identidade própria via `extension_data`, sem tocar no núcleo.

## Consequências

- O scan é **leitor** do padrão, nunca o impõe (não renomeia nada — não-destrutivo).
- A correção do dono é autoritativa (entra pelo override).
- Valida [ADR-0001](0001-resolver-particionamento-por-origem.md)/[ADR-0005](0005-composicao-uniforme-extension-data.md):
  verticais plugam por `extension_data`.
