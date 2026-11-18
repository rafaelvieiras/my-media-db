// Collections, item creation, the search panel and the media type filter.
//
// The domain covers the external collection page, the two local creation screens
// (item and collection) with their shared scaffold, the member list/picker of a
// collection and the search with its type filter. Data keeps its own name: entity
// types, provider names (tmdb), ids and slugs are never translated.
import { defineDomain } from '../types'

export const collections = defineDomain({
  en: {
    detail: {
      loading: 'Loading collection…',
      notFound: 'Collection not found in the source',
      unavailable: 'Source unavailable right now',
      error: 'Could not load the collection',
      view: 'View collection',
      import: 'Import collection',
      movies: 'Collection movies',
      empty: 'This collection has no items.',
    },
    create: {
      title: 'New collection',
      failed: 'Could not create the collection.',
      submit: 'Create collection',
      submitting: 'Creating…',
    },
    item: {
      title: 'New item',
      failed: 'Could not create the item.',
      typeLabel: 'Item type',
      crossLinkBefore: 'Looking for a title that already exists?',
      crossLinkAction: 'Use the Search',
      crossLinkAfter: 'to import straight from the catalog.',
      submit: 'Create item',
      submitting: 'Creating…',
    },
    members: {
      title: 'Collection items',
      add: 'Add item',
      loading: 'Loading collection items…',
      loadError: 'Could not load the collection items',
      empty: 'This collection has no items yet.',
      removing: 'Removing…',
      confirmRemove: 'Remove?',
      removeLabel: 'Remove {title} from the collection',
    },
    picker: {
      addFailed: 'Could not add the item.',
      searchAriaLabel: 'Search for an item to add',
      searchPlaceholder: 'Search for an item in your catalog…',
      done: 'Done',
      hint: 'Type to search your catalog and add an item to the collection.',
      searching: 'Searching…',
      searchError: 'Could not search',
      empty: 'No item available to add.',
    },
    newPage: {
      back: 'Back to the library',
      loading: 'Loading the form…',
      loadError: 'Could not load the form',
    },
    search: {
      screenTitle: 'Search',
      screenDescription:
        'Look through your catalog and external sources; import what is missing.',
      ariaLabel: 'Search title',
      placeholder: 'Search by title, or paste a TMDb ID or URL…',
      initialTitle: 'Search your catalog and external sources',
      initialDescription:
        'Type the title of a movie or series to see what is already in your library and what you can import. If the title on disk is misspelled, paste the ID or the item URL instead (TMDB:39340, tvdb:248741, tt1845307 or themoviedb.org/tv/39340) — an ID is not affected by typos.',
      searching: 'Searching…',
      searchError: 'Could not search',
      catalog: 'Catalog',
      noCatalogMatch: 'No item in your catalog matches the search.',
      noSeasonMatch: 'No season matches the search.',
      noEpisodeMatch: 'No episode matches the search.',
      external: 'External results',
      externalUnavailable: 'External search unavailable',
      externalUnavailableMessage:
        'The external source did not respond or is not configured. This is not the same as having no results — try again later.',
      externalEmpty: 'No external result to import.',
    },
    filter: {
      label: 'Filter by type',
      all: 'All',
    },
    correct: {
      useThis: 'Use this',
    },
  },
  'pt-BR': {
    detail: {
      loading: 'Carregando coleção…',
      notFound: 'Coleção não encontrada na fonte',
      unavailable: 'Fonte indisponível no momento',
      error: 'Não foi possível carregar a coleção',
      view: 'Ver coleção',
      import: 'Importar coleção',
      movies: 'Filmes da coleção',
      empty: 'Esta coleção não tem itens.',
    },
    create: {
      title: 'Nova coleção',
      failed: 'Não foi possível criar a coleção.',
      submit: 'Criar coleção',
      submitting: 'Criando…',
    },
    item: {
      title: 'Novo item',
      failed: 'Não foi possível criar o item.',
      typeLabel: 'Tipo do item',
      crossLinkBefore: 'Procurando um título existente?',
      crossLinkAction: 'Use a Busca',
      crossLinkAfter: 'para importar direto do catálogo.',
      submit: 'Criar item',
      submitting: 'Criando…',
    },
    members: {
      title: 'Itens da coleção',
      add: 'Adicionar item',
      loading: 'Carregando itens da coleção…',
      loadError: 'Não foi possível carregar os itens da coleção',
      empty: 'Esta coleção ainda não tem itens.',
      removing: 'Removendo…',
      confirmRemove: 'Remover?',
      removeLabel: 'Remover {title} da coleção',
    },
    picker: {
      addFailed: 'Não foi possível adicionar o item.',
      searchAriaLabel: 'Buscar item para adicionar',
      searchPlaceholder: 'Buscar um item do seu catálogo…',
      done: 'Concluir',
      hint: 'Digite para buscar um item do seu catálogo e adicioná-lo à coleção.',
      searching: 'Buscando…',
      searchError: 'Não foi possível buscar',
      empty: 'Nenhum item disponível para adicionar.',
    },
    newPage: {
      back: 'Voltar à biblioteca',
      loading: 'Carregando formulário…',
      loadError: 'Não foi possível carregar o formulário',
    },
    search: {
      screenTitle: 'Buscar',
      screenDescription:
        'Procure no seu catálogo e em fontes externas; importe o que faltar.',
      ariaLabel: 'Buscar título',
      placeholder: 'Buscar por título, ou colar um ID ou URL do TMDb…',
      initialTitle: 'Busque no seu catálogo e em fontes externas',
      initialDescription:
        'Digite o título de um filme ou série para ver o que já está na sua biblioteca e o que pode importar. Se o título no disco estiver com erro de digitação, cole o ID ou a URL do item (TMDB:39340, tvdb:248741, tt1845307 ou themoviedb.org/tv/39340) — ID não erra por typo.',
      searching: 'Buscando…',
      searchError: 'Não foi possível buscar',
      catalog: 'Catálogo',
      noCatalogMatch: 'Nenhum item do seu catálogo corresponde à busca.',
      noSeasonMatch: 'Nenhuma temporada corresponde à busca.',
      noEpisodeMatch: 'Nenhum episódio corresponde à busca.',
      external: 'Resultados externos',
      externalUnavailable: 'Busca externa indisponível',
      externalUnavailableMessage:
        'A fonte externa não respondeu ou não está configurada. Isto é diferente de não haver resultados — tente novamente mais tarde.',
      externalEmpty: 'Nenhum resultado externo para importar.',
    },
    filter: {
      label: 'Filtrar por tipo',
      all: 'Todos',
    },
    correct: {
      useThis: 'Usar este',
    },
  },
  es: {
    detail: {
      loading: 'Cargando la colección…',
      notFound: 'Colección no encontrada en la fuente',
      unavailable: 'Fuente no disponible en este momento',
      error: 'No se pudo cargar la colección',
      view: 'Ver colección',
      import: 'Importar colección',
      movies: 'Películas de la colección',
      empty: 'Esta colección no tiene elementos.',
    },
    create: {
      title: 'Nueva colección',
      failed: 'No se pudo crear la colección.',
      submit: 'Crear colección',
      submitting: 'Creando…',
    },
    item: {
      title: 'Nuevo elemento',
      failed: 'No se pudo crear el elemento.',
      typeLabel: 'Tipo de elemento',
      crossLinkBefore: '¿Buscas un título que ya existe?',
      crossLinkAction: 'Usa la búsqueda',
      crossLinkAfter: 'para importar directamente del catálogo.',
      submit: 'Crear elemento',
      submitting: 'Creando…',
    },
    members: {
      title: 'Elementos de la colección',
      add: 'Añadir elemento',
      loading: 'Cargando los elementos de la colección…',
      loadError: 'No se pudieron cargar los elementos de la colección',
      empty: 'Esta colección todavía no tiene elementos.',
      removing: 'Quitando…',
      confirmRemove: '¿Quitar?',
      removeLabel: 'Quitar {title} de la colección',
    },
    picker: {
      addFailed: 'No se pudo añadir el elemento.',
      searchAriaLabel: 'Buscar un elemento para añadir',
      searchPlaceholder: 'Busca un elemento de tu catálogo…',
      done: 'Listo',
      hint: 'Escribe para buscar un elemento de tu catálogo y añadirlo a la colección.',
      searching: 'Buscando…',
      searchError: 'No se pudo buscar',
      empty: 'Ningún elemento disponible para añadir.',
    },
    newPage: {
      back: 'Volver a la biblioteca',
      loading: 'Cargando el formulario…',
      loadError: 'No se pudo cargar el formulario',
    },
    search: {
      screenTitle: 'Buscar',
      screenDescription:
        'Busca en tu catálogo y en fuentes externas; importa lo que falte.',
      ariaLabel: 'Buscar título',
      placeholder: 'Buscar por título, o pegar un ID o URL de TMDb…',
      initialTitle: 'Busca en tu catálogo y en fuentes externas',
      initialDescription:
        'Escribe el título de una película o una serie para ver qué ya está en tu biblioteca y qué puedes importar. Si el título en el disco tiene un error de escritura, pega el ID o la URL del ítem (TMDB:39340, tvdb:248741, tt1845307 o themoviedb.org/tv/39340) — un ID no falla por un error de escritura.',
      searching: 'Buscando…',
      searchError: 'No se pudo buscar',
      catalog: 'Catálogo',
      noCatalogMatch:
        'Ningún elemento de tu catálogo coincide con la búsqueda.',
      noSeasonMatch: 'Ninguna temporada coincide con la búsqueda.',
      noEpisodeMatch: 'Ningún episodio coincide con la búsqueda.',
      external: 'Resultados externos',
      externalUnavailable: 'Búsqueda externa no disponible',
      externalUnavailableMessage:
        'La fuente externa no respondió o no está configurada. Esto no es lo mismo que no tener resultados: inténtalo de nuevo más tarde.',
      externalEmpty: 'Ningún resultado externo para importar.',
    },
    filter: {
      label: 'Filtrar por tipo',
      all: 'Todos',
    },
    correct: {
      useThis: 'Usar este',
    },
  },
})
