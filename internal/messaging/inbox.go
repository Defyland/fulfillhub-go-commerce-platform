package messaging

import (
	"context"
	"errors"
	"sync"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
)

type Inbox interface {
	Record(ctx context.Context, consumerName string, event commerce.OutboxEvent) (bool, error)
}

type ReleasableInbox interface {
	Release(ctx context.Context, consumerName string, event commerce.OutboxEvent) error
}

type InboxRecorder interface {
	RecordInboxMessage(ctx context.Context, consumerName string, event commerce.OutboxEvent) (bool, error)
}

type InboxReleaseRecorder interface {
	ReleaseInboxMessage(ctx context.Context, consumerName string, event commerce.OutboxEvent) error
}

type PersistentInbox struct {
	Recorder InboxRecorder
}

func (i PersistentInbox) Record(ctx context.Context, consumerName string, event commerce.OutboxEvent) (bool, error) {
	if i.Recorder == nil {
		return false, errors.New("persistent inbox recorder is required")
	}
	return i.Recorder.RecordInboxMessage(ctx, consumerName, event)
}

func (i PersistentInbox) Release(ctx context.Context, consumerName string, event commerce.OutboxEvent) error {
	recorder, ok := i.Recorder.(InboxReleaseRecorder)
	if !ok {
		return errors.New("persistent inbox recorder does not support release")
	}
	return recorder.ReleaseInboxMessage(ctx, consumerName, event)
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

func (i *MemoryInbox) Release(_ context.Context, consumerName string, event commerce.OutboxEvent) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	delete(i.processed, consumerName+":"+event.MessageID)
	return nil
}
