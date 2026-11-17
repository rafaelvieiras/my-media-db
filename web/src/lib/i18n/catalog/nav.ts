// Shell copy: the header navigation, the administrative sidebar, the breadcrumb
// of the series tree and the language control.
import { defineDomain } from '../types'

export const nav = defineDomain({
  en: {
    header: {
      library: 'Library',
      filterByType: 'Filter the library by type',
      search: 'Search',
      dashboard: 'Dashboard',
      openMenu: 'Open navigation menu',
      menuTitle: 'Navigation',
    },
    dashboard: {
      title: 'Administration',
      openMenu: 'Open the administration menu',
      libraries: 'Libraries',
      review: 'Review',
      orphans: 'Orphans',
      jobs: 'Jobs',
      settings: 'Settings',
      integrations: 'Integrations',
      webhook: 'Webhook',
      eventsWithoutItem: 'Events without item',
      health: 'Health',
    },
    breadcrumb: {
      label: 'Series trail',
    },
    language: {
      button: 'Language',
      interface: 'Interface',
      metadata: 'Metadata language',
      metadataHint: 'Language the titles and synopses are read in.',
    },
  },
  'pt-BR': {
    header: {
      library: 'Biblioteca',
      filterByType: 'Filtrar biblioteca por tipo',
      search: 'Buscar',
      dashboard: 'Dashboard',
      openMenu: 'Abrir menu de navegação',
      menuTitle: 'Navegação',
    },
    dashboard: {
      title: 'Administração',
      openMenu: 'Abrir menu de administração',
      libraries: 'Bibliotecas',
      review: 'Revisão',
      orphans: 'Órfãs',
      jobs: 'Jobs',
      settings: 'Configurações',
      integrations: 'Integrações',
      webhook: 'Webhook',
      eventsWithoutItem: 'Eventos sem item',
      health: 'Saúde',
    },
    breadcrumb: {
      label: 'Trilha da série',
    },
    language: {
      button: 'Idioma',
      interface: 'Interface',
      metadata: 'Idioma dos metadados',
      metadataHint: 'Idioma em que os títulos e sinopses são lidos.',
    },
  },
  es: {
    header: {
      library: 'Biblioteca',
      filterByType: 'Filtrar la biblioteca por tipo',
      search: 'Buscar',
      dashboard: 'Panel',
      openMenu: 'Abrir el menú de navegación',
      menuTitle: 'Navegación',
    },
    dashboard: {
      title: 'Administración',
      openMenu: 'Abrir el menú de administración',
      libraries: 'Bibliotecas',
      review: 'Revisión',
      orphans: 'Huérfanos',
      jobs: 'Tareas',
      settings: 'Ajustes',
      integrations: 'Integraciones',
      webhook: 'Webhook',
      eventsWithoutItem: 'Eventos sin elemento',
      health: 'Salud',
    },
    breadcrumb: {
      label: 'Ruta de la serie',
    },
    language: {
      button: 'Idioma',
      interface: 'Interfaz',
      metadata: 'Idioma de los metadatos',
      metadataHint: 'Idioma en el que se leen los títulos y las sinopsis.',
    },
  },
})
