package unified

import (
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
)

// NewMemoryStore creates a memory.Store backed by the unified database.
// The returned store uses the same underlying *sql.DB as the unified store,
// so all memory operations (facts, embeddings, user model, dreaming)
// operate on the unified DB.
func (s *Store) NewMemoryStore() (*memory.Store, error) {
	return memory.NewStoreFromDB(s.db)
}
