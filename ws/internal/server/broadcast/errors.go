package broadcast

import "errors"

// ErrUnknownBroadcastType is returned by NewBus when cfg.Type is not a recognized value.
var ErrUnknownBroadcastType = errors.New("unknown broadcast type")
