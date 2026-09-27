package carriage

import (
    "time"

    "github.com/wyw14/solo-037/internal/model"
    "github.com/wyw14/solo-037/internal/track"
)

type BrakeCoordinator struct {
    Controller *Controller
    Position *PositionState
    Track *track.Coordinator
}

func NewBrakeCoordinator(controller *Controller, position *PositionState, tracks *track.Coordinator) *BrakeCoordinator {
    return &BrakeCoordinator{Controller: controller, Position: position, Track: tracks}
}

// BrakeAndRelease 刹停后做停位核验，仅在停位合格时释放轨道租约。
// 保留原签名作为便捷入口；需要指定版本、核验时刻或时效时使用 BrakeVerifyAndRelease。
func (b *BrakeCoordinator) BrakeAndRelease(run model.RunID, commandID string, epoch uint64, at time.Time) error {
    req := StopVerificationRequest{Run: run, CommandID: commandID, Epoch: epoch, Revision: b.Position.Revision(), At: at}
    _, err := b.BrakeVerifyAndRelease(req, at)
    return err
}

func (b *BrakeCoordinator) Hold(run model.RunID) bool {
    lease, ok := b.Track.Lease(run)
    return ok && b.Controller.Velocity(run) > 0 && lease.Run == run
}
