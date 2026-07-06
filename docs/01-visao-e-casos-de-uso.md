# 01 — Visão e casos de uso

A documentação do MyMediaDB é dirigida por **cenários normativos**: situações
reais que qualquer implementação precisa satisfazer. Se uma decisão de design
conflita com um cenário daqui, a decisão está errada — ou o cenário precisa ser
revisto explicitamente.

Os cenários abaixo definem o **quê**. As decisões de *como* vivem nas demais docs.

---

## Cenário A — Título brasileiro que diverge do oficial

**Contexto:** um filme dos anos 80 saiu na TV com um nome, no VHS com outro, e no
TMDB está com o título internacional. O dono quer o nome que faz sentido para ele.

Requisitos:

- **A1.** O dono importa o filme do TMDB e, depois, **edita o título** — sem
  perder o dado original.
- **A2.** A edição é uma **camada de override**: o valor externo continua
  registrado (para referência e re-scrape), mas o valor final exibido e servido
  na API é o do dono.
- **A3.** Re-puxar dados do TMDB **não sobrescreve** o override do dono — atualiza
  apenas a camada externa por baixo.

## Cenário B — Arte própria, sem aprovação de terceiros

**Contexto:** o dono tem um poster melhor (ou uma arte que o Fanart.tv rejeitou).

Requisitos:

- **B1.** O dono faz **upload de uma arte própria** e a marca como preferida para
  aquele item, sem depender de nenhuma moderação externa.
- **B2.** Artes externas e artes do dono coexistem; a **escolhida pelo dono** é a
  que a API serve por padrão.
- **B3.** Nenhuma arte custom é perdida ao re-enriquecer o item de fontes externas.

## Cenário C — Taxonomia própria (o caso do curta animado)

**Contexto:** o dono quer tratar curtas animados (ex.: shorts clássicos) como
**episódios de uma série** que ele mesmo define — algo que a filosofia do TMDB
recusa. Ou agrupar filmes numa coleção que não existe oficialmente.

Requisitos:

- **C1.** O dono pode **criar entidades e relações que não existem em nenhuma
  fonte externa** (uma série "caseira", uma coleção própria).
- **C2.** Itens sem correspondência externa são cidadãos de primeira classe — não
  são "incompletos", são **dele**.
- **C3.** A estrutura (série → temporada → episódio, coleção → filmes) é definida
  pelo dono, independente do que a fonte externa dita.

## Cenário D — O banco é maior que a biblioteca

**Contexto:** o dono quer registrar filmes/séries que **ainda não tem** — para
planejar, acompanhar lançamentos, montar wishlist — ao lado dos que já tem.

Requisitos:

- **D1.** Uma entidade existe **com ou sem arquivo** associado. Ter arquivo é um
  atributo, não um pré-requisito.
- **D2.** O campo **`status`** distingue estados (ex.: possuído, desejado,
  rascunho, não lançado) sem inventar tabelas paralelas.
- **D3.** É possível listar/filtrar "o que eu tenho" vs "o que eu quero" vs
  "rascunhos".

## Cenário E — Ler a biblioteca local e extrair metadados do arquivo

**Contexto:** o dono aponta pastas do servidor; o sistema descobre os arquivos e
extrai os fatos técnicos deles.

Requisitos:

- **E1.** O dono cadastra um ou mais **datasources** (pastas). O scan é
  **não-destrutivo** — não renomeia nem move nada sem ordem explícita.
- **E2.** Para cada arquivo de vídeo, extrai-se **mediainfo** (resolução, codecs,
  HDR, faixas de áudio/legenda, duração real…) e associa-se à entidade.
- **E3.** Um arquivo é identificável de forma estável mesmo que renomeado
  (hash de conteúdo), para não duplicar registros.

## Cenário F — Enriquecimento sob demanda (Pull > Sync)

**Contexto:** o dono quer complementar um item com dados externos, quando quiser.

Requisitos:

- **F1.** Buscar é **local-first**: primeiro no banco; se não houver, consulta a
  fonte externa e apresenta resultados marcados como **"não salvos"**.
- **F2.** A importação é **explícita** — o dono decide o que persistir.
- **F3.** Nada sincroniza automaticamente; o que é puxado é **cacheado localmente**
  (incl. imagens via proxy/cache) para não depender da fonte depois.

## Cenário G — Consumo por Jellyfin/Kodi/Plex

**Contexto:** o dono quer que o seu media server leia o catálogo do MyMediaDB em
vez de bater direto no TMDB.

Requisitos:

- **G1.** O MyMediaDB expõe uma **API compatível com TMDB** (namespace próprio),
  de modo que um cliente que já fala TMDB possa ser apontado para o servidor.
- **G2.** As respostas refletem o catálogo do dono **com os overrides aplicados**
  (título do dono, arte do dono).
- **G3.** As features fora do escopo do TMDB são servidas por uma **API nativa**
  separada, sem contaminar a camada de compatibilidade.

## Cenário H — Multiusuário com permissões

**Contexto:** o dono quer dar acesso a outras pessoas, com níveis diferentes.

Requisitos:

- **H1.** O dono cria **usuários** e emite **tokens** de acesso à API.
- **H2.** Permissões são graduáveis (ex.: só leitura, leitura+edição, admin).
- **H3.** O **limite é definido pelo dono** — não há cota imposta pelo sistema.

## Cenário I — Outras mídias além de filmes e séries (visão)

**Contexto:** o dono tem, no servidor, mídia que **nenhum banco cobre bem** e que
gostaria de catalogar com o mesmo rigor (o escopo universal do projeto).

Requisitos:

- **I1.** O **núcleo não assume** que só existem filmes e séries. Adicionar uma
  vertical nova **não exige alterar o schema do núcleo**.
- **I2.** Campos específicos de cada vertical vivem em **dados de extensão**
  isolados e validados (ver [04](04-modelo-de-dados.md)).
- **I3.** No MVP, esse mecanismo existe no modelo, mas só filmes/séries são
  entregues; verticais adicionais e **plugins de terceiros** vêm depois
  (ver [11](11-decisoes-abertas.md)).

---

## Não-cenários (deixados de fora de propósito)

- **Streaming/playback e transcode** — papel do media server, não deste projeto.
- **Reorganização compulsória de arquivos** — a verdade é o banco, não a pasta.
- **Catálogo público compartilhado / moderação global** — o dado é do dono.
