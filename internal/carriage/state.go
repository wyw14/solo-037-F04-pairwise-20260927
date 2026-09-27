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

// PositionMeasurement 是一次带时间戳的位置观测，时间戳用于判断测量
// 相对于刹停回执是否仍然有效。
type PositionMeasurement struct {
    Position float64 `json:"position"`
    Window PositionWindow `json:"window"`
    ObservedAt time.Time `json:"observed_at"`
}

func (m PositionMeasurement) Fresh(at time.Time, maxAge time.Duration) bool {
    if m.ObservedAt.IsZero() {
        return false
    }
    age := at.Sub(m.ObservedAt)
    return age >= 0 && age <= maxAge
}

type PositionState struct {
    mu sync.Mutex
    revision model.RevisionID
    position float64
    observedAt time.Time
    window PositionWindow
}

func NewPositionState(revision model.RevisionID) *PositionState {
    return &PositionState{revision: revision}
}

func (s *PositionState) Revision() model.RevisionID {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.revision
}

func (s *PositionState) UpdateRevision(revision model.RevisionID) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.revision = revision
    s.window = PositionWindow{Revision: revision}
    // 版本切换后旧测量不再属于当前试验区间。
    s.position = 0
    s.observedAt = time.Time{}
}

func (s *PositionState) SetWindow(start float64, end float64) PositionWindow {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.window = PositionWindow{Revision: s.revision, Start: start, End: end}
    return s.window
}

func (s *PositionState) Observe(position float64) PositionWindow {
    return s.ObserveAt(position, time.Now())
}

// ObserveAt 记录带时间的位置观测；停位核验只接受这类带时间戳的测量。
func (s *PositionState) ObserveAt(position float64, at time.Time) PositionWindow {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.position = position
    s.observedAt = at.UTC()
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

// Measurement 返回最近一次位置测量及其所属轨道窗口。
func (s *PositionState) Measurement() PositionMeasurement {
    s.mu.Lock()
    defer s.mu.Unlock()
    return PositionMeasurement{Position: s.position, Window: s.window, ObservedAt: s.observedAt}
}

// ObservedAt 返回最近一次位置观测时间。
func (s *PositionState) ObservedAt() time.Time {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.observedAt
}
