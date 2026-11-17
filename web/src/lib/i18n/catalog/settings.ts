// Settings, integrations, the integration form and the webhook screen.
import { defineDomain } from '../types'

export const settings = defineDomain({
  en: {
    screen: {
      title: 'Settings',
      subtitle: 'Catalog languages: primary, fallback and the allowed ones.',
      loading: 'Loading settings…',
      loadFailed: 'Could not load the settings',
    },
    language: {
      primary: 'Primary language',
      fallback: 'Fallback language',
      allowed: 'Allowed languages',
      emptyAllowed: 'Select at least one allowed language.',
      primaryNotAllowed:
        'The primary language must be one of the allowed ones.',
      fallbackNotAllowed:
        'The fallback language must be one of the allowed ones.',
    },
    retroactive: {
      title: 'Retroactive enrichment',
      subtitle:
        'Minimum interval between two attempts to re-enrich the SAME item, for libraries without an override of their own.',
      retryDays: 'Retry after (days)',
      retryDaysInvalid: 'Enter a whole number between 1 and 365 days.',
    },
    artwork: {
      title: 'Artwork',
      subtitle:
        'How posters/backgrounds are materialized into the real folder of the library.',
      toggle: 'Download artwork automatically after the scan',
      hint: 'When a new item is auto-imported during the scan, downloads and copies the artwork it finds into the real folder of the library right away, without waiting for a manual action or the periodic job.',
    },
    trailerTheme: {
      title: 'Trailer and theme song',
      subtitle:
        'How the automatic download of the trailer and the theme song via yt-dlp behaves (briefings 0102/0103).',
      toggle: 'Download trailer and theme automatically',
      hint: 'When the scan imports a new item, tries to download the trailer and the theme song automatically in the background (it does not block the scan). Off by default.',
    },
    completeness: {
      title: 'Cataloging completeness',
      subtitle:
        'Per media type, which fields and artwork are desirable to consider an item well cataloged.',
    },
    ytdlp: {
      subtitle:
        'Downloader used to download the trailer (briefing 0102) and the theme song (briefing 0103) from YouTube. No action is needed: MyMediaDB downloads the binary by itself, inside the container, the first time it needs it.',
      loading: 'Loading yt-dlp status…',
      loadFailed: 'Could not load the yt-dlp status',
      customPath: 'Custom binary path',
      customPathPlaceholder: '/usr/local/bin/yt-dlp (optional)',
      customPathAria: 'yt-dlp custom binary path',
      hint: 'Leave it blank to use the automatically downloaded binary (or the MYMEDIADB_YTDLP_PATH environment variable, when set). Fill it in only if you already manage your own yt-dlp installation.',
      termsWarning:
        'Warning: downloading YouTube videos with yt-dlp goes against the YouTube Terms of Service — a practice tolerated de facto across the Kodi/Jellyfin/Plex/tinyMediaManager ecosystem, but a risk you take on by using this feature.',
      badgeUnavailable: 'unavailable',
      badgeAvailableCustom: 'available (custom path)',
      badgeAvailableDownloaded: 'available (downloaded automatically)',
    },
    form: {
      saved: 'Settings saved.',
    },
    integrations: {
      title: 'Integrations',
      subtitle:
        'Provider credentials configurable from the panel, without editing .env or redeploying.',
      loading: 'Loading integrations…',
      loadFailed: 'Could not load the integrations',
      emptyTitle: 'No integration registered',
      emptyDescription:
        'No provider with configurable credentials is registered in this binary.',
      configured: 'Configured',
      notConfigured: 'Not configured',
    },
    integration: {
      backToList: 'Back to integrations',
      loading: 'Loading integration…',
      loadFailed: 'Could not load the integration',
      setInPanel: 'Set in the panel.',
      setByEnv:
        'Set by the {variable} environment variable — save here to override.',
      setByServerEnv:
        'Set by a server environment variable — save here to override.',
      selectPlaceholder: 'Select…',
      showField: 'Show {name}',
      hideField: 'Hide {name}',
      savedRestart: 'Saved. Restart the server (docker restart) to apply.',
    },
    webhook: {
      title: 'Webhook',
      subtitle:
        'Inbound webhook — Jellyfin: token used by the Jellyfin Webhooks plugin to report playback to the watch history.',
      loading: 'Loading token…',
      loadFailed: 'Could not load the webhook token',
      outboundTitle: 'Outbound webhooks',
      outboundSubtitle:
        'There are no configurable outbound events yet — today MyMediaDB only receives events (Jellyfin). Notifying external services (e.g. syncing with a tracker) is left for a future delivery.',
      outboundEmpty: 'No outbound webhook available yet.',
      nothingYet: 'No token generated yet.',
      generate: 'Generate token',
      generating: 'Generating…',
      regenerate: 'Generate new token',
      confirmRegenerate:
        'Generating a new token invalidates the current one — Jellyfin will stop authenticating until you reconfigure the URL. Continue?',
      tokenLabel: 'Token',
      tokenAria: 'Webhook token',
      showToken: 'Show token',
      hideToken: 'Hide token',
      copyToken: 'Copy token',
      copyUrl: 'Copy webhook URL',
      copied: 'Copied!',
      pasteHint:
        'Paste the URL above into the Jellyfin Webhooks plugin (Dashboard → Plugins → Webhooks → Add → "Generic" type), ticking the playback events (play/pause/stop).',
    },
    datasource: {
      add: 'Add datasource',
      loading: 'Loading datasources…',
      loadFailed: 'Could not load the datasources',
      emptyTitle: 'No datasource registered',
      emptyDescription: 'Add a path to start scanning your media.',
      stateOnline: 'online',
      stateOffline: 'offline',
      scan: 'Scan',
      scanning: 'Scanning…',
      reprocess: 'Reprocess',
      reprocessing: 'Reprocessing…',
      reprocessTitle:
        'Fixes the existing catalog: it retries the items under review and cleans up the empty drafts',
      watch: 'Watch this folder for changes',
      watchHint:
        '— speeds up the detection of new files, but does not replace the periodic scan',
      scanProgress: 'Scanning… {seen} files seen',
      scanProgressTotal: 'Scanning… {seen} of {total} files seen',
      lastScanFailed: 'Last scan failed: {detail}',
      reprocessFailed: 'Failed to reprocess',
      reprocessProgress: 'Reprocessing ({phase}): {done}/{total} items…',
      lastReprocess:
        'Last reprocess: {promoted} promoted, {orphans} empty drafts deleted.',
      preview: 'Preview — nothing has been written yet',
      previewSeries_one: '{n} series in review to retry',
      previewSeries_other: '{n} series in review to retry',
      previewMoviesPending_one: '{n} movie pending a match',
      previewMoviesPending_other: '{n} movies pending a match',
      previewDraftEpisodes_one: '{n} episode in review in this library',
      previewDraftEpisodes_other: '{n} episodes in review in this library',
      previewEmptyDrafts_one: '{n} empty draft to delete',
      previewEmptyDrafts_other: '{n} empty drafts to delete',
      previewMode:
        'Mode {mode}. No time budget and no throttle: it resolves everything that is pending right now.',
      applying: 'Applying…',
    },
  },
  'pt-BR': {
    screen: {
      title: 'Configurações',
      subtitle: 'Idiomas do catálogo: principal, fallback e os permitidos.',
      loading: 'Carregando configurações…',
      loadFailed: 'Não foi possível carregar as configurações',
    },
    language: {
      primary: 'Idioma principal',
      fallback: 'Idioma de fallback',
      allowed: 'Idiomas permitidos',
      emptyAllowed: 'Selecione ao menos um idioma permitido.',
      primaryNotAllowed:
        'O idioma principal precisa estar entre os permitidos.',
      fallbackNotAllowed:
        'O idioma de fallback precisa estar entre os permitidos.',
    },
    retroactive: {
      title: 'Enriquecimento retroativo',
      subtitle:
        'Intervalo mínimo entre duas tentativas de reenriquecer o MESMO item, para bibliotecas sem override próprio.',
      retryDays: 'Repetir tentativa após (dias)',
      retryDaysInvalid: 'Informe um número inteiro entre 1 e 365 dias.',
    },
    artwork: {
      title: 'Artes',
      subtitle:
        'Comportamento da materialização de pôsteres/fundos na pasta real da biblioteca.',
      toggle: 'Baixar artes automaticamente após o scan',
      hint: 'Quando um item novo é auto-importado durante o scan, baixa e copia as artes encontradas para a pasta real da biblioteca na hora, sem esperar ação manual ou o job periódico.',
    },
    trailerTheme: {
      title: 'Trailer e tema musical',
      subtitle:
        'Comportamento do download automático de trailer e música-tema via yt-dlp (briefings 0102/0103).',
      toggle: 'Baixar trailer e tema automaticamente',
      hint: 'Ao importar um item novo no scan, tenta baixar trailer e música-tema automaticamente em segundo plano (não bloqueia o scan). Desligado por padrão.',
    },
    completeness: {
      title: 'Completude de catalogação',
      subtitle:
        'Por tipo de mídia, quais campos e artes são desejáveis para considerar um item bem catalogado.',
    },
    ytdlp: {
      subtitle:
        'Downloader usado para baixar trailer (briefing 0102) e música-tema (briefing 0103) do YouTube. Nenhuma ação é necessária: o próprio MyMediaDB baixa o binário sozinho, dentro do container, na primeira vez que precisar dele.',
      loading: 'Carregando status do yt-dlp…',
      loadFailed: 'Não foi possível carregar o status do yt-dlp',
      customPath: 'Path customizado do binário',
      customPathPlaceholder: '/usr/local/bin/yt-dlp (opcional)',
      customPathAria: 'Path customizado do binário yt-dlp',
      hint: 'Deixe em branco para usar o binário baixado automaticamente (ou a variável de ambiente MYMEDIADB_YTDLP_PATH, quando configurada). Preencha só se você já gerencia sua própria instalação do yt-dlp.',
      termsWarning:
        'Atenção: baixar vídeos do YouTube via yt-dlp contraria os Termos de Serviço do YouTube — prática tolerada de facto no ecossistema Kodi/Jellyfin/Plex/tinyMediaManager, mas um risco que você assume ao usar este recurso.',
      badgeUnavailable: 'indisponível',
      badgeAvailableCustom: 'disponível (path customizado)',
      badgeAvailableDownloaded: 'disponível (baixado automaticamente)',
    },
    form: {
      saved: 'Configurações salvas.',
    },
    integrations: {
      title: 'Integrações',
      subtitle:
        'Credenciais de provider configuráveis pelo painel, sem editar .env nem redeployar.',
      loading: 'Carregando integrações…',
      loadFailed: 'Não foi possível carregar as integrações',
      emptyTitle: 'Nenhuma integração registrada',
      emptyDescription:
        'Nenhum provider com credenciais configuráveis está registrado neste binário.',
      configured: 'Configurada',
      notConfigured: 'Não configurada',
    },
    integration: {
      backToList: 'Voltar às integrações',
      loading: 'Carregando integração…',
      loadFailed: 'Não foi possível carregar a integração',
      setInPanel: 'Definido no painel.',
      setByEnv:
        'Definido pela variável de ambiente {variable} — salve aqui para sobrepor.',
      setByServerEnv:
        'Definido por uma variável de ambiente do servidor — salve aqui para sobrepor.',
      selectPlaceholder: 'Selecione…',
      showField: 'Mostrar {name}',
      hideField: 'Ocultar {name}',
      savedRestart: 'Salvo. Reinicie o servidor (docker restart) para aplicar.',
    },
    webhook: {
      title: 'Webhook',
      subtitle:
        'Webhook de entrada — Jellyfin: token usado pelo plugin de Webhooks do Jellyfin para reportar reprodução ao histórico de consumo.',
      loading: 'Carregando token…',
      loadFailed: 'Não foi possível carregar o token do webhook',
      outboundTitle: 'Webhooks de saída',
      outboundSubtitle:
        'Ainda não existem eventos de saída configuráveis — o MyMediaDB só recebe eventos (Jellyfin) hoje. Notificar serviços externos (ex.: sincronizar com um tracker) fica para uma entrega futura.',
      outboundEmpty: 'Nenhum webhook de saída disponível ainda.',
      nothingYet: 'Nenhum token gerado ainda.',
      generate: 'Gerar token',
      generating: 'Gerando…',
      regenerate: 'Gerar novo token',
      confirmRegenerate:
        'Gerar um novo token invalida o atual — o Jellyfin vai parar de autenticar até você reconfigurar a URL. Continuar?',
      tokenLabel: 'Token',
      tokenAria: 'Token do webhook',
      showToken: 'Mostrar token',
      hideToken: 'Ocultar token',
      copyToken: 'Copiar token',
      copyUrl: 'Copiar URL do webhook',
      copied: 'Copiado!',
      pasteHint:
        'Cole a URL acima no plugin de Webhooks do Jellyfin (Painel → Plugins → Webhooks → Adicionar → tipo "Genérico"), marcando os eventos de reprodução (play/pause/stop).',
    },
    datasource: {
      add: 'Adicionar datasource',
      loading: 'Carregando datasources…',
      loadFailed: 'Não foi possível carregar os datasources',
      emptyTitle: 'Nenhum datasource cadastrado',
      emptyDescription:
        'Adicione um caminho para começar a escanear sua mídia.',
      stateOnline: 'online',
      stateOffline: 'offline',
      scan: 'Escanear',
      scanning: 'Escaneando…',
      reprocess: 'Reprocessar',
      reprocessing: 'Reprocessando…',
      reprocessTitle:
        'Corrige o catálogo já existente: retenta os itens em revisão e limpa os rascunhos vazios',
      watch: 'Observar mudanças nesta pasta',
      watchHint:
        '— acelera a detecção de arquivos novos, mas não substitui o scan periódico',
      scanProgress: 'Escaneando… {seen} arquivos vistos',
      scanProgressTotal: 'Escaneando… {seen} de {total} arquivos vistos',
      lastScanFailed: 'Último scan falhou: {detail}',
      reprocessFailed: 'Falha ao reprocessar',
      reprocessProgress: 'Reprocessando ({phase}): {done}/{total} itens…',
      lastReprocess:
        'Último reprocessamento: {promoted} promovidos, {orphans} rascunhos vazios apagados.',
      preview: 'Prévia — nada foi escrito ainda',
      previewSeries_one: '{n} série em revisão a retentar',
      previewSeries_other: '{n} séries em revisão a retentar',
      previewMoviesPending_one: '{n} filme pendente de match',
      previewMoviesPending_other: '{n} filmes pendentes de match',
      previewDraftEpisodes_one: '{n} episódio em revisão nesta biblioteca',
      previewDraftEpisodes_other: '{n} episódios em revisão nesta biblioteca',
      previewEmptyDrafts_one: '{n} rascunho vazio a apagar',
      previewEmptyDrafts_other: '{n} rascunhos vazios a apagar',
      previewMode:
        'Modo {mode}. Sem orçamento de tempo e sem throttle: resolve tudo que está pendente agora.',
      applying: 'Aplicando…',
    },
  },
  es: {
    screen: {
      title: 'Ajustes',
      subtitle:
        'Idiomas del catálogo: principal, alternativo y los permitidos.',
      loading: 'Cargando ajustes…',
      loadFailed: 'No se pudieron cargar los ajustes',
    },
    language: {
      primary: 'Idioma principal',
      fallback: 'Idioma de respaldo',
      allowed: 'Idiomas permitidos',
      emptyAllowed: 'Selecciona al menos un idioma permitido.',
      primaryNotAllowed: 'El idioma principal debe estar entre los permitidos.',
      fallbackNotAllowed:
        'El idioma de respaldo debe estar entre los permitidos.',
    },
    retroactive: {
      title: 'Enriquecimiento retroactivo',
      subtitle:
        'Intervalo mínimo entre dos intentos de volver a enriquecer el MISMO elemento, para bibliotecas sin un override propio.',
      retryDays: 'Reintentar después de (días)',
      retryDaysInvalid: 'Indica un número entero entre 1 y 365 días.',
    },
    artwork: {
      title: 'Arte',
      subtitle:
        'Comportamiento de la materialización de pósteres/fondos en la carpeta real de la biblioteca.',
      toggle: 'Descargar el arte automáticamente después del escaneo',
      hint: 'Cuando un elemento nuevo se autoimporta durante el escaneo, descarga y copia el arte encontrado en la carpeta real de la biblioteca al momento, sin esperar una acción manual ni la tarea periódica.',
    },
    trailerTheme: {
      title: 'Tráiler y tema musical',
      subtitle:
        'Comportamiento de la descarga automática de tráiler y tema musical vía yt-dlp (briefings 0102/0103).',
      toggle: 'Descargar tráiler y tema automáticamente',
      hint: 'Al importar un elemento nuevo en el escaneo, intenta descargar el tráiler y el tema musical automáticamente en segundo plano (no bloquea el escaneo). Desactivado por defecto.',
    },
    completeness: {
      title: 'Completitud de catalogación',
      subtitle:
        'Por tipo de medio, qué campos y artes son deseables para considerar un elemento bien catalogado.',
    },
    ytdlp: {
      subtitle:
        'Descargador usado para bajar el tráiler (briefing 0102) y el tema musical (briefing 0103) de YouTube. No hace falta ninguna acción: el propio MyMediaDB descarga el binario por su cuenta, dentro del contenedor, la primera vez que lo necesita.',
      loading: 'Cargando el estado de yt-dlp…',
      loadFailed: 'No se pudo cargar el estado de yt-dlp',
      customPath: 'Ruta personalizada del binario',
      customPathPlaceholder: '/usr/local/bin/yt-dlp (opcional)',
      customPathAria: 'Ruta personalizada del binario yt-dlp',
      hint: 'Déjala en blanco para usar el binario descargado automáticamente (o la variable de entorno MYMEDIADB_YTDLP_PATH, cuando está configurada). Rellénala solo si ya gestionas tu propia instalación de yt-dlp.',
      termsWarning:
        'Atención: descargar vídeos de YouTube con yt-dlp va contra los Términos del Servicio de YouTube — una práctica tolerada de facto en el ecosistema Kodi/Jellyfin/Plex/tinyMediaManager, pero un riesgo que asumes al usar esta función.',
      badgeUnavailable: 'no disponible',
      badgeAvailableCustom: 'disponible (ruta personalizada)',
      badgeAvailableDownloaded: 'disponible (descargado automáticamente)',
    },
    form: {
      saved: 'Ajustes guardados.',
    },
    integrations: {
      title: 'Integraciones',
      subtitle:
        'Credenciales de proveedor configurables desde el panel, sin editar .env ni volver a desplegar.',
      loading: 'Cargando integraciones…',
      loadFailed: 'No se pudieron cargar las integraciones',
      emptyTitle: 'No hay ninguna integración registrada',
      emptyDescription:
        'Ningún proveedor con credenciales configurables está registrado en este binario.',
      configured: 'Configurada',
      notConfigured: 'Sin configurar',
    },
    integration: {
      backToList: 'Volver a las integraciones',
      loading: 'Cargando integración…',
      loadFailed: 'No se pudo cargar la integración',
      setInPanel: 'Definido en el panel.',
      setByEnv:
        'Definido por la variable de entorno {variable} — guarda aquí para reemplazar.',
      setByServerEnv:
        'Definido por una variable de entorno del servidor — guarda aquí para reemplazar.',
      selectPlaceholder: 'Selecciona…',
      showField: 'Mostrar {name}',
      hideField: 'Ocultar {name}',
      savedRestart:
        'Guardado. Reinicia el servidor (docker restart) para aplicar.',
    },
    webhook: {
      title: 'Webhook',
      subtitle:
        'Webhook de entrada — Jellyfin: token que usa el plugin de Webhooks de Jellyfin para reportar la reproducción al historial de consumo.',
      loading: 'Cargando token…',
      loadFailed: 'No se pudo cargar el token del webhook',
      outboundTitle: 'Webhooks de salida',
      outboundSubtitle:
        'Todavía no hay eventos de salida configurables — hoy MyMediaDB solo recibe eventos (Jellyfin). Notificar a servicios externos (p. ej. sincronizar con un tracker) queda para una entrega futura.',
      outboundEmpty: 'Todavía no hay ningún webhook de salida disponible.',
      nothingYet: 'Todavía no se ha generado ningún token.',
      generate: 'Generar token',
      generating: 'Generando…',
      regenerate: 'Generar token nuevo',
      confirmRegenerate:
        'Generar un token nuevo invalida el actual — Jellyfin dejará de autenticarse hasta que vuelvas a configurar la URL. ¿Continuar?',
      tokenLabel: 'Token',
      tokenAria: 'Token del webhook',
      showToken: 'Mostrar token',
      hideToken: 'Ocultar token',
      copyToken: 'Copiar token',
      copyUrl: 'Copiar URL del webhook',
      copied: '¡Copiado!',
      pasteHint:
        'Pega la URL de arriba en el plugin de Webhooks de Jellyfin (Panel → Plugins → Webhooks → Añadir → tipo "Genérico"), marcando los eventos de reproducción (play/pause/stop).',
    },
    datasource: {
      add: 'Añadir origen de datos',
      loading: 'Cargando orígenes de datos…',
      loadFailed: 'No se pudieron cargar los orígenes de datos',
      emptyTitle: 'No hay ningún origen de datos registrado',
      emptyDescription: 'Añade una ruta para empezar a escanear tu contenido.',
      stateOnline: 'online',
      stateOffline: 'offline',
      scan: 'Escanear',
      scanning: 'Escaneando…',
      reprocess: 'Reprocesar',
      reprocessing: 'Reprocesando…',
      reprocessTitle:
        'Corrige el catálogo existente: reintenta los elementos en revisión y limpia los borradores vacíos',
      watch: 'Vigilar cambios en esta carpeta',
      watchHint:
        '— acelera la detección de archivos nuevos, pero no sustituye el escaneo periódico',
      scanProgress: 'Escaneando… {seen} archivos vistos',
      scanProgressTotal: 'Escaneando… {seen} de {total} archivos vistos',
      lastScanFailed: 'El último escaneo falló: {detail}',
      reprocessFailed: 'Falló el reprocesamiento',
      reprocessProgress: 'Reprocesando ({phase}): {done}/{total} elementos…',
      lastReprocess:
        'Último reprocesamiento: {promoted} promovidos, {orphans} borradores vacíos eliminados.',
      preview: 'Vista previa — todavía no se ha escrito nada',
      previewSeries_one: '{n} serie en revisión para reintentar',
      previewSeries_other: '{n} series en revisión para reintentar',
      previewMoviesPending_one: '{n} película pendiente de coincidencia',
      previewMoviesPending_other: '{n} películas pendientes de coincidencia',
      previewDraftEpisodes_one: '{n} episodio en revisión en esta biblioteca',
      previewDraftEpisodes_other:
        '{n} episodios en revisión en esta biblioteca',
      previewEmptyDrafts_one: '{n} borrador vacío para eliminar',
      previewEmptyDrafts_other: '{n} borradores vacíos para eliminar',
      previewMode:
        'Modo {mode}. Sin presupuesto de tiempo y sin límite: resuelve todo lo que está pendiente ahora.',
      applying: 'Aplicando…',
    },
  },
})
