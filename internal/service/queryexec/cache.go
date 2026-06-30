package queryexec

import (
	"sync"

	"cubodw/internal/engine/query"
)

// resultCache é um cache em memória, com limite de entradas e despejo FIFO, de
// resultados de query indexados pela SQL+args. Mantém contadores de hit/miss.
type resultCache struct {
	mu     sync.Mutex
	max    int
	items  map[string]*query.Result
	order  []string
	hits   int64
	misses int64
}

func newResultCache(max int) *resultCache {
	if max <= 0 {
		return nil // cache desabilitado
	}
	return &resultCache{max: max, items: make(map[string]*query.Result, max)}
}

func (c *resultCache) get(key string) (*query.Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.items[key]; ok {
		c.hits++
		return v, true
	}
	c.misses++
	return nil, false
}

func (c *resultCache) put(key string, v *query.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		c.items[key] = v
		return
	}
	if len(c.order) >= c.max {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.items, oldest)
	}
	c.items[key] = v
	c.order = append(c.order, key)
}

// Stats devolve hits, misses e o tamanho atual.
func (c *resultCache) stats() (hits, misses, size int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.misses, int64(len(c.items))
}

func (c *resultCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*query.Result, c.max)
	c.order = nil
}

// cloneResult faz uma cópia rasa com a fatia externa de linhas nova, para que
// quem consome possa anexar linhas (ex.: total geral) sem corromper o cache.
// As células internas são imutáveis (somente leitura).
func cloneResult(r *query.Result) *query.Result {
	rows := make([][]query.Cell, len(r.Rows))
	copy(rows, r.Rows)
	cp := *r
	cp.Rows = rows
	return &cp
}
