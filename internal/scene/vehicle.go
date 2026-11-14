package scene

import (
	"fmt"
	"path"
	"strings"

	"github.com/Spelunkern/Shaiya-GX/internal/data/model"
	"github.com/Spelunkern/Shaiya-GX/internal/render"
)

// Vehicle uses the native race-specific MON records and the same skinning
// pipeline as creatures. Seat bone indices are the original mount bindings
// recovered by Phoenix; higher variants use a height-based fallback.
type Vehicle struct {
	*CreatureActor
	seatBone int
	board    bool
	moving   bool
	backward bool
	jumping  bool
}

// These native MON records are the standing board variants.
func VehicleAlternate(shape uint8) bool { return shape == 6 || shape == 9 || shape == 11 }

var vehicleSeatBones = [4][6]int{
	{25, 25, 25, 25, 30, 23}, {20, 32, 20, 32, 28, 34},
	{20, 26, 20, 26, 21, 38}, {34, 18, 34, 18, 21, 20},
}

func (s *Assets) BuildVehicle(race, shape uint8) (*Vehicle, error) {
	if race >= 4 {
		return nil, fmt.Errorf("invalid mount race %d", race)
	}
	if s.vehicles[race] == nil {
		folders := [4]string{"Hu", "El", "Vi", "De"}
		b, err := s.Arc.ReadFile("Vehicle/Vehicle_" + folders[race] + "_01.MON")
		if err != nil {
			return nil, err
		}
		s.vehicles[race], err = model.ParseMON(b)
		if err != nil {
			return nil, err
		}
	}
	rows := s.vehicles[race]
	if int(shape) >= len(rows) {
		return nil, fmt.Errorf("mount shape %d out of range", shape)
	}
	actor, err := s.buildCreature("Vehicle", rows[shape], rows[shape].Name)
	if err != nil {
		return nil, err
	}
	if err := actor.SetAction(8); err != nil {
		return nil, err
	}
	bone := -1
	if shape < 6 {
		bone = vehicleSeatBones[race][shape]
	} else if VehicleAlternate(shape) {
		bone = 1
	}
	return &Vehicle{CreatureActor: actor, seatBone: bone, board: VehicleAlternate(shape)}, nil
}

func (v *Vehicle) SetMoving(moving bool) { v.SetMovement(moving, false) }

// SetMovement selects walk for reverse input and run for forward input.
func (v *Vehicle) SetMovement(moving, backward bool) {
	if v == nil || v.CreatureActor == nil {
		return
	}
	v.moving, v.backward = moving, backward && moving
	if v.jumping {
		return
	}
	action := 8
	if moving {
		action = 1 // run
		if v.backward {
			action = 0 // native mount walk
		}
	}
	if err := v.SetAction(action); err != nil {
		v.assets.Missing[err.Error()] = true
	}
}

// SetJumping starts the mount's own ANI when that vehicle model has one.
// The rider remains on its regular riding pose while the mount leaves ground.
func (v *Vehicle) SetJumping() {
	if v == nil || v.CreatureActor == nil || v.jumping || len(v.visual.Animations) < 2 {
		return
	}
	base := strings.TrimSuffix(path.Base(v.visual.Animations[1]), path.Ext(v.visual.Animations[1]))
	base = strings.TrimSuffix(base, "_run")
	file := "Vehicle/ANI/" + base + "_jump.ANI"
	clip, err := v.assets.Clip(file)
	if err != nil {
		v.assets.Missing[err.Error()] = true
		return
	}
	v.clip, v.clipName, v.Time, v.action = clip, file, 0, -2
	v.cueTime = 0
	v.jumping = true
}

func (v *Vehicle) Update(dt float32) {
	if v == nil || v.CreatureActor == nil {
		return
	}
	v.CreatureActor.Update(dt)
	if v.jumping && v.clip != nil && v.clip.Duration() > 0 && v.Time >= v.clip.Duration() {
		v.jumping = false
		v.SetMovement(v.moving, v.backward)
	}
}

// Seat uses the same cached pose as drawing, including before the first draw.
func (v *Vehicle) Seat() render.V3 {
	if v == nil {
		return render.V3{}
	}
	finals := v.finals()
	if v.seatBone >= 0 && v.seatBone < len(finals) {
		m := finals[v.seatBone].Mul(v.World)
		return render.V3{m[12], m[13], m[14]}
	}
	m := v.World
	if v.board {
		return render.V3{m[12], m[13] + .877, m[14]}
	}
	return render.V3{m[12], m[13] + max(.1, v.visual.Height*.7), m[14]}
}
