---
status: gate-passed
---

# Pause On Leave

An explicit final-tab leave pauses active video or audio playback for remaining participants and shows one `User Left.` toast. Socket drops preserve playback and show reconnecting presence for 10 seconds.

Acceptance criteria:
- An explicit final-tab leave pauses an actively playing room through server-authoritative playback state.
- Every remaining client receives one `user_left` event and shows one toast.
- Socket loss never pauses playback; reconnect clears the reconnecting state and notifies survivors.
- Hosts can resume playback and confirm playing while another participant is still downloading.
- Playback state changes identify their actor in a toast.
- Room teardown and empty-room behavior remain unchanged.

Out of scope: persisted leave history and changing readiness from advisory to a playback gate.
