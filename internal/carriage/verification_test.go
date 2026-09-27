package carriage

import (
    "testing"
    "time"

    "github.com/wyw14/solo-037/internal/model"
    "github.com/wyw14/solo-037/internal/tank"
    "github.com/wyw14/solo-037/internal/track"
)

func newVerifiedHarness(t *testing.T) (*BrakeCoordinator, *Controller, *PositionState, model.RunID, model.DeviceCommand, time.Time) {
    t.Helper()
    tracks := track.NewCoordinator(tank.NewRegistry(2.2))
    controller := NewController()
    position := NewPositionState(model.NewRevisionID())
    position.SetWindow(5, 15)
    coordinator := NewBrakeCoordinatorWithPosition(controller, tracks, position)
    run := model.NewRunID()
    if _, err := tracks.Arm(run, "section-a", "carriage-1", time.Time{}); err != nil {
        t.Fatalf("arm: %v", err)
    }
    at := time.Unix(1700000000, 0)
    command := controller.Accelerate(run, 7, 3, 1.2, at)
    return coordinator, controller, position, run, command, at
}

func stop(coordinator *BrakeCoordinator, run model.RunID, command model.DeviceCommand, position *PositionState, receiptAt, observedAt, verifyAt time.Time) StopVerification {
    _, _ = coordinator.Controller.Brake(run, command.ID, command.Epoch, receiptAt)
    if !observedAt.IsZero() {
        position.Observe(10, observedAt)
    }
    return coordinator.VerifyStop(run, command.ID, command.Epoch, verifyAt)
}

func TestStopVerificationPass(t *testing.T) {
    coordinator, _, position, run, command, at := newVerifiedHarness(t)
    v := stop(coordinator, run, command, position, at, at.Add(time.Millisecond), at.Add(2*time.Millisecond))
    if !v.Passed {
        t.Fatalf("expected verification to pass, reason: %s", v.Reason)
    }
    if v.Reason != "" {
        t.Fatalf("passed verification should carry no reason, got %q", v.Reason)
    }
    if v.ReceiptAge != 2*time.Millisecond || v.PositionAge != time.Millisecond {
        t.Fatalf("unexpected ages receipt=%s position=%s", v.ReceiptAge, v.PositionAge)
    }
    if v.Measurement.Position != 10 || !v.Measurement.Within(v.Window) {
        t.Fatalf("measurement not reflected: %+v window %+v", v.Measurement, v.Window)
    }
}

func TestStopVerificationMissingReceipt(t *testing.T) {
    coordinator, _, _, run, command, at := newVerifiedHarness(t)
    v := coordinator.VerifyStop(run, command.ID, command.Epoch, at)
    if v.Passed || v.Reason != StopReasonMissingReceipt {
        t.Fatalf("got passed=%v reason=%q", v.Passed, v.Reason)
    }
}

func TestStopVerificationCommandMismatch(t *testing.T) {
    coordinator, _, _, run, command, at := newVerifiedHarness(t)
    v := coordinator.VerifyStop(run, "other-command", command.Epoch, at)
    if v.Passed || v.Reason != StopReasonCommandMismatch {
        t.Fatalf("got passed=%v reason=%q", v.Passed, v.Reason)
    }
}

func TestStopVerificationReceiptStale(t *testing.T) {
    coordinator, _, position, run, command, at := newVerifiedHarness(t)
    // 回执已超过容差窗口：即便位置新鲜，也不得发布合格结论。
    v := stop(coordinator, run, command, position,
        at.Add(-time.Second),
        at.Add(-500*time.Millisecond),
        at)
    if v.Passed || v.Reason != StopReasonReceiptStale {
        t.Fatalf("got passed=%v reason=%q", v.Passed, v.Reason)
    }
}

func TestStopVerificationPositionStale(t *testing.T) {
    coordinator, _, position, run, command, at := newVerifiedHarness(t)
    // 回执新鲜，但位置测量陈旧。
    v := stop(coordinator, run, command, position,
        at.Add(-10*time.Millisecond),
        at.Add(-time.Second),
        at)
    if v.Passed || v.Reason != StopReasonPositionStale {
        t.Fatalf("got passed=%v reason=%q", v.Passed, v.Reason)
    }
}

func TestStopVerificationPositionMissing(t *testing.T) {
    coordinator, _, _, run, command, at := newVerifiedHarness(t)
    v := stop(coordinator, run, command, coordinator.Position, at.Add(-time.Millisecond), time.Time{}, at)
    if v.Passed || v.Reason != StopReasonPositionMissing {
        t.Fatalf("got passed=%v reason=%q", v.Passed, v.Reason)
    }
}

func TestStopVerificationOutsideWindow(t *testing.T) {
    coordinator, _, position, run, command, at := newVerifiedHarness(t)
    if _, err := coordinator.Controller.Brake(run, command.ID, command.Epoch, at); err != nil {
        t.Fatalf("brake: %v", err)
    }
    position.Observe(42, at.Add(time.Millisecond))
    v := coordinator.VerifyStop(run, command.ID, command.Epoch, at.Add(2*time.Millisecond))
    if v.Passed || v.Reason != StopReasonPositionOutside {
        t.Fatalf("got passed=%v reason=%q", v.Passed, v.Reason)
    }
}

func TestStopVerificationPositionBeforeReceipt(t *testing.T) {
    coordinator, _, position, run, command, at := newVerifiedHarness(t)
    // 位置测量早于刹停回执，可能是运动中的旧读数，不能证明停位。
    v := stop(coordinator, run, command, position,
        at.Add(2*time.Millisecond),
        at.Add(time.Millisecond),
        at.Add(3*time.Millisecond))
    if v.Passed || v.Reason != StopReasonPositionPremature {
        t.Fatalf("got passed=%v reason=%q", v.Passed, v.Reason)
    }
}

func TestStopVerificationPositionRevisionMismatch(t *testing.T) {
    coordinator, _, position, run, command, at := newVerifiedHarness(t)
    if _, err := coordinator.Controller.Brake(run, command.ID, command.Epoch, at); err != nil {
        t.Fatalf("brake: %v", err)
    }
    position.Observe(10, at.Add(time.Millisecond))
    // 窗口重新标定为新修订版本，旧测量不再适用。
    position.UpdateRevision(model.NewRevisionID())
    position.SetWindow(5, 15)
    v := coordinator.VerifyStop(run, command.ID, command.Epoch, at.Add(2*time.Millisecond))
    if v.Passed || v.Reason != StopReasonPositionRevision {
        t.Fatalf("got passed=%v reason=%q", v.Passed, v.Reason)
    }
}

func TestBrakeAndReleaseGatedByVerification(t *testing.T) {
    coordinator, _, position, run, command, at := newVerifiedHarness(t)

    // 无位置测量：释放被拒，轨道租约仍保留。
    if err := coordinator.BrakeAndRelease(run, command.ID, command.Epoch, at); err == nil {
        t.Fatal("expected release without position to fail")
    }
    if _, ok := coordinator.Track.Lease(run); !ok {
        t.Fatal("track lease must be retained when verification fails")
    }

    // 位置陈旧：同样不得释放。
    position.Observe(10, at.Add(-time.Second))
    if err := coordinator.BrakeAndRelease(run, command.ID, command.Epoch, at); err == nil {
        t.Fatal("expected release with stale position to fail")
    }
    if _, ok := coordinator.Track.Lease(run); !ok {
        t.Fatal("track lease must be retained when position is stale")
    }

    // 新鲜且在窗口内：释放成功（观测时刻不早于本次刹停回执）。
    position.Observe(10, at.Add(2*time.Millisecond))
    if err := coordinator.BrakeAndRelease(run, command.ID, command.Epoch, at.Add(2*time.Millisecond)); err != nil {
        t.Fatalf("expected verified release to succeed, got %v", err)
    }
    if _, ok := coordinator.Track.Lease(run); ok {
        t.Fatal("track lease should be released after verified stop")
    }
}

func TestPositionMeasurementFreshness(t *testing.T) {
    at := time.Unix(1700000000, 0)
    state := NewPositionState(model.NewRevisionID())
    state.SetWindow(0, 100)
    if m := state.Measurement(); m.Fresh(time.Second, at) {
        t.Fatal("unrecorded measurement must never be fresh")
    }
    state.Observe(50, at.Add(-500*time.Millisecond))
    m := state.Measurement()
    if !m.Fresh(time.Second, at) {
        t.Fatal("measurement within max age should be fresh")
    }
    if m.Fresh(100*time.Millisecond, at) {
        t.Fatal("measurement older than max age should be stale")
    }
    if m.Fresh(time.Second, at.Add(-time.Second)) {
        t.Fatal("measurement observed after verify time must not be fresh")
    }
}
