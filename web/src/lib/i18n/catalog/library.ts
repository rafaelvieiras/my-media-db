// Library screens: the home listing, the datasource list, browsing one type at a
// time, and the create/edit forms of a datasource (with the language and retry
// fields they share, and the danger zone of the edit screen).
//
// "Datasource" is the name the interface gives to a registered library folder; it
// is a product term, not an API enum, so it is translated like any other label.
import { defineDomain } from '../types'

export const library = defineDomain({
  en: {
    index: {
      title: 'Library',
      description: 'Catalog detected from your datasources.',
      newItem: 'New item',
      newCollection: 'New collection',
    },
    list: {
      title: 'Libraries',
      description:
        'Point at media folders, scan them, and follow the progress.',
    },
    browse: {
      notFoundTitle: 'Library type not found',
      notFoundDescription:
        'The link you used does not match any known library type.',
    },
    form: {
      backToLibraries: 'Back to libraries',
      path: 'Path',
      pathPlaceholder: '/mnt/media/movies',
      pathAriaLabel: 'Datasource path',
      preferredLanguage: 'Preferred language',
      preferredLanguageAriaLabel: 'Datasource preferred language',
      useGlobalLanguage: 'Use the global language',
      retryDays: 'Enrichment retry (days)',
      retryDaysAriaLabel: 'Datasource enrichment retry (days)',
      retryDaysPlaceholder: 'Use the global default',
      retryDaysPlaceholderWithDefault: 'Use the global default ({n} days)',
    },
    create: {
      title: 'Add datasource',
      typeAriaLabel: 'Datasource type',
      submit: 'Add datasource',
      submitting: 'Adding…',
      failed: 'Could not add the datasource.',
    },
    edit: {
      title: 'Edit datasource',
      loading: 'Loading datasource…',
      loadFailed: 'Could not load the datasource',
      notFound: 'Datasource not found',
      saveFailed: 'Could not save the new path.',
      removeFailed: 'Could not remove the datasource.',
      danger: {
        title: 'Danger zone',
        description:
          'Removing the datasource does not delete the files on disk, but it stops watching this folder and hides the catalog detected from it.',
        confirmAriaLabel: 'Confirm datasource removal',
        question:
          'Are you sure? This removes the datasource and cannot be undone.',
        confirm: 'Confirm removal',
        removing: 'Removing…',
        remove: 'Remove datasource',
      },
    },
  },
  'pt-BR': {
    index: {
      title: 'Biblioteca',
      description: 'Catálogo detectado a partir dos seus datasources.',
      newItem: 'Novo item',
      newCollection: 'Nova coleção',
    },
    list: {
      title: 'Bibliotecas',
      description: 'Aponte pastas de mídia, escaneie e acompanhe o progresso.',
    },
    browse: {
      notFoundTitle: 'Tipo de biblioteca não encontrado',
      notFoundDescription:
        'O link usado não corresponde a nenhum tipo de biblioteca conhecido.',
    },
    form: {
      backToLibraries: 'Voltar às bibliotecas',
      path: 'Caminho',
      pathPlaceholder: '/mnt/midia/filmes',
      pathAriaLabel: 'Caminho do datasource',
      preferredLanguage: 'Idioma preferido',
      preferredLanguageAriaLabel: 'Idioma preferido do datasource',
      useGlobalLanguage: 'Usar idioma global',
      retryDays: 'Reentativa de enriquecimento (dias)',
      retryDaysAriaLabel: 'Reentativa de enriquecimento (dias) do datasource',
      retryDaysPlaceholder: 'Usar padrão global',
      retryDaysPlaceholderWithDefault: 'Usar padrão global ({n} dias)',
    },
    create: {
      title: 'Adicionar datasource',
      typeAriaLabel: 'Tipo do datasource',
      submit: 'Adicionar datasource',
      submitting: 'Adicionando…',
      failed: 'Não foi possível adicionar o datasource.',
    },
    edit: {
      title: 'Editar datasource',
      loading: 'Carregando datasource…',
      loadFailed: 'Não foi possível carregar o datasource',
      notFound: 'Datasource não encontrado',
      saveFailed: 'Não foi possível salvar o novo caminho.',
      removeFailed: 'Não foi possível remover o datasource.',
      danger: {
        title: 'Zona de perigo',
        description:
          'Remover o datasource não apaga os arquivos no disco, mas desliga o acompanhamento desta pasta e some com o catálogo detectado a partir dela.',
        confirmAriaLabel: 'Confirmar remoção do datasource',
        question:
          'Tem certeza? Isso remove o datasource e não pode ser desfeito.',
        confirm: 'Confirmar remoção',
        removing: 'Removendo…',
        remove: 'Remover datasource',
      },
    },
  },
  es: {
    index: {
      title: 'Biblioteca',
      description: 'Catálogo detectado a partir de tus fuentes de datos.',
      newItem: 'Nuevo elemento',
      newCollection: 'Nueva colección',
    },
    list: {
      title: 'Bibliotecas',
      description: 'Apunta carpetas de medios, escanea y sigue el progreso.',
    },
    browse: {
      notFoundTitle: 'Tipo de biblioteca no encontrado',
      notFoundDescription:
        'El enlace usado no corresponde a ningún tipo de biblioteca conocido.',
    },
    form: {
      backToLibraries: 'Volver a las bibliotecas',
      path: 'Ruta',
      pathPlaceholder: '/mnt/medios/peliculas',
      pathAriaLabel: 'Ruta de la fuente de datos',
      preferredLanguage: 'Idioma preferido',
      preferredLanguageAriaLabel: 'Idioma preferido de la fuente de datos',
      useGlobalLanguage: 'Usar el idioma global',
      retryDays: 'Reintento de enriquecimiento (días)',
      retryDaysAriaLabel:
        'Reintento de enriquecimiento (días) de la fuente de datos',
      retryDaysPlaceholder: 'Usar el valor global',
      retryDaysPlaceholderWithDefault: 'Usar el valor global ({n} días)',
    },
    create: {
      title: 'Añadir fuente de datos',
      typeAriaLabel: 'Tipo de la fuente de datos',
      submit: 'Añadir fuente de datos',
      submitting: 'Añadiendo…',
      failed: 'No se pudo añadir la fuente de datos.',
    },
    edit: {
      title: 'Editar fuente de datos',
      loading: 'Cargando fuente de datos…',
      loadFailed: 'No se pudo cargar la fuente de datos',
      notFound: 'Fuente de datos no encontrada',
      saveFailed: 'No se pudo guardar la nueva ruta.',
      removeFailed: 'No se pudo eliminar la fuente de datos.',
      danger: {
        title: 'Zona de peligro',
        description:
          'Eliminar la fuente de datos no borra los archivos del disco, pero deja de vigilar esta carpeta y oculta el catálogo detectado a partir de ella.',
        confirmAriaLabel: 'Confirmar la eliminación de la fuente de datos',
        question:
          '¿Seguro? Esto elimina la fuente de datos y no se puede deshacer.',
        confirm: 'Confirmar eliminación',
        removing: 'Eliminando…',
        remove: 'Eliminar fuente de datos',
      },
    },
  },
})
