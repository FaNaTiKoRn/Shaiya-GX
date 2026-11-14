package app

import (
	"strings"

	"github.com/Spelunkern/Shaiya-GX/internal/proto"
)

type vehicleView struct {
	pending             bool
	deadline            float64
	mounted             bool
	mountCooldownUntil  float64
	mountCooldownLength float64
}

func (a *App) requestVehicle() {
	if a.state == nil || a.world == nil || a.state.Self.Dead || a.vehicle.pending || a.inventory.pending || a.shop.pending {
		return
	}
	if !a.state.Self.Mounted {
		item, ok := a.state.Inventory[[2]uint8{0, 13}]
		if !ok || item.Type != 42 {
			a.questMessage("Equip a mount first.")
			return
		}
	}
	if err := a.world.Send(proto.UseVehiclePacket()); err != nil {
		a.backToLogin("Unable to use mount.")
		return
	}
	a.vehicle.pending, a.vehicle.deadline = true, a.clock+10
}
func (a *App) vehicleCommand(text string) bool {
	if !strings.EqualFold(strings.TrimSpace(text), "/mount") {
		return false
	}
	a.requestVehicle()
	return true
}
func (a *App) confirmedVehicle(p []byte) {
	if proto.Op(p) != proto.UseVehicle {
		return
	}
	r, err := proto.DecodeVehicleResult(p)
	if err != nil {
		return
	}
	wasPending := a.vehicle.pending
	a.vehicle.pending = false
	if r.Result == 0 {
		if wasPending {
			a.questMessage("Mount request was refused.")
		}
		if r.CooldownMS > 0 {
			a.captureMountCooldownData()
			a.startMountCooldownFor(float64(r.CooldownMS) / 1000)
		}
		return
	}
	if r.On != 0 {
		a.vehicle.mounted = true
		a.captureMountCooldownData()
	} else {
		if a.wv != nil {
			a.wv.mountJumping = false
		}
		if r.CooldownMS > 0 {
			a.captureMountCooldownData()
			a.startMountCooldownFor(float64(r.CooldownMS) / 1000)
		} else if a.vehicle.mounted {
			a.startMountCooldown()
		}
	}
}

func (a *App) confirmedMountShape(p []byte) {
	if proto.Op(p) != proto.CharacterMountShape || a.state == nil {
		return
	}
	r, err := proto.DecodeMountShape(p)
	if err != nil || r.ID != a.state.SelfID {
		return
	}
	if r.Shape != 0 {
		a.vehicle.mounted = true
		a.captureMountCooldownData()
		return
	}
	if a.wv != nil {
		a.wv.mountJumping = false
	}
	if a.vehicle.mounted {
		a.startMountCooldown()
	}
}

func (a *App) captureMountCooldownData() {
	if a.state == nil {
		return
	}
	item, ok := a.state.Inventory[[2]uint8{0, 13}]
	if !ok || item.Type != 42 {
		return
	}
	def := a.itemsItem(item.Type, item.TypeID)
	if def == nil {
		return
	}
	// For Type 42 the GX server interprets the item definition's ReqIg as
	// seconds; consumables continue to use their shared ReqIg group table.
	a.vehicle.mountCooldownLength = float64(def.ReqIg)
}

func (a *App) startMountCooldown() {
	a.startMountCooldownFor(a.vehicle.mountCooldownLength)
	a.vehicle.mounted = false
}

func (a *App) startMountCooldownFor(seconds float64) {
	if a.vehicle.mountCooldownUntil > a.clock {
		a.vehicle.mounted = false
		return
	}
	a.vehicle.mountCooldownLength = seconds
	if seconds > 0 {
		a.vehicle.mountCooldownUntil = a.clock + seconds
	}
	a.vehicle.mounted = false
}

func (a *App) mountCooldown(it proto.InventoryItem, key [2]uint8) float64 {
	if key != ([2]uint8{0, 13}) || it.Type != 42 {
		return 0
	}
	return max(0, a.vehicle.mountCooldownUntil-a.clock)
}

func (a *App) mountCooldownDuration(it proto.InventoryItem, key [2]uint8) float64 {
	if a.mountCooldown(it, key) > 0 {
		return a.vehicle.mountCooldownLength
	}
	return 0
}
