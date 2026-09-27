package carriage

import (
    "sync"
    "time"

    "github.com/wyw14/solo-037/internal/model"
)

type PositionWindow struct {
    Revision model.RevisionID `json:"revision"`
    Start float64 `json:"start"`
    End float64 `json:"end"`
    Entered bool `json:"entered"`
}

// PositionMeasurement 是带观测时间的单次位置测量。
type PositionMeasurement struct {
    Position float64 `json:"position"`
    Revision model.RevisionID `json:"revision"`
    ObservedAt time.Time `json:"observed_at"`
    Recorded bool `json:"recorded"`
}

// Fresh 判断测量相对 at 是否未超过 maxAge。没有记录的测量永远不新鲜。
func (m PositionMeasurement) Fresh(maxAge time.Duration, at time.Time) bool {
    if !m.Recorded {
        return false
    }
    age := at.UTC().Sub(m.ObservedAt.UTC())
    return age >= 0 && age <= maxAge
}

// Within 判断位置是否落在给定窗口（含端点）且修订版本一致。
func (m PositionMeasurement) Within(window PositionWindow) bool {
    return m.Recorded && m.Revision == window.Revision &&
        m.Position >= window.Start && m.Position <= window.End
}

type PositionState struct {
    mu sync.Mutex
    revision model.RevisionID
    position float64
    positionAt time.Time
    positionRevision model.RevisionID
    recorded bool
    window PositionWindow
}

func NewPositionState(revision model.RevisionID) *PositionState {
    return &PositionState{revision: revision}
}

func (s *PositionState) UpdateRevision(revision model.RevisionID) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.revision = revision
    s.window = PositionWindow{Revision: revision}
}

func (s *PositionState) SetWindow(start float64, end float64) PositionWindow {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.window = PositionWindow{Revision: s.revision, Start: start, End: end}
    return s.window
}

func (s *PositionState) Observe(position float64, at time.Time) PositionWindow {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.position = position
    s.positionAt = at.UTC()
    s.positionRevision = s.revision
    s.recorded = true
    if position >= s.window.Start && position <= s.window.End && s.window.Revision == s.revision {
        s.window.Entered = true
    }
    return s.window
}

func (s *PositionState) Snapshot() PositionWindow {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.window
}

func (s *PositionState) Position() float64 {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.position
}

// Measurement 返回最近一次位置测量及其观测时间。
func (s *PositionState) Measurement() PositionMeasurement {
    s.mu.Lock()
    defer s.mu.Unlock()
    return PositionMeasurement{
        Position: s.position,
        Revision: s.positionRevision,
        ObservedAt: s.positionAt,
        Recorded: s.recorded,
    }
}
