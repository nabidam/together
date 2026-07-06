# Product Requirements Document: Together (V1)

## 1. Product Overview

Together is an ephemeral, real-time collaboration and media consumption platform. It allows users to join temporary rooms without creating an account to synchronously watch/listen to media, collaborate on a shared digital canvas, and communicate via text chat. The application circumvents heavy streaming bandwidth requirements by utilizing a "download and sync local playback" model.

## 2. User Stories & Acceptance Criteria

### 2.1 Ephemeral Session Management

- **Story:** As a Host, I can create a new ephemeral room and generate a unique invite link.
- **Acceptance Criteria:** System generates a unique, shareable URL. The creator is automatically assigned the "Host" role.

- **Story:** As a Guest, I can join via a link, choose a temporary display name, and enter the room without creating an account.
- **Acceptance Criteria:** Users accessing the invite link are prompted for a display name. No email, password, or persistent account generation is required.

- **Story:** As a Host, I can promote other users or kick disruptive ones.
- **Acceptance Criteria:** Host interface includes options next to user names to "Promote to Host" or "Remove from Room". Removed users are disconnected and blocked from re-entering the current active session.

### 2.2 Media Sync & Local Playback

- **Story:** As a Host, I can select a static media file (hosted on the server) to make it available to the room.
- **Acceptance Criteria:** Host can view a catalog of available static server media and designate one as the active room media.

- **Story:** As a User, I can click "Download Media" to save the static file to my local device.
- **Acceptance Criteria:** Users are presented with a clear download link/button for the Host-selected media.

- **Story:** As a User, I can load my locally downloaded file into the web player.
- **Acceptance Criteria:** The UI provides a file input to load the local file into the browser's media player.

- **Story:** As a User, when anyone in the room clicks play, pause, or scrubs the timeline, my local player's state syncs instantly.
- **Acceptance Criteria:** Play, pause, and seek events are broadcast to all users in the room. The local player state updates to match the incoming event with minimal latency.

### 2.3 Collaborative Canvas

- **Story:** As a User, I can select basic brush tools to draw on a real-time shared canvas.
- **Acceptance Criteria:** Canvas supports freehand drawing. Strokes are broadcast and rendered on all participants' screens.

- **Story:** As a User, I can click "Export" to instantly save a snapshot of our current canvas to my device.
- **Acceptance Criteria:** Export function generates and downloads a `.png` file of the current canvas state.

- **Story:** As a Host, I can clear the canvas for everyone.
- **Acceptance Criteria:** Host has a "Clear Canvas" button. Activating it wipes the canvas for all connected clients simultaneously.

### 2.4 Text Chat

- **Story:** As a User, I can send text messages and emojis in a persistent side-panel chat.
- **Acceptance Criteria:** Chat supports text and standard emoji inputs. Messages display the sender's temporary display name and a timestamp.

## 3. Functional Requirements

- **Session State Management:** The system must maintain real-time state for active rooms, including connected participants, participant roles (Host/Guest), participant readiness states, and canvas coordinate arrays.
- **Media State Synchronization:** The system must broadcast media control events (Play, Pause, Seek) to all connected WebSocket clients in a specific room.
- **Participant Status Tracking:** The system must track and display individual user states next to their names in the participant list (e.g., "Downloading," "File Ready," "In Sync").
- **Optimistic Rendering:** The client UI must render local canvas brush strokes instantly before the synchronization event is sent to the server.

## 4. Non-Functional Requirements

- **Hardware Efficiency:** The system must be capable of running smoothly on highly constrained environments (e.g., 2 CPU cores, 2GB RAM).
- **UI/UX Aesthetics:** The interface must feature a warm, minimalist aesthetic with soft rounded corners, avoiding overly corporate or sterile designs.
- **Viewport Priority (Theater Mode):** The media player and collaborative canvas must dominate the viewport. Chat and participant lists must be contained in an elegantly collapsible side-panel.

## 5. Validation Rules

- **Local File Verification:** When a user loads a local file for playback, the client must validate the file's name and/or file size against the Host's designated file metadata.
- **Display Name Constraints:** Guest display names must be between 2 and 32 characters, stripping leading/trailing whitespace and preventing empty inputs.

## 6. Error Cases & Edge Cases

- **File Mismatch:** If a user loads a local file that does not match the Host's selected media (by name or size), the UI must display a non-blocking warning indicating a potential mismatch.
- **Playback Drift:** If a client's local playback timestamp drifts by more than 1.5 seconds compared to the Host's timestamp, the client must silently seek to synchronize with the Host.
- **Host Disconnection:** If the current Host disconnects or drops offline, the system must automatically and immediately transfer the "Host" role to the oldest connected user in the room.
- **Canvas Event Overload:** To prevent server CPU spikes, client-side canvas drawing events must be batched (e.g., sending an array of coordinate strokes every 50ms) rather than sent per pixel/movement.
- **Empty State (Room):** When a user joins a room before media is selected, the media player area must display an empty state prompting them to wait for the Host to select media.
- **Empty State (Canvas):** A newly initialized canvas must be completely transparent/blank until the first stroke is registered.
- **Offline/Network Drop:** If a user loses WebSocket connection, the UI must gracefully disable real-time controls, show an "Offline / Reconnecting..." indicator, and attempt exponential backoff reconnection.

## 7. Constraints

- **Memory-Bound State:** Room states are ephemeral and held in memory; a server restart will inherently clear all active sessions and canvas states.
- **Stateless Media Playback:** The server does not stream the media content (e.g., via WebRTC or HLS); it relies entirely on local file loading and WebSocket trigger events to minimize bandwidth and processing requirements.

## 8. Out of Scope (V1)

- User account registration, login, and profile management.
- Persistent database storage for chat history or canvas drawings after a session ends.
- Server-side media streaming (video/audio encoding, HLS, WebRTC).
- Voice or video conferencing capabilities.
- Advanced canvas tools (layers, shape primitives, text insertion).

## 9. Future Improvements

- Persistent user accounts and historical room logs.
- Direct peer-to-peer (WebRTC) file sharing to avoid centralized server downloads.
- Expand canvas functionality to include layers, custom color palettes, and typed text.
- Interactive widgets such as room polls or shared sticky notes.
- Voice chat integration for background audio communication during playback.
