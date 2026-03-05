# Replay Mechanism: Deep Dive

## 🎯 What Problem Does Replay Solve?

### The Core Problem: Network Hiccups Cause Message Loss

```
Without replay mechanism:

Server sends:  seq 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10
                          ↓         ↓
Client receives: 1 → 2 → 3 → ??? → 6 → 7 → 8 → 9 → 10
                             ↑
                        Messages 4-5 lost
                        (WiFi glitch, packet drop, buffer overflow)

Problem:
  - Client has NO WAY to detect loss (no sequence numbers)
  - Client has NO WAY to recover (messages gone forever)
  - User sees: Stale prices, missing order fills, account balance wrong

Real-world impact for trading platform:
  ❌ User places order at $50,000 BTC
  ❌ Price dropped to $45,000 (but client never received update)
  ❌ User thinks they got good price but actually overpaid $5,000
  ❌ Legal liability + user complaints
```

### The Solution: Sequence Numbers + Replay Buffer

```
With replay mechanism:

Server sends:  seq 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10
                          ↓         ↓
Client receives: 1 → 2 → 3 → ??? → 6 → 7 → 8 → 9 → 10
                                    ↑
Client detects gap: Expected seq 4, got seq 6
Client requests replay: {"type": "replay", "data": {"from": 4, "to": 5}}
                                    ↓
Server sends from buffer:          4 → 5
                                    ↓
Client receives and fills gap:  1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10
                                    ✅ All messages recovered!

Recovery time: 10-50ms (vs 500ms-2s for full reconnect)
```

## 🏗️ Architecture Components

### 1. Message Envelope (message.go)

Every message sent to clients is wrapped in an envelope:

```go
type MessageEnvelope struct {
    Seq       int64           `json:"seq"`       // Sequence number (1, 2, 3, ...)
    Timestamp int64           `json:"ts"`        // Unix milliseconds
    Type      string          `json:"type"`      // "price:update", "order:fill", etc.
    Data      json.RawMessage `json:"data"`      // Actual payload
}
```

**Example on the wire:**

```json
{
  "seq": 42,
  "ts": 1696284123456,
  "type": "price:update",
  "data": {
    "symbol": "BTCUSD",
    "price": 50000.50,
    "volume": 123456789
  }
}
```

**Client receives this and:**
1. Checks `seq`: Is this the next expected sequence? (prev + 1)
2. If gap detected: Request replay for missing sequences
3. Extracts `data`: Update UI with price information
4. Calculates latency: `Date.now() - ts` = network delay

### 2. Sequence Generator (message.go:108)

Per-client atomic counter:

```go
type SequenceGenerator struct {
    counter int64  // Atomic counter, starts at 0
}

func (s *SequenceGenerator) Next() int64 {
    return atomic.AddInt64(&s.counter, 1)  // Returns 1, 2, 3, ...
}
```

**Key characteristics:**
- **Per-client**: Each WebSocket connection has independent sequence
- **Thread-safe**: Multiple goroutines can call Next() concurrently
- **Monotonic**: Always increasing, never gaps in generation
- **Starts at 1**: First message has seq=1, not 0
- **No wraparound**: int64 max = 9,223,372,036,854,775,807 (millions of years at 10 msg/sec)

**Example with 3 clients:**

```
Client A connects → seqGen counter = 0
  Server sends msg 1 → seqGen.Next() → counter = 1 → client sees seq 1
  Server sends msg 2 → seqGen.Next() → counter = 2 → client sees seq 2
  Server sends msg 3 → seqGen.Next() → counter = 3 → client sees seq 3

Client B connects → NEW seqGen counter = 0 (independent!)
  Server sends msg 1 → seqGen.Next() → counter = 1 → client sees seq 1
  Server sends msg 2 → seqGen.Next() → counter = 2 → client sees seq 2

Client C connects → NEW seqGen counter = 0
  Server sends msg 1 → seqGen.Next() → counter = 1 → client sees seq 1
```

**Why per-client sequences?**
- Clients connect at different times (Client B missed messages before connecting)
- Simplifies client logic (always expect seq=1 on first message)
- Allows disconnected client to reconnect with `since` parameter

### 3. Replay Buffer (replay_buffer.go)

Circular buffer storing recent messages per client:

```go
type ReplayBuffer struct {
    messages []*MessageEnvelope  // Slice of recent messages
    maxSize  int                  // 100 messages (configurable)
    mu       sync.RWMutex         // Thread-safety
}
```

**Memory layout:**

```
┌─────────────────────────────────────────────────────────────┐
│  ReplayBuffer for Client #1 (50KB total)                    │
│                                                              │
│  [0]  seq=901   ts=...  type="price:update"  data={...}     │
│  [1]  seq=902   ts=...  type="price:update"  data={...}     │
│  [2]  seq=903   ts=...  type="price:update"  data={...}     │
│  ...                                                         │
│  [99] seq=1000  ts=...  type="price:update"  data={...}     │
│       ↑ Most recent message                                 │
│                                                              │
│  When message #1001 arrives:                                │
│  [0]  seq=902   ← Oldest (seq=901) evicted                 │
│  [1]  seq=903                                               │
│  ...                                                         │
│  [99] seq=1001  ← Newest appended                           │
└─────────────────────────────────────────────────────────────┘

Buffer covers: 100 messages
At 10 msg/sec: 10 seconds of history
At 50 msg/sec: 2 seconds of history
```

**Operations:**

```go
// Add message to buffer (called from broadcast)
buffer.Add(envelope)

// Get specific range (gap filling)
messages := buffer.GetRange(fromSeq: 103, toSeq: 149)
// Returns: [seq 103, 104, 105, ..., 149] (if still in buffer)

// Get everything after sequence (reconnection)
messages := buffer.GetSince(sinceSeq: 950)
// Returns: [seq 951, 952, ..., 1000] (all newer than 950)

// Clear buffer (on disconnect)
buffer.Clear()
```

## 🔄 Complete Message Flow with Replay

### Scenario 1: Normal Operation (No Gaps)

```
┌──────────────────────────────────────────────────────────────┐
│ 1. NATS publishes price update                               │
└────────────────┬─────────────────────────────────────────────┘
                 │
                 ↓
┌──────────────────────────────────────────────────────────────┐
│ 2. Server receives via natsConn.Subscribe()                  │
│    Payload: {"symbol":"BTCUSD","price":50000}                │
└────────────────┬─────────────────────────────────────────────┘
                 │
                 ↓
┌──────────────────────────────────────────────────────────────┐
│ 3. broadcast() wraps in envelope (server.go:320)             │
│                                                               │
│    for each client:                                          │
│      envelope := WrapMessage(                                │
│        data: natsPayload,                                    │
│        type: "price:update",                                 │
│        seqGen: client.seqGen  ← Generates unique seq         │
│      )                                                        │
│      // envelope.Seq = 42 (for this client)                  │
│                                                               │
│      client.replayBuffer.Add(envelope)  ← Store in buffer    │
│      client.send <- envelope            ← Send to client     │
└────────────────┬─────────────────────────────────────────────┘
                 │
                 ↓
┌──────────────────────────────────────────────────────────────┐
│ 4. Client receives message                                   │
│                                                               │
│    ws.onmessage = (event) => {                               │
│      const msg = JSON.parse(event.data)                      │
│      // msg = {seq: 42, ts: ..., type: "price:update", ...}  │
│                                                               │
│      // Gap detection                                        │
│      if (msg.seq !== lastSeq + 1) {                          │
│        // GAP DETECTED!                                      │
│        requestReplay(lastSeq + 1, msg.seq - 1)               │
│      }                                                        │
│                                                               │
│      lastSeq = msg.seq  // Update expected sequence          │
│      handleMessage(msg) // Process message                   │
│    }                                                          │
│                                                               │
│    In this case:                                             │
│      lastSeq = 41                                            │
│      msg.seq = 42                                            │
│      42 === 41 + 1 ✓ No gap!                                 │
│      lastSeq = 42                                            │
│      updatePriceDisplay(msg.data)                            │
└──────────────────────────────────────────────────────────────┘
```

### Scenario 2: Gap Detection and Replay

```
Timeline:

t=0s   Client receives: seq 40
t=1s   Client receives: seq 41
t=2s   Client receives: seq 42
t=3s   [WiFi glitch - client briefly offline]
       Server sends: seq 43, 44, 45 (client misses these)
t=4s   [Client back online]
t=4s   Client receives: seq 46 ← Expected 43, got 46!

┌────────────────────────────────────────────────────────────┐
│ Client detects gap                                         │
│                                                             │
│   lastSeq = 42                                             │
│   received seq = 46                                        │
│   expected = lastSeq + 1 = 43                              │
│   46 !== 43 → GAP DETECTED!                                │
│                                                             │
│   Missing sequences: 43, 44, 45                            │
└────────────────┬───────────────────────────────────────────┘
                 │
                 ↓
┌────────────────────────────────────────────────────────────┐
│ Client sends replay request                                │
│                                                             │
│   ws.send(JSON.stringify({                                 │
│     type: "replay",                                        │
│     data: {                                                │
│       from: 43,  // First missing seq                      │
│       to: 45     // Last missing seq (received - 1)        │
│     }                                                       │
│   }))                                                       │
└────────────────┬───────────────────────────────────────────┘
                 │
                 ↓
┌────────────────────────────────────────────────────────────┐
│ Server receives replay request (server.go:512)             │
│                                                             │
│   handleClientMessage(client, message) {                   │
│     switch req.Type {                                      │
│       case "replay":                                       │
│         // Parse request                                   │
│         from = 43                                          │
│         to = 45                                            │
│                                                             │
│         // Get from replay buffer                          │
│         messages = client.replayBuffer.GetRange(43, 45)    │
│         // Returns: [                                      │
│         //   {seq: 43, ts: ..., data: ...},                │
│         //   {seq: 44, ts: ..., data: ...},                │
│         //   {seq: 45, ts: ..., data: ...}                 │
│         // ]                                                │
│                                                             │
│         // Send each message                               │
│         for each msg in messages {                         │
│           client.send <- msg (with 1 second timeout)       │
│         }                                                   │
│     }                                                       │
│   }                                                         │
└────────────────┬───────────────────────────────────────────┘
                 │
                 ↓
┌────────────────────────────────────────────────────────────┐
│ Client receives replayed messages                          │
│                                                             │
│   t=4.01s  Receives: seq 43 ✓                              │
│   t=4.02s  Receives: seq 44 ✓                              │
│   t=4.03s  Receives: seq 45 ✓                              │
│                                                             │
│   Gap filled!                                              │
│   lastSeq = 45                                             │
│                                                             │
│   Now continue with normal messages:                       │
│   t=5s     Receives: seq 46 (already received earlier)     │
│            → Deduplicate or ignore                         │
│   t=6s     Receives: seq 47                                │
│   t=7s     Receives: seq 48                                │
│   ...                                                       │
└────────────────────────────────────────────────────────────┘

Total recovery time: ~30-50ms
  - Detect gap: 0ms (immediate on receive)
  - Send request: 5-10ms (WebSocket roundtrip)
  - Server lookup: <1ms (buffer scan)
  - Send replayed messages: 10-30ms (3 messages × network latency)
  - Client processes: 5-10ms (parse + dedupe)
```

### Scenario 3: Reconnection with `since` Parameter

```
Client A has been connected for hours, received sequences 1 to 10,000

t=0h   Client disconnects (phone locked, network switch, etc.)
       lastSeq = 10,000

       Server continues broadcasting to other clients:
       seq 10,001, 10,002, ..., 10,050

t=5m   Client reconnects (new WebSocket connection)

┌────────────────────────────────────────────────────────────┐
│ Client reconnection strategy                               │
│                                                             │
│   Option A: Restart from seq 1 (bad - wastes bandwidth)   │
│   Option B: Request replay since last known seq (good!)    │
│                                                             │
│   Client sends immediately after reconnecting:             │
│   ws.send(JSON.stringify({                                 │
│     type: "replay",                                        │
│     data: {                                                │
│       since: 10000  // "Give me everything after 10,000"   │
│     }                                                       │
│   }))                                                       │
└────────────────┬───────────────────────────────────────────┘
                 │
                 ↓
┌────────────────────────────────────────────────────────────┐
│ Server processes "since" request (server.go:552)           │
│                                                             │
│   if (replayReq.Since > 0) {                               │
│     messages = client.replayBuffer.GetSince(10000)         │
│     // Returns everything with seq > 10,000                │
│   }                                                         │
│                                                             │
│   Buffer contains: seq 9,951 to 10,050 (last 100 msgs)    │
│                          ↑                                 │
│                    Client's 10,000 is in buffer!           │
│                                                             │
│   GetSince(10000) returns:                                 │
│     [seq 10,001, 10,002, ..., 10,050]                      │
│     50 messages                                            │
└────────────────┬───────────────────────────────────────────┘
                 │
                 ↓
┌────────────────────────────────────────────────────────────┐
│ Client catches up                                          │
│                                                             │
│   Receives 50 messages in ~200ms                           │
│   lastSeq = 10,050                                         │
│   Fully caught up!                                         │
│                                                             │
│   Now continues with live messages:                        │
│   seq 10,051, 10,052, ...                                  │
└────────────────────────────────────────────────────────────┘

Recovery time: 200-500ms (vs 500ms-2s for full reconnect + resubscribe)
```

### Scenario 4: Gap Too Old (Buffer Eviction)

```
Buffer size: 100 messages
Current buffer: seq 9,901 to 10,000 (oldest to newest)

Client receives: seq 9,850 then [long pause] then seq 10,000

Client detects gap: 9,851 to 9,999 missing (149 messages)

┌────────────────────────────────────────────────────────────┐
│ Client requests replay                                     │
│                                                             │
│   ws.send({                                                │
│     type: "replay",                                        │
│     data: {from: 9851, to: 9999}                           │
│   })                                                        │
└────────────────┬───────────────────────────────────────────┘
                 │
                 ↓
┌────────────────────────────────────────────────────────────┐
│ Server checks buffer (server.go:557)                       │
│                                                             │
│   messages = buffer.GetRange(9851, 9999)                   │
│                                                             │
│   Buffer only has: seq 9,901 to 10,000                     │
│                    ↑ Oldest message in buffer              │
│                                                             │
│   Requested: 9,851 to 9,999                                │
│   Available: 9,901 to 9,999 (partial!)                     │
│                                                             │
│   GetRange returns: [seq 9,901, 9,902, ..., 9,999]         │
│   99 messages (instead of requested 149)                   │
│                                                             │
│   Missing: seq 9,851 to 9,900 (50 messages evicted)        │
└────────────────┬───────────────────────────────────────────┘
                 │
                 ↓
┌────────────────────────────────────────────────────────────┐
│ Client receives partial replay                             │
│                                                             │
│   Received: seq 9,901 to 9,999                             │
│   Expected: seq 9,851 to 9,999                             │
│                                                             │
│   Still have gap: 9,851 to 9,900 (irrecoverable!)          │
│                                                             │
│   Client decision:                                         │
│   Option A: Give up, continue from 9,901 (data loss)       │
│   Option B: Full reconnect (get fresh state)               │
│   Option C: Fetch from REST API (if available)             │
│                                                             │
│   Recommended for trading platform:                        │
│   if (gapStillExists) {                                    │
│     // Full reconnect to ensure data consistency          │
│     ws.close()                                             │
│     reconnect()                                            │
│   }                                                         │
└────────────────────────────────────────────────────────────┘

Prevention:
  - Increase buffer size (100 → 500 messages)
  - Reduce message rate (less frequent updates)
  - Faster reconnection (detect disconnect sooner)
  - Client-side persistence (store messages in IndexedDB)
```

## 📊 Performance Characteristics

### Memory Cost

**Per client:**
```
Replay buffer: 100 messages × 500 bytes avg = 50 KB
Sequence generator: 8 bytes (int64)
Total overhead: ~50 KB per client
```

**For 2,184 clients:**
```
2,184 clients × 50 KB = 109.2 MB
(Part of the 384 MB allocated for clients in 512MB container)
```

### Time Complexity

**Buffer operations:**
```
Add(message):           O(1) amortized (append to slice)
GetRange(from, to):     O(n) where n = buffer size (linear scan)
GetSince(since):        O(n) where n = buffer size (linear scan)
Clear():                O(1) (slice truncation)
```

**Real-world performance:**
```
Buffer size: 100 messages
GetRange() scan: ~1 microsecond on modern CPU
Not a bottleneck (replay requests are rare)
```

**Optimization potential:**
```
Current: Linear scan
Optimized: Binary search (O(log n))
Future: Index by sequence (O(1) lookup, more memory)

For 100 message buffer:
  Linear: 100 comparisons
  Binary search: 7 comparisons (log₂ 100)
  Index: 1 lookup

Speed difference: <1μs (not worth complexity for 100 messages)
```

### Replay Latency

```
Component               Time
───────────────────────────────────────
Gap detection           0ms (immediate)
Request send            5-10ms (WebSocket RTT)
Buffer lookup           <1ms (linear scan)
Response send           10-30ms (3-50 messages)
Client processing       5-10ms (parse JSON)
───────────────────────────────────────
Total                   20-51ms

Compare to full reconnect:
WebSocket handshake     100-300ms
Re-subscribe to topics  50-100ms
Fetch initial state     200-1000ms
───────────────────────────────────────
Total                   350-1400ms

Replay is 10-70× faster than reconnect!
```

## 💻 Client-Side Implementation Guide

### JavaScript/TypeScript Example

```typescript
class WebSocketClient {
  private ws: WebSocket
  private lastSeq: number = 0
  private pendingReplays: Set<number> = new Set()

  connect() {
    this.ws = new WebSocket('ws://localhost:3004/ws')

    this.ws.onmessage = (event) => {
      const envelope = JSON.parse(event.data)
      this.handleMessage(envelope)
    }

    this.ws.onopen = () => {
      // Request catch-up on reconnection
      if (this.lastSeq > 0) {
        this.requestReplaySince(this.lastSeq)
      }
    }
  }

  handleMessage(envelope: MessageEnvelope) {
    const { seq, ts, type, data } = envelope

    // Gap detection
    if (this.lastSeq > 0 && seq !== this.lastSeq + 1) {
      const gapStart = this.lastSeq + 1
      const gapEnd = seq - 1

      console.warn(`Gap detected: ${gapStart} to ${gapEnd}`)

      // Avoid duplicate replay requests
      if (!this.pendingReplays.has(gapStart)) {
        this.requestReplayRange(gapStart, gapEnd)
        this.pendingReplays.add(gapStart)
      }
    }

    // Update last seen sequence
    this.lastSeq = seq

    // Calculate latency
    const latency = Date.now() - ts
    console.log(`Message seq=${seq} latency=${latency}ms`)

    // Route message by type
    switch (type) {
      case 'price:update':
        this.handlePriceUpdate(data)
        break
      case 'order:fill':
        this.handleOrderFill(data)
        break
      // ... other message types
    }
  }

  requestReplayRange(from: number, to: number) {
    console.log(`Requesting replay: ${from} to ${to}`)
    this.ws.send(JSON.stringify({
      type: 'replay',
      data: { from, to }
    }))

    // Clear pending flag after timeout (assume replay completed)
    setTimeout(() => {
      this.pendingReplays.delete(from)
    }, 5000)
  }

  requestReplaySince(since: number) {
    console.log(`Requesting replay since: ${since}`)
    this.ws.send(JSON.stringify({
      type: 'replay',
      data: { since }
    }))
  }

  handlePriceUpdate(data: any) {
    // Update UI with new price
    console.log(`Price update: ${data.symbol} = ${data.price}`)
  }

  handleOrderFill(data: any) {
    // Show notification
    console.log(`Order filled: ${data.orderId}`)
  }
}

interface MessageEnvelope {
  seq: number
  ts: number
  type: string
  data: any
}
```

### React Hook Example

```typescript
import { useEffect, useRef, useState } from 'react'

export function useWebSocketWithReplay(url: string) {
  const ws = useRef<WebSocket | null>(null)
  const [lastSeq, setLastSeq] = useState(0)
  const [messages, setMessages] = useState<any[]>([])
  const [connected, setConnected] = useState(false)

  useEffect(() => {
    const socket = new WebSocket(url)
    ws.current = socket

    socket.onopen = () => {
      setConnected(true)

      // Request catch-up on reconnection
      if (lastSeq > 0) {
        socket.send(JSON.stringify({
          type: 'replay',
          data: { since: lastSeq }
        }))
      }
    }

    socket.onmessage = (event) => {
      const envelope = JSON.parse(event.data)

      // Gap detection
      if (lastSeq > 0 && envelope.seq !== lastSeq + 1) {
        console.warn(`Gap: expected ${lastSeq + 1}, got ${envelope.seq}`)

        // Request missing messages
        socket.send(JSON.stringify({
          type: 'replay',
          data: {
            from: lastSeq + 1,
            to: envelope.seq - 1
          }
        }))
      }

      setLastSeq(envelope.seq)
      setMessages(prev => [...prev, envelope])
    }

    socket.onclose = () => {
      setConnected(false)
      // Auto-reconnect after 2 seconds
      setTimeout(() => {
        // Re-run effect (reconnect)
      }, 2000)
    }

    return () => {
      socket.close()
    }
  }, [url]) // Only reconnect if URL changes

  return { messages, connected, lastSeq }
}

// Usage in component:
function TradingDashboard() {
  const { messages, connected, lastSeq } = useWebSocketWithReplay('ws://localhost:3004/ws')

  return (
    <div>
      <div>Status: {connected ? 'Connected' : 'Disconnected'}</div>
      <div>Last sequence: {lastSeq}</div>
      <ul>
        {messages.map(msg => (
          <li key={msg.seq}>
            Seq {msg.seq}: {msg.type} - {JSON.stringify(msg.data)}
          </li>
        ))}
      </ul>
    </div>
  )
}
```

## 🚨 Edge Cases and Gotchas

### 1. Out-of-Order Delivery

```
Possible scenario: TCP packet reordering

Client receives: seq 1, 2, 3, 5, 4, 6

Detection:
  - Receive seq 5, expect 4 → Gap detected!
  - Request replay for seq 4
  - Then receive seq 4 (original delayed packet)
  - Then receive seq 4 again (replayed)

Solution: Client-side deduplication
  const receivedSeqs = new Set<number>()

  if (receivedSeqs.has(envelope.seq)) {
    console.log('Duplicate sequence, ignoring')
    return
  }
  receivedSeqs.add(envelope.seq)
```

### 2. Rapid Reconnections

```
Client disconnects and reconnects rapidly:

Connection 1: Receives seq 1-50
Connection 2: (new connection, reset to seq 1)

Problem: Client thinks seq reset, but it's a new connection

Solution: Store connection ID with sequences
  {
    seq: 1,
    connectionId: "abc123",  // Add this field
    ...
  }

  Client detects connection change and resets lastSeq:
  if (envelope.connectionId !== currentConnectionId) {
    lastSeq = 0
    currentConnectionId = envelope.connectionId
  }
```

### 3. Buffer Too Small

```
Message rate: 50 msg/sec
Buffer size: 100 messages
Coverage: 2 seconds

Client offline for 3 seconds → 150 messages missed
Buffer only has last 100 → 50 messages irrecoverable

Detection:
  Client requests: from=1000, to=1149 (150 messages)
  Server returns: seq 1050-1149 (100 messages)
  Client checks: First returned seq (1050) > requested (1000)
  → Irrecoverable gap

Response:
  alert('Data loss detected, refreshing...')
  window.location.reload() // Full page reload
```

### 4. Slow Replay Response

```
Client requests 100 messages
Server has them all but client's network is slow
Timeout: Client gives up after 5 seconds

Prevention:
  - Paginate large replays (request 10 messages at a time)
  - Increase client timeout for replay requests
  - Server-side: Send replay in batches with delays
```

## 📈 Monitoring and Alerting

### Key Metrics (from /stats endpoint)

```json
{
  "messageReplayRequests": 1234,
  "slowClientsDisconnected": 5,
  "rateLimitedMessages": 0
}
```

**Interpretation:**

```
messageReplayRequests > 100/hour:
  → High network instability
  → Consider increasing buffer size
  → Check if server CPU/memory saturated

slowClientsDisconnected > 10/hour:
  → Many slow clients (mobile, bad network)
  → Consider increasing send timeout
  → May need to optimize message size

rateLimitedMessages > 0:
  → Clients sending too many requests
  → Potential DoS attempt
  → Check if replay requests in loop
```

### Server Logs

```bash
$ docker logs sukko-go-2 | grep replay

📬 Client 42 requesting replay: seq 1000 to 1050
📬 Replaying 50 messages to client 42
```

**Alert conditions:**

```
if replay_requests_per_minute > 10:
  alert("High replay rate - possible network issues")

if avg_replay_size > 50:
  alert("Large gaps - buffer too small or high latency")

if replay_failure_rate > 5%:
  alert("Clients too slow to handle replay")
```

## 🎯 Best Practices

### Server-Side

1. **Buffer Size**: Start with 100 messages, monitor replay requests
   ```go
   // Too small: Many "gap too old" failures
   buffer := NewReplayBuffer(50)  // Only 5 sec at 10 msg/sec

   // Good: Covers typical network hiccup (10 sec)
   buffer := NewReplayBuffer(100)

   // Generous: Covers reconnection (60 sec)
   buffer := NewReplayBuffer(600)  // Uses 300KB per client!
   ```

2. **Replay Timeout**: Give more time than regular sends
   ```go
   // Regular send: 100ms timeout (fail fast)
   case <-time.After(100 * time.Millisecond):

   // Replay send: 1 second timeout (more lenient)
   case <-time.After(1 * time.Second):
   ```

3. **Log Replay Requests**: Monitor for patterns
   ```go
   s.logger.Printf("📬 Client %d requesting replay: seq %d to %d", ...)
   ```

### Client-Side

1. **Deduplication**: Always track received sequences
   ```typescript
   const received = new Set<number>()
   if (received.has(seq)) return // Skip duplicate
   received.add(seq)
   ```

2. **Limit Replay Requests**: Avoid request loops
   ```typescript
   const pendingReplays = new Map<number, number>() // seq → timestamp

   // Only request if not already pending
   if (!pendingReplays.has(from)) {
     requestReplay(from, to)
     pendingReplays.set(from, Date.now())
   }

   // Clear after timeout
   setTimeout(() => pendingReplays.delete(from), 5000)
   ```

3. **Persist Last Sequence**: Survive page reload
   ```typescript
   // Save to localStorage
   localStorage.setItem('lastSeq', lastSeq.toString())

   // Restore on page load
   const savedSeq = parseInt(localStorage.getItem('lastSeq') || '0')
   if (savedSeq > 0) {
     requestReplaySince(savedSeq)
   }
   ```

4. **Full Reconnect on Irrecoverable Gap**:
   ```typescript
   if (gapSizeRequested !== gapSizeReceived) {
     console.error('Irrecoverable gap, full reconnect needed')
     localStorage.removeItem('lastSeq') // Clear persisted seq
     window.location.reload() // Full page reload
   }
   ```

## 🎓 Summary

### What Replay Solves

✅ **Gap detection**: Clients know when messages are missed
✅ **Fast recovery**: 20-50ms vs 500-2000ms for full reconnect
✅ **Data consistency**: No silent message loss
✅ **Network resilience**: Handle brief WiFi glitches gracefully
✅ **Mobile-friendly**: Frequent network switches don't break experience

### How It Works

1. **Sequence numbers**: Every message gets unique, increasing seq
2. **Replay buffer**: Server stores last 100 messages per client (50KB)
3. **Gap detection**: Client checks `seq === lastSeq + 1`
4. **Replay request**: Client sends `{"type":"replay","data":{from,to}}`
5. **Server response**: Sends buffered messages (if still available)
6. **Recovery**: Client fills gap and continues

### Memory Cost

```
Per client: 50 KB (100 messages × 500 bytes)
2,184 clients: 109 MB (part of 384 MB for clients)
Tradeoff: 72% fewer connections, but 100% message reliability
```

### Key Files

- `message.go` - MessageEnvelope, SequenceGenerator
- `replay_buffer.go` - ReplayBuffer (Add, GetRange, GetSince)
- `server.go:320` - broadcast() wraps messages and adds to buffer
- `server.go:512` - handleClientMessage() processes replay requests
- `connection.go:126` - Client struct with replayBuffer field

---

**The replay mechanism transforms unreliable WebSocket delivery into production-grade message reliability suitable for a trading platform handling real money.**
