package server

import (
	"sync"
	"time"
)

// ttlTelemetria é o TTL do cache de GET /api/v1/health/system (briefing
// 0055): fresco o bastante para refletir uma mudança recente (ex. acabou de
// rodar um scan), sem recalcular a cada reload/foco de janela do TanStack
// Query. Constante de código, não preferência exposta na UI — mesmo espírito
// de ttlCacheExterno (internal/server/search.go).
const ttlTelemetria = 60 * time.Second

// telemetryCache é um cache em memória, protegido por mutex, do snapshot de
// telemetria JÁ SERIALIZADO (systemHealthResponse), com expiração por TTL.
// Mesmo desenho de cacheExterno, mas SINGLETON (uma única entrada): este
// endpoint não tem parâmetro variável, diferente da busca externa que
// cacheia por (query,tipo).
type telemetryCache struct {
	mu     sync.Mutex
	ttl    time.Duration
	item   *systemHealthResponse
	expira time.Time
	agora  func() time.Time
}

// novoTelemetryCache cria um cache vazio com o TTL dado.
func novoTelemetryCache(ttl time.Duration) *telemetryCache {
	return &telemetryCache{ttl: ttl, agora: time.Now}
}

// obter devolve o snapshot cacheado e true quando existe e ainda não
// expirou; caso contrário (systemHealthResponse{}, false).
func (c *telemetryCache) obter() (systemHealthResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.item == nil || c.agora().After(c.expira) {
		return systemHealthResponse{}, false
	}
	return *c.item, true
}

// guardar grava (ou sobrescreve) o snapshot com nova expiração.
func (c *telemetryCache) guardar(resp systemHealthResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item := resp
	c.item = &item
	c.expira = c.agora().Add(c.ttl)
}
