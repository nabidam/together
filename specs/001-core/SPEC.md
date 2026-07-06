# SPEC.md: Together (V1)

## 1. Core Features & User Stories

**Ephemeral Session Management**

- _As a Host,_ I can create a new ephemeral room and generate a unique invite link.
- _As a Host,_ I can promote other users or kick disruptive ones.
- _As a Guest,_ I can join via a link, choose a temporary display name, and enter the room without creating an account.

**Media Sync & Local Playback (Watch & Listen)**

- _As a Host,_ I can select a static media file (movie or music) hosted on the server to make it available to the room.
- _As a User,_ I can click "Download Media" to save the static file to my local device.
- _As a User,_ I can load my locally downloaded file into the web player so I can watch without relying on continuous bandwidth.
- _As a User,_ when anyone in the room clicks play, pause, or scrubs the timeline, my local player's state syncs instantly via WebSockets.

**Collaborative Canvas**

- _As a User,_ I can select basic brush tools to draw on a real-time shared canvas.
- _As a User,_ I can click "Export" to instantly save a `.png` snapshot of our current canvas to my device.
- _As a Host,_ I can clear the canvas for everyone.

**Text Chat**

- _As a User,_ I can send text messages and emojis in a persistent side-panel chat to react to the movie or music without interrupting the audio.

## 2. Edge Cases & Error Handling

- **File Mismatch:** Because users are loading local files, there is a risk that User A loads the movie while User B accidentally loads a random video. The system should ideally check the file name or size upon local selection and warn the user if it doesn't match the Host's designated file.
- **Drift Correction:** Even with local files, browser rendering speeds can cause timestamps to drift. The client should silently sync to the Host's timestamp if it drifts by more than 1.5 seconds.
- **Host Disconnection:** If the Host loses connection, the server must automatically transfer the "Host" token to the next oldest connected user to keep the room alive.
- **Canvas Event Overload:** WebSocket messages for canvas drawing must be batched on the client side (e.g., sending an array of coordinate strokes every 50ms) to avoid CPU spikes on the server.

## 3. Suggested Tech Stack

To comfortably run this on a 2-core, 2GB RAM machine while ensuring high performance, we will utilize highly efficient, compiled backend routing and a reactive frontend.

- **Backend:** Go, utilizing the **Gin** framework for fast, lightweight HTTP routing.
- **Configuration & Logging:** **Viper** for environment management and **Zap** for structured, high-performance logging (crucial for debugging WebSocket drops without consuming heavy resources).
- **Real-time Communication:** `gorilla/websocket` for Go. State is held in memory for active rooms.
- **Database (For static media metadata only):** SQLite. It is lightweight, requires no background service, and easily fits within the RAM constraints.
- **Frontend Framework:** SvelteKit (Vite).
- **UI Components:** **Shadcn-svelte** intertwined with Tailwind CSS to rapidly build out the interface.
- **Canvas API:** Native HTML5 `<canvas>` API with `.toDataURL('image/png')` for the export function.

## 4. UI/UX Guidelines

- **Warm & Minimalist Aesthetic:** The UI should avoid sterile, overly corporate designs. Use warm, inviting color palettes, soft rounded corners, and a minimalist layout that keeps the focus entirely on the media and the connection between users.
- **Theater Mode Priority:** The video player or canvas should dominate the viewport. The chat and room participant list should be elegantly tucked into a collapsible side-panel.
- **Clear State Indicators:** Next to each user's name in the participant list, display a status dot (e.g., "Downloading," "File Ready," "In Sync") so the Host knows when everyone is ready to hit play.
- **Optimistic Canvas Rendering:** Local brush strokes must render instantly on the user's screen before the WebSocket event is sent, ensuring the drawing experience feels organic and zero-latency.
