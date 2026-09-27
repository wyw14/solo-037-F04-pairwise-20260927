package carriage

import (
    "time"

    "github.com/wyw14/solo-037/internal/model"
)

// DefaultStopTolerance 是刹停回执与位置测量相对核验时刻允许的最大时延。
const DefaultStopTolerance = 250 * time.Millisecond

// 停位核验未通过的原因。
const (
    StopReasonNoCommand          = "no active command"
    StopReasonCommandMismatch    = "command id or epoch mismatch"
    StopReasonMissingReceipt     = "missing brake receipt"
    StopReasonReceiptMismatch    = "receipt not bound to current command"
    StopReasonReceiptUnconfirmed = "receipt not confirmed"
    StopReasonReceiptVelocity    = "receipt velocity not zero"
    StopReasonReceiptStale       = "brake receipt stale"
    StopReasonWindowMissing      = "position window not configured"
    StopReasonPositionMissing    = "position not measured"
    StopReasonPositionRevision   = "position revision outside window revision"
    StopReasonPositionOutside    = "stopped position outside window"
    StopReasonPositionStale      = "position measurement stale"
    StopReasonPositionPremature  = "position measured before brake receipt"
)

// StopVerification 关联当前命令、刹停回执、位置测量与轨道窗口，
// 给出带时间戳的停位核验结论。Passed 为 false 时绝不代表合格，
// Reason 说明阻断原因。
type StopVerification struct {
    Run model.RunID `json:"run"`
    Command model.DeviceCommand `json:"command"`
    Receipt model.DeviceReceipt `json:"receipt"`
    Measurement PositionMeasurement `json:"measurement"`
    Window PositionWindow `json:"window"`
    VerifiedAt time.Time `json:"verified_at"`
    ReceiptAge time.Duration `json:"receipt_age"`
    PositionAge time.Duration `json:"position_age"`
    Tolerance time.Duration `json:"tolerance"`
    Passed bool `json:"passed"`
    Reason string `json:"reason,omitempty"`
}

// VerifyStop 用默认可接受时延核验停位。
func (b *BrakeCoordinator) VerifyStop(run model.RunID, commandID string, epoch uint64, at time.Time) StopVerification {
    return b.VerifyStopWithTolerance(run, commandID, epoch, at, DefaultStopTolerance)
}

// VerifyStopWithTolerance 关联当前命令、刹停回执、位置测量与轨道窗口，
// 在 at 时刻核验拖车是否停在试验允许区间内。回执或位置测量超过
// tolerance、缺失、与当前命令/修订不符，或位置落在窗口外，都不发布
// 合格结论。
func (b *BrakeCoordinator) VerifyStopWithTolerance(run model.RunID, commandID string, epoch uint64, at time.Time, tolerance time.Duration) StopVerification {
    at = at.UTC()
    verification := StopVerification{Run: run, VerifiedAt: at, Tolerance: tolerance}

    command := b.Controller.Commands(run)
    verification.Command = command
    if command.ID == "" {
        verification.Reason = StopReasonNoCommand
        return verification
    }
    if command.ID != commandID || command.Epoch != epoch {
        verification.Reason = StopReasonCommandMismatch
        return verification
    }

    receipt, receiptOK := b.Controller.Receipt(run)
    verification.Receipt = receipt
    if !receiptOK {
        verification.Reason = StopReasonMissingReceipt
        return verification
    }
    if receipt.CommandID != command.ID || receipt.Epoch != command.Epoch {
        verification.Reason = StopReasonReceiptMismatch
        return verification
    }
    if !receipt.Confirmed {
        verification.Reason = StopReasonReceiptUnconfirmed
        return verification
    }
    if receipt.Velocity != 0 {
        verification.Reason = StopReasonReceiptVelocity
        return verification
    }
    receiptAge := at.Sub(receipt.RecordedAt.UTC())
    verification.ReceiptAge = receiptAge
    if receiptAge < 0 || receiptAge > tolerance {
        verification.Reason = StopReasonReceiptStale
        return verification
    }

    if b.Position == nil {
        verification.Reason = StopReasonPositionMissing
        return verification
    }
    window := b.Position.Snapshot()
    verification.Window = window
    if window.End <= window.Start {
        verification.Reason = StopReasonWindowMissing
        return verification
    }

    measurement := b.Position.Measurement()
    verification.Measurement = measurement
    if !measurement.Recorded {
        verification.Reason = StopReasonPositionMissing
        return verification
    }
    if measurement.Revision != window.Revision {
        verification.Reason = StopReasonPositionRevision
        return verification
    }
    if !measurement.Within(window) {
        verification.Reason = StopReasonPositionOutside
        return verification
    }
    positionAge := at.Sub(measurement.ObservedAt.UTC())
    verification.PositionAge = positionAge
    if positionAge < 0 || positionAge > tolerance {
        verification.Reason = StopReasonPositionStale
        return verification
    }
    if measurement.ObservedAt.UTC().Before(receipt.RecordedAt.UTC()) {
        verification.Reason = StopReasonPositionPremature
        return verification
    }

    verification.Passed = true
    return verification
}

// BrakeAndVerify 下发刹停命令并立即在 at 时刻核验停位。
func (b *BrakeCoordinator) BrakeAndVerify(run model.RunID, commandID string, epoch uint64, at time.Time) (StopVerification, error) {
    receipt, err := b.Controller.Brake(run, commandID, epoch, at)
    if err != nil {
        return StopVerification{Run: run, VerifiedAt: at.UTC(), Tolerance: DefaultStopTolerance, Reason: err.Error()}, err
    }
    _ = receipt
    return b.VerifyStop(run, commandID, epoch, at), nil
}
