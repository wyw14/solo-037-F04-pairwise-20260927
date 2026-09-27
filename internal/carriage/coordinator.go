package carriage

import (
    "errors"
    "time"

    "github.com/wyw14/solo-037/internal/model"
    "github.com/wyw14/solo-037/internal/track"
)

type BrakeCoordinator struct {
    Controller *Controller
    Track *track.Coordinator
    Position *PositionState
}

func NewBrakeCoordinator(controller *Controller, tracks *track.Coordinator) *BrakeCoordinator {
    return &BrakeCoordinator{Controller: controller, Track: tracks}
}

// NewBrakeCoordinatorWithPosition 在刹停协调器上关联位置测量与轨道窗口，
// 使刹停流程能够发布带时间的停位核验结论。
func NewBrakeCoordinatorWithPosition(controller *Controller, tracks *track.Coordinator, position *PositionState) *BrakeCoordinator {
    return &BrakeCoordinator{Controller: controller, Track: tracks, Position: position}
}

// BrakeAndRelease 只有在停位核验通过（速度归零且停在窗口内、回执与位置
// 测量均未过期）时才释放轨道；核验不通过时保留轨道占用。
func (b *BrakeCoordinator) BrakeAndRelease(run model.RunID, commandID string, epoch uint64, at time.Time) error {
    receipt, err := b.Controller.Brake(run, commandID, epoch, at)
    if err != nil {
        return err
    }
    if !receipt.Confirmed || receipt.Velocity != 0 || !b.Controller.Stopped(run, commandID, epoch) {
        return errors.New("brake proof incomplete")
    }
    verification := b.VerifyStop(run, commandID, epoch, at)
    if !verification.Passed {
        if verification.Reason == "" {
            return errors.New("stop verification failed")
        }
        return errors.New("stop verification failed: " + verification.Reason)
    }
    return b.Track.Release(run)
}

func (b *BrakeCoordinator) Hold(run model.RunID) bool {
    lease, ok := b.Track.Lease(run)
    return ok && b.Controller.Velocity(run) > 0 && lease.Run == run
}
