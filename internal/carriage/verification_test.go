package carriage

import (
    "testing"
    "time"

    "github.com/wyw14/solo-037/internal/model"
    "github.com/wyw14/solo-037/internal/tank"
    "github.com/wyw14/solo-037/internal/track"
)

type stopFixture struct {
    coord *BrakeCoordinator
    controller *Controller
    position *PositionState
    tracks *track.Coordinator
    run model.RunID
    command model.DeviceCommand
    revision model.RevisionID
    t0 time.Time
}

func newStopFixture(t *testing.T, section string) stopFixture {
    t.Helper()
    t0 := time.Unix(1700000000, 0).UTC()
    registry := tank.NewRegistry(2.4)
    tracks := track.NewCoordinator(registry)
    controller := NewController()
    revision := model.NewRevisionID()
    positionState := NewPositionState(revision)
    positionState.SetWindow(5, 15)
    coord := NewBrakeCoordinator(controller, positionState, tracks)
    run := model.NewRunID()
    lease, err := tracks.Arm(run, section, "carriage-1", t0)
    if err != nil {
        t.Fatalf("arm: %v", err)
    }
    command := controller.Accelerate(run, lease.Fence, lease.Fence, 1.4, t0)
    return stopFixture{
        coord: coord, controller: controller, position: positionState, tracks: tracks,
        run: run, command: command, revision: revision, t0: t0,
    }
}

func (f stopFixture) request(at time.Time) StopVerificationRequest {
    return StopVerificationRequest{
        Run: f.run, CommandID: f.command.ID, Epoch: f.command.Epoch,
        Revision: f.revision, At: at, ReceiptFreshness: time.Second, PositionFreshness: time.Second,
    }
}

func TestStopVerificationAcceptableWhenStoppedInsideWindow(t *testing.T) {
    f := newStopFixture(t, "section-pass")
    if _, err := f.controller.Brake(f.run, f.command.ID, f.command.Epoch, f.t0.Add(10*time.Millisecond)); err != nil {
        t.Fatalf("brake: %v", err)
    }
    f.position.ObserveAt(10, f.t0.Add(20*time.Millisecond))

    v := f.coord.VerifyStop(f.request(f.t0.Add(30 * time.Millisecond)))
    if !v.Acceptable || v.Reason != "" {
        t.Fatalf("expected acceptable verification, got %+v", v)
    }
    if v.StoppedAt != f.t0.Add(10*time.Millisecond) || v.ObservedAt != f.t0.Add(20*time.Millisecond) || v.VerifiedAt != f.t0.Add(30*time.Millisecond) {
        t.Fatalf("verification timeline not bound: %+v", v)
    }
    if v.Position != 10 || v.Window.Start != 5 || v.Window.End != 15 || v.Setpoint != 1.4 {
        t.Fatalf("verification did not bind command/window/measurement: %+v", v)
    }
}

func TestStopVerificationMissingReceiptDoesNotPass(t *testing.T) {
    f := newStopFixture(t, "section-no-receipt")
    f.position.ObserveAt(10, f.t0.Add(20*time.Millisecond))

    v := f.coord.VerifyStop(f.request(f.t0.Add(30 * time.Millisecond)))
    if v.Acceptable || v.Reason != StopReasonMissingReceipt {
        t.Fatalf("missing receipt must not pass: %+v", v)
    }
}

func TestStopVerificationSupersededCommandDoesNotPass(t *testing.T) {
    f := newStopFixture(t, "section-stale-command")
    staleCommand := f.command
    // 新命令取代旧命令。
    f.controller.Accelerate(f.run, staleCommand.Epoch+1, staleCommand.Fence, 2.0, f.t0.Add(5*time.Millisecond))
    req := f.request(f.t0.Add(30 * time.Millisecond))

    v := f.coord.VerifyStop(req)
    if v.Acceptable || v.Reason != StopReasonCommandMismatch {
        t.Fatalf("superseded command must not pass: %+v", v)
    }
}

func TestStopVerificationMeasurementBeforeStopRejected(t *testing.T) {
    f := newStopFixture(t, "section-before-stop")
    // 位置测量早于停稳时刻：量到的是滑行位置。
    f.position.ObserveAt(10, f.t0.Add(5*time.Millisecond))
    if _, err := f.controller.Brake(f.run, f.command.ID, f.command.Epoch, f.t0.Add(10*time.Millisecond)); err != nil {
        t.Fatalf("brake: %v", err)
    }

    v := f.coord.VerifyStop(f.request(f.t0.Add(30 * time.Millisecond)))
    if v.Acceptable || v.Reason != StopReasonMeasurementBeforeStop {
        t.Fatalf("pre-stop measurement must not pass: %+v", v)
    }
}

func TestStopVerificationStaleReceiptDoesNotPass(t *testing.T) {
    f := newStopFixture(t, "section-stale-receipt")
    if _, err := f.controller.Brake(f.run, f.command.ID, f.command.Epoch, f.t0); err != nil {
        t.Fatalf("brake: %v", err)
    }
    f.position.ObserveAt(10, f.t0.Add(20*time.Millisecond))
    req := f.request(f.t0.Add(time.Second))
    req.ReceiptFreshness = 250 * time.Millisecond // 位置仍新鲜，回执已过期

    v := f.coord.VerifyStop(req)
    if v.Acceptable || v.Reason != StopReasonReceiptStale {
        t.Fatalf("stale receipt must not pass: %+v", v)
    }
}

func TestStopVerificationStalePositionDoesNotPass(t *testing.T) {
    f := newStopFixture(t, "section-stale-position")
    if _, err := f.controller.Brake(f.run, f.command.ID, f.command.Epoch, f.t0.Add(10*time.Millisecond)); err != nil {
        t.Fatalf("brake: %v", err)
    }
    f.position.ObserveAt(10, f.t0.Add(20*time.Millisecond))
    req := f.request(f.t0.Add(100 * time.Millisecond))
    req.PositionFreshness = 50 * time.Millisecond // 回执仍新鲜，位置已过期

    v := f.coord.VerifyStop(req)
    if v.Acceptable || v.Reason != StopReasonPositionStale {
        t.Fatalf("stale position must not pass: %+v", v)
    }
}

func TestStopVerificationNoMeasurementDoesNotPass(t *testing.T) {
    f := newStopFixture(t, "section-no-measurement")
    if _, err := f.controller.Brake(f.run, f.command.ID, f.command.Epoch, f.t0.Add(10*time.Millisecond)); err != nil {
        t.Fatalf("brake: %v", err)
    }

    v := f.coord.VerifyStop(f.request(f.t0.Add(30 * time.Millisecond)))
    if v.Acceptable || v.Reason != StopReasonNoMeasurement {
        t.Fatalf("missing measurement must not pass: %+v", v)
    }
}

func TestStopVerificationRevisionMismatchDoesNotPass(t *testing.T) {
    f := newStopFixture(t, "section-revision")
    if _, err := f.controller.Brake(f.run, f.command.ID, f.command.Epoch, f.t0.Add(10*time.Millisecond)); err != nil {
        t.Fatalf("brake: %v", err)
    }
    f.position.ObserveAt(10, f.t0.Add(20*time.Millisecond))
    req := f.request(f.t0.Add(30 * time.Millisecond))
    req.Revision = model.NewRevisionID() // 窗口属于旧版本

    v := f.coord.VerifyStop(req)
    if v.Acceptable || v.Reason != StopReasonRevisionMismatch {
        t.Fatalf("revision mismatch must not pass: %+v", v)
    }
}

func TestStopVerificationOutsideWindowDoesNotPass(t *testing.T) {
    f := newStopFixture(t, "section-outside")
    if _, err := f.controller.Brake(f.run, f.command.ID, f.command.Epoch, f.t0.Add(10*time.Millisecond)); err != nil {
        t.Fatalf("brake: %v", err)
    }
    f.position.ObserveAt(42, f.t0.Add(20*time.Millisecond))

    v := f.coord.VerifyStop(f.request(f.t0.Add(30 * time.Millisecond)))
    if v.Acceptable || v.Reason != StopReasonOutsideWindow {
        t.Fatalf("position outside window must not pass: %+v", v)
    }
}

func TestBrakeVerifyAndReleaseHoldsLeaseOnFailure(t *testing.T) {
    f := newStopFixture(t, "section-hold")
    // 停点越界：合格结论不得发布，租约必须保留。
    f.position.ObserveAt(42, f.t0.Add(20*time.Millisecond))
    v, err := f.coord.BrakeVerifyAndRelease(f.request(f.t0.Add(30*time.Millisecond)), f.t0.Add(10*time.Millisecond))
    if err == nil || v.Acceptable || v.Reason != StopReasonOutsideWindow {
        t.Fatalf("expected outside-window failure, got %+v err=%v", v, err)
    }
    if _, ok := f.tracks.Lease(f.run); !ok {
        t.Fatal("track lease must remain held after failed stop verification")
    }
}

func TestBrakeVerifyAndReleaseReleasesLeaseOnSuccess(t *testing.T) {
    f := newStopFixture(t, "section-release")
    f.position.ObserveAt(10, f.t0.Add(20*time.Millisecond))
    v, err := f.coord.BrakeVerifyAndRelease(f.request(f.t0.Add(30*time.Millisecond)), f.t0.Add(10*time.Millisecond))
    if err != nil || !v.Acceptable {
        t.Fatalf("expected successful verification, got %+v err=%v", v, err)
    }
    if _, ok := f.tracks.Lease(f.run); ok {
        t.Fatal("track lease must be released after acceptable stop verification")
    }
}

func TestPositionMeasurementFreshnessAndRevisionReset(t *testing.T) {
    revision := model.NewRevisionID()
    state := NewPositionState(revision)
    state.SetWindow(5, 15)
    at := time.Unix(1700000000, 0).UTC()

    if m := state.Measurement(); m.Fresh(at, time.Second) {
        t.Fatal("never-observed position must not be fresh")
    }
    state.ObserveAt(10, at)
    m := state.Measurement()
    if !m.Fresh(at.Add(100*time.Millisecond), time.Second) || m.Fresh(at.Add(2*time.Second), time.Second) {
        t.Fatalf("freshness window wrong: %+v", m)
    }

    state.UpdateRevision(model.NewRevisionID())
    if m := state.Measurement(); !m.ObservedAt.IsZero() || m.Window.Revision == revision {
        t.Fatalf("revision change must invalidate prior measurement: %+v", m)
    }
}
