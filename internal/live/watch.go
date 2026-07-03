// Package live holds realtime room state: activities and (in Task 5) the WebSocket hub.
package live

import "fmt"

type WatchState struct {
	MediaID   int64   `json:"mediaId"`
	Paused    bool    `json:"paused"`
	Position  float64 `json:"position"`
	Rate      float64 `json:"rate"`
	Version   int64   `json:"version"`
	UpdatedAt int64   `json:"updatedAt"`
}

func NewWatch(mediaID int64, nowMs int64) WatchState {
	return WatchState{MediaID: mediaID, Paused: true, Rate: 1, Version: 1, UpdatedAt: nowMs}
}

// PositionAt projects the playhead to nowMs using the server-authoritative state.
func (s WatchState) PositionAt(nowMs int64) float64 {
	if s.Paused {
		return s.Position
	}
	return s.Position + float64(nowMs-s.UpdatedAt)/1000*s.Rate
}

// Apply executes a client intent and returns the new state. Position of the
// current moment is materialized first so pause/play never lose time.
func (s WatchState) Apply(action string, pos float64, nowMs int64) (WatchState, error) {
	s.Position = s.PositionAt(nowMs)
	switch action {
	case "play":
		s.Paused = false
	case "pause":
		s.Paused = true
	case "seek":
		if pos < 0 {
			pos = 0
		}
		s.Position = pos
	default:
		return s, fmt.Errorf("unknown action %q", action)
	}
	s.Version++
	s.UpdatedAt = nowMs
	return s, nil
}
