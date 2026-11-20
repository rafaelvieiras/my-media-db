// The execution queue screen (ADR-0040, issue #62): what runs now, what waits in
// line and what finished recently — scans and jobs in the same list, because they
// share the same queue. Every label here is about a RUN, never about the item
// being processed: the item names come from the server (a path, a title).
import { defineDomain } from '../types'

export const queue = defineDomain({
  en: {
    screen: {
      title: 'Queue',
      subtitle:
        'Everything the server is running, in order. Scans and jobs share one line — only one runs at a time.',
      loading: 'Loading the queue…',
      loadError: 'Could not load the queue',
      emptyTitle: 'Nothing running',
      emptyDescription:
        'The queue is empty. A scheduled job or a scan will appear here as soon as it starts.',
    },
    sections: {
      running: 'Running now',
      queued: 'Waiting',
      queuedEmpty: 'Nothing waiting.',
      recent: 'Recently finished',
      recentEmpty: 'No run finished yet.',
    },
    item: {
      kindScan: 'Scan',
      kindJob: 'Job',
      position: '#{position} in line',
      waitingFor: 'waiting {duration}',
      running: 'running {duration}',
      took: 'took {duration}',
      startedAt: 'started {when}',
      finishedAt: 'finished {when}',
      step: 'Step {index}/{total}: {label}',
      progress: '{done} of {total}',
      okFailed: '{ok} ok · {failed} failed',
      currentItem: 'Now: {item}',
      unknownItem: 'nothing reported yet',
      showTrail: 'Show what it did',
      hideTrail: 'Hide what it did',
      trailLoading: 'Loading the trail…',
      trailError: 'Could not load the trail',
      trailEmpty: 'This run has not reported any item yet.',
    },
    event: {
      ok: 'Done',
      error: 'Failed',
      skip: 'Skipped',
    },
    trigger: {
      manual: 'Manual',
      schedule: 'Scheduled',
      watch: 'Watcher',
    },
    status: {
      queued: 'Waiting',
      running: 'Running',
      success: 'Finished',
      error: 'Failed',
    },
  },
  'pt-BR': {
    screen: {
      title: 'Fila',
      subtitle:
        'Tudo o que o servidor está executando, em ordem. Varreduras e jobs dividem a mesma fila — só um roda por vez.',
      loading: 'Carregando a fila…',
      loadError: 'Não foi possível carregar a fila',
      emptyTitle: 'Nada rodando',
      emptyDescription:
        'A fila está vazia. Um job agendado ou uma varredura aparece aqui assim que começar.',
    },
    sections: {
      running: 'Rodando agora',
      queued: 'Esperando',
      queuedEmpty: 'Nada esperando.',
      recent: 'Concluídos recentemente',
      recentEmpty: 'Nenhuma execução concluída ainda.',
    },
    item: {
      kindScan: 'Varredura',
      kindJob: 'Job',
      position: '#{position} na fila',
      waitingFor: 'esperando há {duration}',
      running: 'rodando há {duration}',
      took: 'levou {duration}',
      startedAt: 'começou {when}',
      finishedAt: 'terminou {when}',
      step: 'Etapa {index}/{total}: {label}',
      progress: '{done} de {total}',
      okFailed: '{ok} ok · {failed} com erro',
      currentItem: 'Agora: {item}',
      unknownItem: 'nada reportado ainda',
      showTrail: 'Ver o que fez',
      hideTrail: 'Ocultar o que fez',
      trailLoading: 'Carregando o rastro…',
      trailError: 'Não foi possível carregar o rastro',
      trailEmpty: 'Esta execução ainda não reportou nenhum item.',
    },
    event: {
      ok: 'Concluído',
      error: 'Falhou',
      skip: 'Ignorado',
    },
    trigger: {
      manual: 'Manual',
      schedule: 'Agendado',
      watch: 'Watcher',
    },
    status: {
      queued: 'Esperando',
      running: 'Rodando',
      success: 'Concluído',
      error: 'Falhou',
    },
  },
  es: {
    screen: {
      title: 'Cola',
      subtitle:
        'Todo lo que el servidor está ejecutando, en orden. Escaneos y tareas comparten una sola cola — solo uno se ejecuta a la vez.',
      loading: 'Cargando la cola…',
      loadError: 'No fue posible cargar la cola',
      emptyTitle: 'Nada en ejecución',
      emptyDescription:
        'La cola está vacía. Una tarea programada o un escaneo aparecerá aquí en cuanto empiece.',
    },
    sections: {
      running: 'En ejecución',
      queued: 'En espera',
      queuedEmpty: 'Nada en espera.',
      recent: 'Terminados recientemente',
      recentEmpty: 'Ninguna ejecución terminada todavía.',
    },
    item: {
      kindScan: 'Escaneo',
      kindJob: 'Tarea',
      position: '#{position} en la cola',
      waitingFor: 'esperando {duration}',
      running: 'ejecutando desde hace {duration}',
      took: 'tardó {duration}',
      startedAt: 'empezó {when}',
      finishedAt: 'terminó {when}',
      step: 'Paso {index}/{total}: {label}',
      progress: '{done} de {total}',
      okFailed: '{ok} ok · {failed} con error',
      currentItem: 'Ahora: {item}',
      unknownItem: 'nada reportado todavía',
      showTrail: 'Ver qué hizo',
      hideTrail: 'Ocultar qué hizo',
      trailLoading: 'Cargando el rastro…',
      trailError: 'No fue posible cargar el rastro',
      trailEmpty: 'Esta ejecución todavía no reportó ningún elemento.',
    },
    event: {
      ok: 'Hecho',
      error: 'Falló',
      skip: 'Omitido',
    },
    trigger: {
      manual: 'Manual',
      schedule: 'Programado',
      watch: 'Vigilante',
    },
    status: {
      queued: 'En espera',
      running: 'Ejecutando',
      success: 'Terminado',
      error: 'Falló',
    },
  },
})
