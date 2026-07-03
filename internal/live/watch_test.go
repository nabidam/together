package live

import "testing"

func TestNewWatch(t *testing.T) {
	s := NewWatch(7, 1000)
	if !s.Paused || s.Position != 0 || s.Rate != 1 || s.Version != 1 || s.MediaID != 7 {
		t.Fatalf("bad initial state %+v", s)
	}
}

func TestPlayAdvancesPosition(t *testing.T) {
	s := NewWatch(1, 0)
	s, _ = s.Apply("play", 0, 10_000)
	if s.Paused || s.Version != 2 {
		t.Fatalf("%+v", s)
	}
	if got := s.PositionAt(70_000); got != 60 { // 60s after play
		t.Fatalf("want 60 got %v", got)
	}
}

func TestPauseFreezesPosition(t *testing.T) {
	s := NewWatch(1, 0)
	s, _ = s.Apply("play", 0, 0)
	s, _ = s.Apply("pause", 0, 30_000) // paused at 30s
	if !s.Paused || s.Position != 30 {
		t.Fatalf("%+v", s)
	}
	if got := s.PositionAt(99_000); got != 30 {
		t.Fatalf("paused must not advance, got %v", got)
	}
}

func TestSeekWhilePlaying(t *testing.T) {
	s := NewWatch(1, 0)
	s, _ = s.Apply("play", 0, 0)
	s, _ = s.Apply("seek", 300, 10_000)
	if s.Paused {
		t.Fatal("seek must not pause")
	}
	if got := s.PositionAt(20_000); got != 310 {
		t.Fatalf("want 310 got %v", got)
	}
}

func TestSeekClampsNegative(t *testing.T) {
	s := NewWatch(1, 0)
	s, _ = s.Apply("seek", -5, 0)
	if s.Position != 0 {
		t.Fatalf("want 0 got %v", s.Position)
	}
}

func TestUnknownActionErrors(t *testing.T) {
	s := NewWatch(1, 0)
	if _, err := s.Apply("explode", 0, 0); err == nil {
		t.Fatal("want error")
	}
}

func TestVersionMonotonic(t *testing.T) {
	s := NewWatch(1, 0)
	for i, a := range []string{"play", "seek", "pause", "play"} {
		s, _ = s.Apply(a, 10, int64(i)*1000)
		if s.Version != int64(i)+2 {
			t.Fatalf("version %d after %d actions", s.Version, i+1)
		}
	}
}

func TestClockGoingBackwardDoesNotRewind(t *testing.T) {
	s := NewWatch(1, 1000)
	s, _ = s.Apply("play", 0, 1000)
	if got := s.PositionAt(500); got != 0 { // clock stepped back
		t.Fatalf("PositionAt must clamp, got %v", got)
	}
	s, _ = s.Apply("pause", 0, 500) // pause with regressed clock
	if s.Position < 0 {
		t.Fatalf("Position went negative: %v", s.Position)
	}
}
