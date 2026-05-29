package messaging

import (
	"context"
	"sync"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
)

type Inbox interface {
	Record(ctx context.Context, consumerName string, event commerce.OutboxEvent) (bool, error)
}

type MemoryInbox struct {
	mu        sync.Mutex
	processed map[string]struct{}
}

func NewMemoryInbox() *MemoryInbox {
	return &MemoryInbox{processed: make(map[string]struct{})}
}

func (i *MemoryInbox) Record(_ context.Context, consumerName string, event commerce.OutboxEvent) (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	key := consumerName + ":" + event.MessageID
	if _, ok := i.processed[key]; ok {
		return false, nil
	}
	i.processed[key] = struct{}{}
	return true, nil
}
