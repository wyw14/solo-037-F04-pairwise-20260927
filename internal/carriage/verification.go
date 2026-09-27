package carriage

import (
    "fmt"
    "time"

    "github.com/wyw14/solo-037/internal/model"
)

// 核验失效原因；任何非空原因都意味着不得发布“停位合格”结论。
const (
    StopReasonCommandMismatch = "command superseded"
    StopReasonMissingReceipt = "missing brake receipt"
    StopReasonReceiptStale = "brake receipt stale"
    StopReasonNoMeasurement = "position measurement unavailable"
    StopReasonMeasurementBeforeStop = "position measurement predates stop"
    StopReasonPositionStale = "position measurement stale"
    StopReasonRevisionMismatch = "position window revision mismatch"
    StopReasonOutsideWindow = "stopped position outside allowed window"
)

// DefaultFreshness 是停位核验默认允许的证据最大时延：
// 回执与位置测量都必须在核验时刻前的该时长之内，且测量不得早于停稳。
const DefaultFreshness = 250 * time.Millisecond

// StopVerification 是带时间的停位核验结果，关联：
// 当前加速命令（ID/Epoch/Setpoint/IssuedAt）、刹停回执（停稳时间）、
// 位置测量（位置/观测时间）与轨道窗口（版本/区间）。
//
// Acceptable 为 true 才表示停在试验允许区间内；任何证据缺失或过期，
// 都只给出 Reason，绝不发布合格结论。
type StopVerification struct {
    Run model.RunID `json:"run"`
    CommandID string `json:"command_id"`
    Epoch uint64 `json:"epoch"`
    Setpoint float64 `json:"setpoint"`
    IssuedAt time.Time `json:"issued_at"`
    StoppedAt time.Time `json:"stopped_at"`
    Position float64 `json:"position"`
    ObservedAt time.Time `json:"observed_at"`
    Window PositionWindow `json:"window"`
    ReceiptFreshness time.Duration `json:"receipt_freshness"`
    PositionFreshness time.Duration `json:"position_freshness"`
    VerifiedAt time.Time `json:"verified_at"`
    Reason string `json:"reason,omitempty"`
    Acceptable bool `json:"acceptable"`
}

// StopVerificationRequest 携带核验所需的命令身份与时效参数。
type StopVerificationRequest struct {
    Run model.RunID
    CommandID string
    Epoch uint64
    Revision model.RevisionID
    // At 为核验时刻；零值取位置测量时刻（仍需停稳回执更早）。
    At time.Time
    // ReceiptFreshness 限定刹停回执距核验时刻的最大时延，零值取 DefaultFreshness。
    ReceiptFreshness time.Duration
    // PositionFreshness 限定位置测量距核验时刻的最大时延，零值取 DefaultFreshness。
    PositionFreshness time.Duration
}

// VerifyStop 关联当前命令、刹停回执、位置测量与轨道窗口，给出带时间的停位核验。
// 该函数只做核验，不改变任何状态；调用方应仅在 Acceptable 时释放轨道租约。
func (b *BrakeCoordinator) VerifyStop(req StopVerificationRequest) StopVerification {
    result := StopVerification{
        Run: req.Run,
        CommandID: req.CommandID,
        Epoch: req.Epoch,
        ReceiptFreshness: req.ReceiptFreshness,
        PositionFreshness: req.PositionFreshness,
    }
    if result.ReceiptFreshness <= 0 {
        result.ReceiptFreshness = DefaultFreshness
    }
    if result.PositionFreshness <= 0 {
        result.PositionFreshness = DefaultFreshness
    }

    // 1) 当前命令：核验针对的必须是仍在生效的加速命令。
    command := b.Controller.Commands(req.Run)
    if command.ID != req.CommandID || command.Epoch != req.Epoch {
        result.Reason = StopReasonCommandMismatch
        return result.finalize(req.At)
    }
    result.Setpoint = command.Setpoint
    result.IssuedAt = command.IssuedAt

    // 2) 刹停回执：必须存在、与命令绑定、已确认且速度归零；
    //    回执本身也要在时效内——过期回执不能证明“现在”仍停着。
    receipt, ok := b.Controller.Receipt(req.Run)
    if !ok {
        result.Reason = StopReasonMissingReceipt
        return result.finalize(req.At)
    }
    if receipt.CommandID != req.CommandID || receipt.Epoch != req.Epoch || !receipt.Confirmed || receipt.Velocity != 0 {
        result.Reason = StopReasonReceiptStale
        return result.finalize(req.At)
    }
    result.StoppedAt = receipt.RecordedAt

    // 3) 位置测量与轨道窗口。
    measurement := b.Position.Measurement()
    result.Position = measurement.Position
    result.ObservedAt = measurement.ObservedAt
    result.Window = measurement.Window

    verifyAt := req.At
    if verifyAt.IsZero() {
        verifyAt = measurement.ObservedAt
    }
    result.VerifiedAt = verifyAt.UTC()

    if measurement.ObservedAt.IsZero() {
        result.Reason = StopReasonNoMeasurement
        return result
    }
    // 窗口必须属于当前试验版本；窗口版本不符说明区间已重标定，旧测量不可用。
    if measurement.Window.Revision != req.Revision {
        result.Reason = StopReasonRevisionMismatch
        return result
    }
    // 位置测量必须在停稳之后，否则量到的是滑行中的位置。
    if measurement.ObservedAt.Before(receipt.RecordedAt) {
        result.Reason = StopReasonMeasurementBeforeStop
        return result
    }
    // 回执与测量各自独立保鲜：任一过期都不得发布合格结论。
    if receiptAge := verifyAt.Sub(receipt.RecordedAt); receiptAge < 0 || receiptAge > result.ReceiptFreshness {
        result.Reason = StopReasonReceiptStale
        return result
    }
    if !measurement.Fresh(verifyAt, result.PositionFreshness) {
        result.Reason = StopReasonPositionStale
        return result
    }
    // 4) 停点必须落在试验允许区间内。
    if measurement.Position < measurement.Window.Start || measurement.Position > measurement.Window.End {
        result.Reason = StopReasonOutsideWindow
        return result
    }

    result.Acceptable = true
    return result
}

// finalize 在证据不足、无法确定核验时刻时只补齐核验时间，不改变失效原因。
func (v StopVerification) finalize(at time.Time) StopVerification {
    if !at.IsZero() {
        v.VerifiedAt = at.UTC()
    }
    return v
}

// BrakeVerifyAndRelease 执行刹停并做停位核验；仅当停位合格时才释放轨道租约。
// 回执或位置过期、停点越界等情况下返回带原因的核验结果与错误，租约保持占用。
func (b *BrakeCoordinator) BrakeVerifyAndRelease(req StopVerificationRequest, brakeAt time.Time) (StopVerification, error) {
    if _, err := b.Controller.Brake(req.Run, req.CommandID, req.Epoch, brakeAt); err != nil {
        return StopVerification{Run: req.Run, CommandID: req.CommandID, Epoch: req.Epoch, Reason: StopReasonCommandMismatch, VerifiedAt: brakeAt.UTC()}, err
    }
    verification := b.VerifyStop(req)
    if !verification.Acceptable {
        return verification, fmt.Errorf("stop verification failed: %s", verification.Reason)
    }
    if err := b.Track.Release(req.Run); err != nil {
        return verification, err
    }
    return verification, nil
}
