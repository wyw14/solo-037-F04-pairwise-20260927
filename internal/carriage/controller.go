package carriage

import (
    "errors"
    "sync"
    "time"

    "github.com/wyw14/solo-037/internal/model"
)

type Controller struct {
    mu sync.Mutex
    active map[model.RunID]model.DeviceCommand
    receipts map[model.RunID]model.DeviceReceipt
    velocities map[model.RunID]float64
}

func NewController() *Controller {
    return &Controller{active: make(map[model.RunID]model.DeviceCommand), receipts: make(map[model.RunID]model.DeviceReceipt), velocities: make(map[model.RunID]float64)}
}

func (c *Controller) Accelerate(run model.RunID, epoch uint64, fence uint64, velocity float64, at time.Time) model.DeviceCommand {
    c.mu.Lock()
    defer c.mu.Unlock()
    command := model.DeviceCommand{ID: string(model.NewEventID()), Run: run, Epoch: epoch, Fence: fence, Setpoint: velocity, IssuedAt: at.UTC()}
    c.active[run] = command
    c.velocities[run] = velocity
    return command
}

func (c *Controller) SetReceipt(receipt model.DeviceReceipt) {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.receipts[findRun(c.active, receipt.CommandID)] = receipt
}

func findRun(commands map[model.RunID]model.DeviceCommand, commandID string) model.RunID {
    for run, command := range commands {
        if command.ID == commandID {
            return run
        }
    }
    return ""
}

func (c *Controller) Brake(run model.RunID, commandID string, epoch uint64, at time.Time) (model.DeviceReceipt, error) {
    c.mu.Lock()
    defer c.mu.Unlock()
    command, ok := c.active[run]
    if !ok || command.ID != commandID || command.Epoch != epoch {
        return model.DeviceReceipt{}, errors.New("stale brake command")
    }
    receipt := model.DeviceReceipt{CommandID: commandID, Epoch: epoch, Velocity: 0, Confirmed: true, RecordedAt: at.UTC()}
    c.receipts[run] = receipt
    c.velocities[run] = 0
    return receipt, nil
}

func (c *Controller) Stopped(run model.RunID, commandID string, epoch uint64) bool {
    c.mu.Lock()
    defer c.mu.Unlock()
    receipt, ok := c.receipts[run]
    return ok && receipt.CommandID == commandID && receipt.Epoch == epoch && receipt.Confirmed && c.velocities[run] == 0
}

func (c *Controller) Velocity(run model.RunID) float64 {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.velocities[run]
}

func (c *Controller) Commands(run model.RunID) model.DeviceCommand {
    c.mu.Lock()
    defer c.mu.Unlock()
    return c.active[run]
}

// Receipt 返回该运行最近一次刹停回执及是否存在。
func (c *Controller) Receipt(run model.RunID) (model.DeviceReceipt, bool) {
    c.mu.Lock()
    defer c.mu.Unlock()
    receipt, ok := c.receipts[run]
    return receipt, ok
}
