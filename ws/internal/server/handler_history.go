package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	valkey "github.com/valkey-io/valkey-go"

	"github.com/klurvio/sukko/internal/server/history"
	"github.com/klurvio/sukko/internal/shared/license"
	"github.com/klurvio/sukko/internal/shared/logging"
)

// handleHistoryRequest delivers historical messages for a channel to the client.
// It runs validation (FR-008 checks a–e), marks the channel in-progress, then
// launches an async delivery goroutine via c.clientWg.
func (s *Server) handleHistoryRequest(c *Client, req HistoryRequest) {
	// (a) History feature must be enabled.
	if !s.config.HistoryEnabled || s.historyWriter == nil {
		sendHistoryError(c, req.Channel, history.ErrCodeHistoryDisabled, "message history is not enabled")
		return
	}

	// (b) Edition gate.
	edition := license.Community
	if s.editionManager != nil {
		edition = s.editionManager.CurrentEdition()
	}
	if !license.EditionHasFeature(edition, license.MessageHistory) {
		if s.historyWriter != nil {
			s.historyWriter.Metrics().EditionGateDenials.Inc()
		}
		s.logger.Warn().
			Str("edition", edition.String()).
			Str("channel", req.Channel).
			Str("event", history.EventHistoryEditionGateDenied).
			Msg("history request denied: edition gate")
		sendHistoryError(c, req.Channel, history.ErrCodeHistoryEditionGate, "message history requires a Pro or higher edition")
		return
	}

	// (c) Client must be subscribed to the channel.
	if !c.subscriptions.Has(req.Channel) {
		sendHistoryError(c, req.Channel, history.ErrCodeHistoryNotSubscribed, "subscribe to channel before requesting history")
		return
	}

	// (d) Limit must be within [1, HistoryMaxLimit] (operator-configured cap, default 100).
	limit := req.Limit
	if limit < 1 || limit > s.config.HistoryMaxLimit {
		sendHistoryError(c, req.Channel, history.ErrCodeHistoryInvalidLimit,
			fmt.Sprintf("limit must be between 1 and %d", s.config.HistoryMaxLimit))
		return
	}

	// (f) Channel must be in {tenant}.{suffix} format — validate before any state mutation.
	tenantID, bareChannel, ok := strings.Cut(req.Channel, ".")
	if !ok || bareChannel == "" {
		sendHistoryError(c, req.Channel, history.ErrCodeHistoryInvalidChannel, "channel must be in {tenant}.{suffix} format")
		return
	}

	// Acquire in-progress lock for this channel (prevents concurrent history deliveries).
	c.historyMu.Lock()
	if _, alreadyRunning := c.historyInProgress[req.Channel]; alreadyRunning {
		c.historyMu.Unlock()
		sendHistoryError(c, req.Channel, history.ErrCodeHistoryInProgress, "history delivery already in progress for this channel")
		return
	}
	c.historyInProgress[req.Channel] = struct{}{}
	c.historyMu.Unlock()

	valkeyClient := s.historyWriter.ValkeyClient()
	m := s.historyWriter.Metrics()
	streamKey := history.StreamKey(s.historyEnv, tenantID, bareChannel)

	c.clientWg.Go(func() {
		// pos1: panic recovery — runs last in LIFO, catches everything else.
		defer logging.RecoverPanic(s.logger, "historyDelivery", map[string]any{"channel": req.Channel})

		// pos2: clear in-progress flag so subsequent requests for this channel can proceed.
		defer clearInProgress(c, req.Channel)

		// declared 3rd → executes 2nd: cancel delivery context to release resources.
		deliveryCtx, deliveryCancel := context.WithTimeout(c.clientCtx, s.config.HistoryDeliveryTimeout)
		defer deliveryCancel()

		// declared 4th → executes 1st (before cancel): observe delivery duration.
		// deliverySource tracks the actual source label for the histogram — updated if future
		// paths (e.g. Kafka fallback) deliver via a different source.
		deliverySource := history.HistorySourceCache
		start := time.Now()
		defer func() { m.DeliveryDuration.WithLabelValues(deliverySource).Observe(time.Since(start).Seconds()) }()

		// --- Step 1: fetch attach_id (newest entry ID) ---
		attachID := fetchAttachID(deliveryCtx, valkeyClient, streamKey, m)

		// --- Step 2: fetch history entries ---
		var endID string
		if attachID != "" {
			endID = attachID
		} else {
			endID = "+"
		}

		entries, err := fetchHistoryEntries(deliveryCtx, valkeyClient, streamKey, endID, limit)
		if err != nil {
			s.logger.Warn().Err(err).Str("channel", req.Channel).Msg("history: XREVRANGE failed")
			sendHistoryError(c, req.Channel, history.ErrCodeHistoryStorageUnavailable, "history storage temporarily unavailable")
			return
		}

		// --- Step 3: reverse to chronological order (XREVRANGE returns newest-first) ---
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}

		// --- Step 4: deliver cache entries ---
		delivered := 0
		for _, entry := range entries {
			// Check for timeout/cancellation before consuming a sequence number.
			select {
			case <-deliveryCtx.Done():
				sendHistoryComplete(c, req.Channel, delivered, history.HistorySourceCache, attachID, true)
				return
			default:
			}
			payload := []byte(entry.FieldValues[history.HistoryFieldPayload])
			if len(payload) == 0 {
				m.SkipTotal.WithLabelValues(history.HistorySkipReasonEmptyPayload).Inc()
				continue
			}
			env := HistoryMessageEnvelope{
				Type:     MsgTypeMessage,
				Seq:      c.seqGen.Next(),
				Ts:       time.Now().UnixMilli(),
				Channel:  req.Channel,
				Data:     json.RawMessage(payload),
				History:  true,
				StreamID: entry.ID,
			}
			data, err := json.Marshal(env)
			if err != nil {
				continue
			}
			select {
			case c.send <- RawMsg(data):
				delivered++
			default:
				m.DeliveryDropped.WithLabelValues(history.HistoryDropReasonPumpFull).Inc()
			}
		}

		// --- Step 5: send history_complete ---
		sendHistoryComplete(c, req.Channel, delivered, history.HistorySourceCache, attachID, false)
	})
}

// fetchAttachID retrieves the most recent entry ID from the stream for client-side sync.
func fetchAttachID(ctx context.Context, client valkey.Client, streamKey string, m *history.Metrics) string {
	result := client.Do(ctx, client.B().Xrevrange().Key(streamKey).End("+").Start("-").Count(1).Build())
	entries, err := result.AsXRange()
	if err != nil || len(entries) == 0 {
		if err != nil {
			m.AttachIDFailures.Inc()
		}
		return ""
	}
	return entries[0].ID
}

// fetchHistoryEntries fetches up to limit entries from the stream starting at endID going backwards.
func fetchHistoryEntries(ctx context.Context, client valkey.Client, streamKey, endID string, limit int) ([]valkey.XRangeEntry, error) {
	result := client.Do(ctx, client.B().Xrevrange().Key(streamKey).End(endID).Start("-").Count(int64(limit)).Build())
	entries, err := result.AsXRange()
	if err != nil {
		// Nil reply (empty stream) is not an error.
		var ve *valkey.ValkeyError
		if errors.As(err, &ve) && ve.IsNil() {
			return nil, nil
		}
		return nil, fmt.Errorf("XREVRANGE %s: %w", streamKey, err)
	}
	return entries, nil
}

// sendHistoryComplete sends a non-blocking history_complete envelope to the client.
// truncated should be true when delivery was cut short by a context timeout.
func sendHistoryComplete(c *Client, channel string, count int, source, attachID string, truncated bool) {
	env := HistoryCompleteEnvelope{
		Type:      RespTypeHistoryComplete,
		Channel:   channel,
		Count:     count,
		Source:    source,
		AttachID:  attachID,
		Truncated: truncated,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	select {
	case c.send <- RawMsg(data):
	default:
	}
}

// sendHistoryError sends a non-blocking history_error envelope to the client.
func sendHistoryError(c *Client, channel, code, message string) {
	env := HistoryErrorEnvelope{
		Type:    RespTypeHistoryError,
		Code:    code,
		Channel: channel,
		Message: message,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	select {
	case c.send <- RawMsg(data):
	default:
	}
}

// clearInProgress removes the channel from the client's in-progress history map.
func clearInProgress(c *Client, channel string) {
	c.historyMu.Lock()
	delete(c.historyInProgress, channel)
	c.historyMu.Unlock()
}
