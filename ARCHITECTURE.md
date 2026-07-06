# Software Architecture Document: Together (V1)

## 1. System Overview

Together is an ephemeral, real-time collaboration application designed for low-bandwidth, highly-synchronized multimedia consumption. To meet the constraints of running on restricted hardware (2 cores, 2GB RAM) and avoid the overhead of heavy media streaming, the system employs a **Thin Server / Thick Client** topology.

The backend acts exclusively as an in-memory state manager and real-time event router (signaling server) using WebSockets. Static media files are served via a highly optimized web server (e.g., Nginx or a lightweight CDN) for straightforward HTTP downloads. The client assumes the heavy lifting of local media playback, optimistic canvas rendering, and UI state reconciliation.

## 2. Module Responsibilities and Boundaries

### 2.1 Frontend Client (Single Page Application)

- **Media Controller:** Manages the HTML5 `<video>/<audio>` element, intercepts local playback events, blocks default behaviors when needed, and compares local timestamps with the server's truth source.
- **Canvas Engine:** Captures local pointer events, renders optimistic strokes on an off-screen or primary HTML5 Canvas, batches coordinate changes, and renders incoming network strokes.
- **Signaling Manager:** Maintains the WebSocket connection, handles reconnection with exponential backoff, and sequences incoming/outgoing real-time messages.
- **UI/Layout Manager:** Manages the collapsible side-panel (theater mode), user status indicators, and modal prompts (e.g., File Mismatch warning).

### 2.2 Backend Server (Signaling Node)

- **Room & State Manager:** Manages the lifecycle of ephemeral rooms. Handles role assignments, host migrations (on disconnect), and user kicking.
- **Event Broadcaster:** Validates and fans out WebSocket events (chat, canvas batched strokes, media sync) to appropriate room subsets (pub/sub).
- **Static Asset Server:** A simple HTTP module serving the catalog of available static media files and their metadata.

## 3. Data Model (In-Memory Schema)

_Note: Per the PRD, all state is ephemeral. The "schema" represents primary in-memory data structures (Dictionaries/HashMaps for O(1) lookups)._

### 3.1 DDL / Memory Structures

```typescript
// Index: Map<RoomID, Room>
interface Room {
  id: string; // PK, UUID or NanoID
  hostId: string; // FK -> User.id
  mediaId: string | null; // FK -> Media.id
  canvasState: Stroke[]; // Array of completed strokes for late-joiners
  createdAt: number; // Timestamp
}

// Index: Map<UserID, User>
interface User {
  id: string; // PK, UUID
  roomId: string; // FK -> Room.id (Constraint: User must belong to 1 Room)
  socketId: string; // Unique connection identifier
  displayName: string; // Constraint: length 2-32, stripped whitespace
  role: "HOST" | "GUEST";
  status: "WAITING" | "DOWNLOADING" | "FILE_READY" | "IN_SYNC";
  playbackTime: number; // Last reported media timestamp
  lastPing: number; // For dead-connection culling
}

interface Stroke {
  userId: string;
  points: [x: number, y: number][]; // Batched coordinates
  color: string;
  width: number;
}

// Static configuration mapped in memory
interface MediaCatalogItem {
  id: string; // PK
  filename: string;
  sizeBytes: number;
  url: string;
}
```

### 3.2 Constraints & Indices

- **Indices:**
- `rooms_by_id`: Hash map for `O(1)` room lookups.
- `users_by_id`: Hash map for `O(1)` user lookups.
- `users_by_room`: Multimap/List mapped by `roomId` to quickly retrieve all users in a specific room `O(k)`.

- **Constraints:**
- `displayName`: Enforced at the API boundary; must match `^\\S(.*\\S)?$` and be 2-32 characters.
- `canvas_limits`: Max stroke batch size (e.g., max 100 points per payload) to prevent memory exhaustion.
- `room_capacity`: Hardcoded limit (e.g., 50 users/room) to protect the 2GB RAM limit.

## 4. API Contract

### 4.1 HTTP REST Endpoints

- **`GET /api/media`**
- _Response:_ `200 OK` `[{ id, filename, sizeBytes, url }]`
- _Auth:_ None.

- **`POST /api/rooms`**
- _Request:_ Empty body.
- _Response:_ `201 Created` `{ roomId, hostToken }`
- _Auth:_ None. Creates room and generates an ephemeral JWT `hostToken`.

### 4.2 WebSocket Contract

_Transport: WSS. All payloads are JSON._

**Client -> Server Events:**

- `JOIN_ROOM`: `{ roomId, displayName, hostToken? }`
- `MEDIA_ACTION`: `{ action: "PLAY"|"PAUSE"|"SEEK", timestamp: float }` (Host only)
- `CANVAS_DRAW`: `{ points: [[x,y], ...], color, width }` (Sent every 50ms during draw)
- `CANVAS_CLEAR`: `{}` (Host only)
- `CHAT_MESSAGE`: `{ content: string }`
- `UPDATE_STATUS`: `{ status: "DOWNLOADING"|"FILE_READY"|"IN_SYNC" }`
- `MANAGE_USER`: `{ targetUserId, action: "PROMOTE"|"KICK" }` (Host only)

**Server -> Client Events:**

- `ROOM_STATE_SYNC`: `{ users: [...], hostId, mediaId, currentCanvas: [...] }` (Sent on join)
- `USER_JOINED` / `USER_LEFT`: `{ user: User }` / `{ userId }`
- `USER_UPDATED`: `{ userId, status, role }`
- `MEDIA_SYNC`: `{ action, timestamp, hostId }`
- `CANVAS_UPDATE`: `{ userId, points, color, width }`
- `CANVAS_CLEARED`: `{}`
- `CHAT_BROADCAST`: `{ messageId, userId, displayName, content, timestamp }`
- `KICKED`: `{ reason }`
- `ERROR`: `{ code, message }`

## 5. Component Hierarchy (Frontend)

```text
AppRoot
 ├── ConnectionBoundary (handles WS offline/reconnect overlays)
 └── RoomLayout
      ├── MainTheaterArea
      │    ├── EmptyState (Waiting for Host/Media)
      │    ├── MediaLayer
      │    │    ├── LocalFileInput
      │    │    └── HTML5 Video/Audio Player
      │    └── CanvasLayer
      │         ├── DrawingSurface (Canvas API)
      │         └── Toolbar (Brush size, Export, Clear)
      └── CollapsibleSidePanel
           ├── ParticipantList
           │    └── ParticipantItem (Name, Status Badge, Host Controls)
           └── ChatModule
                ├── MessageHistory
                └── ChatInput (Text, Emoji)

```

## 6. Dependency Graph

```text
[Browser Clients]
   │    │
   │    └── HTTP GET (Media Download) ──> [Static Media Server / Nginx]
   │
   └── WebSocket (WSS) ──> [Node.js Signaling Server]
                              ├── Socket.io / ws Engine
                              ├── Room Manager (In-Memory)
                              ├── Canvas Batch Processor
                              └── Media Drift Analyzer

```

## 7. Error Handling Strategy

1. **Playback Drift Correction:** The client continuously tracks its local playback time against the last known `MEDIA_SYNC` timestamp. If `|localTime - (hostTime + networkLatency)| > 1.5s`, the client silently invokes `video.currentTime = hostTime` to forcefully resynchronize.
2. **File Mismatch Evaluation:** Upon the user selecting a local file, the client checks `file.name` and `file.size` against the `MediaCatalogItem`. If discordant, a non-blocking UI toast appears: _"Warning: The loaded file appears to be different from the Host's selection. Sync issues may occur."_
3. **Connection Resilience (Network Drop):**

- Client UI transitions to an offline state, disabling inputs.
- Socket logic attempts reconnection using an exponential backoff algorithm (1s, 2s, 4s, 8s...).
- Upon reconnection, the client issues a `SYNC_REQUEST` to fetch missed chat and canvas states.

4. **Host Migration & Dropout:**

- If the backend detects a Host socket closure, it triggers an immediate timer (e.g., 5 seconds for transient drops).
- If the Host does not reconnect, the backend selects the oldest `createdAt` `User` in the room, updates their role to `HOST`, and broadcasts `USER_UPDATED`.

5. **Canvas Event Overload Mitigation:**

- _Client:_ Debounces mouse/touch events. Pushes points to a buffer. Flushes buffer via WS every 50ms.
- _Server:_ Drops malformed coordinates or massive payloads (>1KB per frame) to prevent memory-based DoS.

## 8. Configuration Strategy

Configuration is managed entirely via environment variables (`.env`) injected at container/process runtime.

- `PORT`: Server binding port (Default: 3000)
- `MAX_ROOM_SIZE`: Limits participant count to constrain memory per room (Default: 50)
- `PING_INTERVAL`: WebSocket heartbeat interval in ms (Default: 10000)
- `CANVAS_BATCH_RATE`: Millisecond delay expected between canvas payloads (Default: 50)
- `STATIC_MEDIA_PATH`: Absolute path or URL to the media directory/CDN.
- `MAX_SERVER_MEMORY_MB`: Threshold to reject new room creations if the 2GB server limit is nearing capacity (e.g., 1800).
