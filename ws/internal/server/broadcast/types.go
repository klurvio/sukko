package broadcast

import "sync/atomic"

// subscriberEntry holds a subscriber channel and its per-subscriber drop counter.
// Used by valkeyBus to track active subscribers.
type subscriberEntry struct {
	ch    chan *Message
	drops *atomic.Uint64
}
