// Review queue, orphans, the events without item, the consumption history and
// the cataloging completeness form/badge.
import { defineDomain } from '../types'

export const review = defineDomain({
  en: {
    queue: {
      title: 'Review',
      subtitle:
        'Items matched with low confidence and unidentified items, grouped by album, series or source folder.',
      autoSyncAll: 'Sync eligible candidates',
      syncing: 'Syncing…',
      autoSyncSummary:
        '{resolved} resolved automatically, {pending} still need manual review.',
      countInGroups_one: 'in {n} group',
      countInGroups_other: 'in {n} groups',
      loading: 'Loading the review queue…',
      loadError: 'Could not load the review',
      emptyTitle: 'Nothing to review',
      emptyDescription:
        'No item is waiting for curation — everything matched with confidence.',
      thresholdHint:
        'Items below 70% confidence land here; from 92% on, auto-sync applies the candidate by itself.',
      autoSyncConfirmTitle: 'Sync the eligible candidates?',
      autoSyncConfirm_one:
        'This evaluates {n} item of the queue and re-enriches the ones that match.',
      autoSyncConfirm_other:
        'This evaluates {n} items of the queue and re-enriches the ones that match.',
      autoSyncConfirmAction: 'Sync candidates',
      filteredOf_one: '{shown} of {total} item',
      filteredOf_other: '{shown} of {total} items',
    },
    group: {
      autoSync: 'Auto-sync this group',
      summary: '{resolved} resolved · {pending} still pending',
      kind: {
        pasta: 'folder',
        serie: 'series',
        album: 'album',
      },
      shownOf: 'Showing {shown} of {total}',
      showMore: 'Show more ({n})',
      autoSyncConfirmTitle: 'Sync the eligible candidates of this group?',
      autoSyncConfirm_one:
        'This evaluates {n} item of this group and re-enriches the ones that match.',
      autoSyncConfirm_other:
        'This evaluates {n} items of this group and re-enriches the ones that match.',
    },
    row: {
      posterAlt: 'Poster for {title}',
      showCandidate: 'Show candidate',
      confirmCandidate: 'Confirm candidate',
      confirming: 'Confirming…',
      keepAsIs: 'Keep as is (no external metadata)',
      correct: 'Correct',
      viewDetail: 'View details',
      mergeNotice:
        'Duplicate merged automatically — the item already imported was combined with this one.',
      notIdentified: 'not identified',
      lowConfidence: 'low confidence ({score}%)',
      appliedKeep: '"{title}" kept as is — the identity was written.',
      appliedCandidate:
        '"{title}" — candidate applied, the item was re-enriched.',
      keepConfirmTitle: 'Keep as is?',
      keepConfirmDescription:
        'This writes the current identity as final and re-enriches the item. There is no undo.',
      keepConfirmAction: 'Keep as is',
    },
    preview: {
      searching: 'Looking for a candidate…',
      searchError: 'Could not look up a candidate — try again later.',
      none: 'no candidate found — neither in the local catalog nor in the external source',
      confidence: 'confidence {score}%',
      methodLocal: 'local catalog',
      methodExternal: 'external provider',
      strongMatch: 'strong match',
      checkBeforeConfirming: 'check before confirming',
      alternateTitle: 'via alternate title: "{title}"',
    },
    correction: {
      searchLabel: 'Search for the correct title',
      searchPlaceholder: 'Search for the correct title…',
      hint: 'Type the correct title to choose the right identity.',
      searching: 'Searching…',
      searchFailed: 'Could not search',
      unavailableTitle: 'External search unavailable',
      unavailableMessage:
        'The external source did not respond or is not configured.',
      noResults: 'No external results for this search.',
    },
    orphans: {
      title: 'Orphans',
      subtitle:
        'Auto-created items whose file is gone and that you never edited. Nothing is deleted on its own — a purge is always yours and explicit.',
      loading: 'Loading the orphans…',
      loadError: 'Could not load the orphans',
      emptyTitle: 'No orphans',
      emptyDescription:
        'No auto-created item lost its file — there is nothing to clean up.',
      selectAll: 'Select all',
      selected_one: '{n} selected',
      selected_other: '{n} selected',
      purgeSelected: 'Purge selected',
      confirmBatch_one:
        'Are you sure? This removes {n} item and cannot be undone.',
      confirmBatch_other:
        'Are you sure? This removes {n} items and cannot be undone.',
      confirmTitle: 'Confirm purge',
      confirmPurge: 'Confirm purge',
      purging: 'Purging…',
      purge: 'Purge',
      purgeItem: 'Purge {title}',
      selectItem: 'Select {title}',
      confirmOne: 'Removes this item and cannot be undone.',
    },
    orphanEvents: {
      title: 'Events without a related item',
      subtitle:
        'Playback events reported by a webhook that did not match any cataloged item. When you import or correct the identity (external IDs) of the matching item, the event is linked automatically — there is nothing to do here.',
      loading: 'Loading events…',
      loadError: 'Could not load the events without a related item',
      emptyTitle: 'No event without a related item',
      emptyDescription:
        'Every playback event received so far matched a cataloged item.',
      range: '{from}–{to} of {total}',
      untitled: 'Item without a title',
      untitledWithIds: 'Item without a title ({ids})',
    },
    history: {
      badge: 'Watched on {date}',
      sectionTitle: 'History',
      loading: 'Loading history…',
      loadError: 'Could not load the history',
      deleteLabel: 'Delete event',
      deleting: 'Deleting…',
      confirmDelete: 'Delete?',
      deleteFailed: 'Could not delete the event.',
      status: {
        started: 'Started watching',
        paused: 'Paused',
        stopped: 'Stopped',
        completed: 'Watched',
      },
      origin: {
        manual: 'Recorded manually',
      },
    },
    completeness: {
      loading: 'Loading cataloging completeness…',
      loadError: 'Could not load cataloging completeness',
      information: 'Information',
      castAndFile: 'Cast + file',
      artwork: 'Artwork',
      field: {
        description: 'Synopsis',
        runtime: 'Duration',
        certification: 'Age rating',
        keywords: 'Keywords',
        cast: 'Cast',
        media_file: 'Media file',
      },
      artworkKind: {
        poster: 'Poster',
        fanart: 'Fanart',
        logo: 'Logo',
        clearart: 'Clearart',
        banner: 'Banner',
        thumb: 'Thumb',
        disc: 'Disc cover',
      },
      incomplete: 'Incomplete cataloging: {missing}',
      artworkItem: '{kind} artwork',
    },
    filters: {
      searchLabel: 'Search the queue by title',
      searchPlaceholder: 'Search by title, series, album or folder…',
      kindLabel: 'Filter by group type',
      kindAll: 'All types',
      reasonLabel: 'Filter by reason',
      reasonAll: 'All reasons',
      reasonNotIdentified: 'Not identified',
      reasonLowConfidence: 'Low confidence',
      clear: 'Clear filters',
      noResultsTitle: 'No item matches the filters',
      noResultsDescription:
        'Clear the search or change the filters to see the queue again.',
    },
  },
  'pt-BR': {
    queue: {
      title: 'Revisão',
      subtitle:
        'Itens casados com baixa confiança e não identificados, agrupados pelo álbum, série ou pasta de origem.',
      autoSyncAll: 'Sincronizar candidatos elegíveis',
      syncing: 'Sincronizando…',
      autoSyncSummary:
        '{resolved} resolvidos automaticamente, {pending} continuam para revisão manual.',
      countInGroups_one: 'em {n} grupo',
      countInGroups_other: 'em {n} grupos',
      loading: 'Carregando a fila de revisão…',
      loadError: 'Não foi possível carregar a revisão',
      emptyTitle: 'Nada para revisar',
      emptyDescription:
        'Nenhum item pendente de curadoria — tudo casado com confiança.',
      thresholdHint:
        'Itens com confiança abaixo de 70% entram aqui; a partir de 92% o auto-sync aplica o candidato sozinho.',
      autoSyncConfirmTitle: 'Sincronizar os candidatos elegíveis?',
      autoSyncConfirm_one:
        'Isso avalia {n} item da fila e re-enriquece os que casarem.',
      autoSyncConfirm_other:
        'Isso avalia {n} itens da fila e re-enriquece os que casarem.',
      autoSyncConfirmAction: 'Sincronizar candidatos',
      filteredOf_one: '{shown} de {total} item',
      filteredOf_other: '{shown} de {total} itens',
    },
    group: {
      autoSync: 'Auto-sincronizar este grupo',
      summary: '{resolved} resolvidos · {pending} ainda pendentes',
      kind: {
        pasta: 'pasta',
        serie: 'série',
        album: 'álbum',
      },
      shownOf: 'Mostrando {shown} de {total}',
      showMore: 'Mostrar mais ({n})',
      autoSyncConfirmTitle: 'Sincronizar os candidatos elegíveis deste grupo?',
      autoSyncConfirm_one:
        'Isso avalia {n} item deste grupo e re-enriquece os que casarem.',
      autoSyncConfirm_other:
        'Isso avalia {n} itens deste grupo e re-enriquece os que casarem.',
    },
    row: {
      posterAlt: 'Pôster de {title}',
      showCandidate: 'Ver candidato',
      confirmCandidate: 'Confirmar candidato',
      confirming: 'Confirmando…',
      keepAsIs: 'Manter como está (sem metadados externos)',
      correct: 'Corrigir',
      viewDetail: 'Ver detalhe',
      mergeNotice:
        'Duplicata mesclada automaticamente — o item já importado foi combinado com este.',
      notIdentified: 'não identificado',
      lowConfidence: 'baixa confiança ({score}%)',
      appliedKeep: '"{title}" mantido como está — a identidade foi gravada.',
      appliedCandidate:
        '"{title}" — candidato aplicado, o item foi re-enriquecido.',
      keepConfirmTitle: 'Manter como está?',
      keepConfirmDescription:
        'Isso grava a identidade atual como definitiva e re-enriquece o item. Não há como desfazer.',
      keepConfirmAction: 'Manter como está',
    },
    preview: {
      searching: 'Buscando candidato…',
      searchError:
        'Não foi possível buscar um candidato — tente novamente mais tarde.',
      none: 'nenhum candidato encontrado — nem no catálogo local, nem na fonte externa',
      confidence: 'confiança {score}%',
      methodLocal: 'catálogo local',
      methodExternal: 'provedor externo',
      strongMatch: 'match forte',
      checkBeforeConfirming: 'confira antes de confirmar',
      alternateTitle: 'via título alternativo: "{title}"',
    },
    correction: {
      searchLabel: 'Buscar o título correto',
      searchPlaceholder: 'Buscar o título correto…',
      hint: 'Digite o título correto para escolher a identidade certa.',
      searching: 'Buscando…',
      searchFailed: 'Não foi possível buscar',
      unavailableTitle: 'Busca externa indisponível',
      unavailableMessage:
        'A fonte externa não respondeu ou não está configurada.',
      noResults: 'Nenhum resultado externo para esta busca.',
    },
    orphans: {
      title: 'Órfãs',
      subtitle:
        'Itens auto-criados cujo arquivo sumiu e sem edição sua. Nada é apagado sozinho — a purga é sempre sua e explícita.',
      loading: 'Carregando as órfãs…',
      loadError: 'Não foi possível carregar as órfãs',
      emptyTitle: 'Nenhuma órfã',
      emptyDescription:
        'Nenhum item auto-criado ficou sem arquivo — não há nada para limpar.',
      selectAll: 'Selecionar todas',
      selected_one: '{n} selecionado',
      selected_other: '{n} selecionados',
      purgeSelected: 'Purgar selecionadas',
      confirmBatch_one:
        'Tem certeza? Isso remove {n} item e não pode ser desfeito.',
      confirmBatch_other:
        'Tem certeza? Isso remove {n} itens e não pode ser desfeito.',
      confirmTitle: 'Confirmar purga',
      confirmPurge: 'Confirmar purga',
      purging: 'Purgando…',
      purge: 'Purgar',
      purgeItem: 'Purgar {title}',
      selectItem: 'Selecionar {title}',
      confirmOne: 'Remove este item e não pode ser desfeito.',
    },
    orphanEvents: {
      title: 'Eventos sem item relacionado',
      subtitle:
        'Eventos de reprodução reportados por um webhook que não bateram com nenhum item catalogado. Quando você importar ou corrigir a identidade (IDs externos) do item correspondente, o evento é vinculado automaticamente — não é preciso fazer nada aqui.',
      loading: 'Carregando eventos…',
      loadError: 'Não foi possível carregar os eventos sem item relacionado',
      emptyTitle: 'Nenhum evento sem item relacionado',
      emptyDescription:
        'Todo evento de reprodução recebido até agora casou com algum item catalogado.',
      range: '{from}–{to} de {total}',
      untitled: 'Item sem título',
      untitledWithIds: 'Item sem título ({ids})',
    },
    history: {
      badge: 'Assistido em {date}',
      sectionTitle: 'Histórico',
      loading: 'Carregando histórico…',
      loadError: 'Não foi possível carregar o histórico',
      deleteLabel: 'Apagar evento',
      deleting: 'Apagando…',
      confirmDelete: 'Apagar?',
      deleteFailed: 'Não foi possível apagar o evento.',
      status: {
        started: 'Começou a assistir',
        paused: 'Pausou',
        stopped: 'Parou',
        completed: 'Assistiu',
      },
      origin: {
        manual: 'Registrado manualmente',
      },
    },
    completeness: {
      loading: 'Carregando completude de catalogação…',
      loadError: 'Não foi possível carregar a completude de catalogação',
      information: 'Informações',
      castAndFile: 'Elenco + arquivo',
      artwork: 'Artes',
      field: {
        description: 'Sinopse',
        runtime: 'Duração',
        certification: 'Classificação indicativa',
        keywords: 'Palavras-chave',
        cast: 'Elenco',
        media_file: 'Arquivo de mídia',
      },
      artworkKind: {
        poster: 'Poster',
        fanart: 'Fanart',
        logo: 'Logo',
        clearart: 'Clearart',
        banner: 'Banner',
        thumb: 'Thumb',
        disc: 'Capa de disco',
      },
      incomplete: 'Catalogação incompleta: {missing}',
      artworkItem: 'arte {kind}',
    },
    filters: {
      searchLabel: 'Buscar na fila por título',
      searchPlaceholder: 'Buscar por título, série, álbum ou pasta…',
      kindLabel: 'Filtrar por tipo de grupo',
      kindAll: 'Todos os tipos',
      reasonLabel: 'Filtrar por motivo',
      reasonAll: 'Todos os motivos',
      reasonNotIdentified: 'Não identificado',
      reasonLowConfidence: 'Baixa confiança',
      clear: 'Limpar filtros',
      noResultsTitle: 'Nenhum item corresponde aos filtros',
      noResultsDescription:
        'Limpe a busca ou mude os filtros para ver a fila novamente.',
    },
  },
  es: {
    queue: {
      title: 'Revisión',
      subtitle:
        'Elementos emparejados con baja confianza y no identificados, agrupados por el álbum, la serie o la carpeta de origen.',
      autoSyncAll: 'Sincronizar candidatos elegibles',
      syncing: 'Sincronizando…',
      autoSyncSummary:
        '{resolved} resueltos automáticamente, {pending} siguen para revisión manual.',
      countInGroups_one: 'en {n} grupo',
      countInGroups_other: 'en {n} grupos',
      loading: 'Cargando la cola de revisión…',
      loadError: 'No se pudo cargar la revisión',
      emptyTitle: 'Nada para revisar',
      emptyDescription:
        'Ningún elemento pendiente de curación: todo quedó emparejado con confianza.',
      thresholdHint:
        'Los elementos con confianza por debajo del 70% entran aquí; a partir del 92% el auto-sync aplica el candidato solo.',
      autoSyncConfirmTitle: '¿Sincronizar los candidatos elegibles?',
      autoSyncConfirm_one:
        'Esto evalúa {n} elemento de la cola y vuelve a enriquecer los que coincidan.',
      autoSyncConfirm_other:
        'Esto evalúa {n} elementos de la cola y vuelve a enriquecer los que coincidan.',
      autoSyncConfirmAction: 'Sincronizar candidatos',
      filteredOf_one: '{shown} de {total} elemento',
      filteredOf_other: '{shown} de {total} elementos',
    },
    group: {
      autoSync: 'Sincronizar este grupo',
      summary: '{resolved} resueltos · {pending} aún pendientes',
      kind: {
        pasta: 'carpeta',
        serie: 'serie',
        album: 'álbum',
      },
      shownOf: 'Mostrando {shown} de {total}',
      showMore: 'Mostrar más ({n})',
      autoSyncConfirmTitle:
        '¿Sincronizar los candidatos elegibles de este grupo?',
      autoSyncConfirm_one:
        'Esto evalúa {n} elemento de este grupo y vuelve a enriquecer los que coincidan.',
      autoSyncConfirm_other:
        'Esto evalúa {n} elementos de este grupo y vuelve a enriquecer los que coincidan.',
    },
    row: {
      posterAlt: 'Póster de {title}',
      showCandidate: 'Ver candidato',
      confirmCandidate: 'Confirmar candidato',
      confirming: 'Confirmando…',
      keepAsIs: 'Dejar como está (sin metadatos externos)',
      correct: 'Corregir',
      viewDetail: 'Ver detalle',
      mergeNotice:
        'Duplicado fusionado automáticamente: el elemento ya importado se combinó con este.',
      notIdentified: 'no identificado',
      lowConfidence: 'baja confianza ({score}%)',
      appliedKeep: '"{title}" se mantuvo como está: la identidad se guardó.',
      appliedCandidate:
        '"{title}": candidato aplicado, el elemento se volvió a enriquecer.',
      keepConfirmTitle: '¿Dejar como está?',
      keepConfirmDescription:
        'Esto guarda la identidad actual como definitiva y vuelve a enriquecer el elemento. No se puede deshacer.',
      keepConfirmAction: 'Dejar como está',
    },
    preview: {
      searching: 'Buscando un candidato…',
      searchError: 'No se pudo buscar un candidato; inténtalo más tarde.',
      none: 'no se encontró ningún candidato, ni en el catálogo local ni en la fuente externa',
      confidence: 'confianza {score}%',
      methodLocal: 'catálogo local',
      methodExternal: 'proveedor externo',
      strongMatch: 'coincidencia fuerte',
      checkBeforeConfirming: 'revisa antes de confirmar',
      alternateTitle: 'vía título alternativo: "{title}"',
    },
    correction: {
      searchLabel: 'Buscar el título correcto',
      searchPlaceholder: 'Buscar el título correcto…',
      hint: 'Escribe el título correcto para elegir la identidad correcta.',
      searching: 'Buscando…',
      searchFailed: 'No se pudo buscar',
      unavailableTitle: 'Búsqueda externa no disponible',
      unavailableMessage:
        'La fuente externa no respondió o no está configurada.',
      noResults: 'Ningún resultado externo para esta búsqueda.',
    },
    orphans: {
      title: 'Huérfanos',
      subtitle:
        'Elementos creados automáticamente cuyo archivo desapareció y que nunca editaste. Nada se borra solo: la purga siempre es tuya y explícita.',
      loading: 'Cargando los huérfanos…',
      loadError: 'No se pudieron cargar los huérfanos',
      emptyTitle: 'Ningún huérfano',
      emptyDescription:
        'Ningún elemento creado automáticamente se quedó sin archivo: no hay nada que limpiar.',
      selectAll: 'Seleccionar todos',
      selected_one: '{n} seleccionado',
      selected_other: '{n} seleccionados',
      purgeSelected: 'Purgar seleccionados',
      confirmBatch_one:
        '¿Seguro? Esto elimina {n} elemento y no se puede deshacer.',
      confirmBatch_other:
        '¿Seguro? Esto elimina {n} elementos y no se puede deshacer.',
      confirmTitle: 'Confirmar purga',
      confirmPurge: 'Confirmar purga',
      purging: 'Purgando…',
      purge: 'Purgar',
      purgeItem: 'Purgar {title}',
      selectItem: 'Seleccionar {title}',
      confirmOne: 'Elimina este elemento y no se puede deshacer.',
    },
    orphanEvents: {
      title: 'Eventos sin elemento relacionado',
      subtitle:
        'Eventos de reproducción informados por un webhook que no coincidieron con ningún elemento catalogado. Cuando importes o corrijas la identidad (IDs externos) del elemento correspondiente, el evento se vincula automáticamente: no hay nada que hacer aquí.',
      loading: 'Cargando eventos…',
      loadError: 'No se pudieron cargar los eventos sin elemento relacionado',
      emptyTitle: 'Ningún evento sin elemento relacionado',
      emptyDescription:
        'Todos los eventos de reproducción recibidos hasta ahora coincidieron con algún elemento catalogado.',
      range: '{from}–{to} de {total}',
      untitled: 'Elemento sin título',
      untitledWithIds: 'Elemento sin título ({ids})',
    },
    history: {
      badge: 'Visto el {date}',
      sectionTitle: 'Historial',
      loading: 'Cargando el historial…',
      loadError: 'No se pudo cargar el historial',
      deleteLabel: 'Eliminar evento',
      deleting: 'Eliminando…',
      confirmDelete: '¿Eliminar?',
      deleteFailed: 'No se pudo eliminar el evento.',
      status: {
        started: 'Empezó a ver',
        paused: 'Pausó',
        stopped: 'Detuvo',
        completed: 'Vio',
      },
      origin: {
        manual: 'Registrado manualmente',
      },
    },
    completeness: {
      loading: 'Cargando la completitud de catalogación…',
      loadError: 'No se pudo cargar la completitud de catalogación',
      information: 'Información',
      castAndFile: 'Reparto + archivo',
      artwork: 'Artes',
      field: {
        description: 'Sinopsis',
        runtime: 'Duración',
        certification: 'Clasificación por edad',
        keywords: 'Palabras clave',
        cast: 'Reparto',
        media_file: 'Archivo de medios',
      },
      artworkKind: {
        poster: 'Póster',
        fanart: 'Fanart',
        logo: 'Logo',
        clearart: 'Clearart',
        banner: 'Banner',
        thumb: 'Thumb',
        disc: 'Carátula',
      },
      incomplete: 'Catalogación incompleta: {missing}',
      artworkItem: 'arte {kind}',
    },
    filters: {
      searchLabel: 'Buscar en la cola por título',
      searchPlaceholder: 'Buscar por título, serie, álbum o carpeta…',
      kindLabel: 'Filtrar por tipo de grupo',
      kindAll: 'Todos los tipos',
      reasonLabel: 'Filtrar por motivo',
      reasonAll: 'Todos los motivos',
      reasonNotIdentified: 'No identificado',
      reasonLowConfidence: 'Baja confianza',
      clear: 'Limpiar filtros',
      noResultsTitle: 'Ningún elemento corresponde a los filtros',
      noResultsDescription:
        'Limpia la búsqueda o cambia los filtros para ver la cola de nuevo.',
    },
  },
})
