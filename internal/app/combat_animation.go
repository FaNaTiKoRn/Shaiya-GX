package app

import (
	"github.com/Spelunkern/Shaiya-GX/internal/data/sdata"

	"github.com/Spelunkern/Shaiya-GX/internal/proto"
	"github.com/Spelunkern/Shaiya-GX/internal/render"
	"github.com/Spelunkern/Shaiya-GX/internal/scene"
)

func (a *App) playerActor(id uint32) *scene.Character {
	if a.wv == nil {
		return nil
	}
	if id == a.state.SelfID {
		return a.wv.self
	}
	if actor := a.wv.actors[id]; actor != nil {
		return actor.character
	}
	return nil
}

func (a *App) playerWeapon(id uint32) uint8 {
	if id == a.state.SelfID {
		return a.state.Inventory[[2]uint8{0, 5}].Type
	}
	if e := a.state.Entities[id]; e != nil && e.Shape != nil {
		return e.Shape.Equipment[5][0]
	}
	return 0
}

// weaponSwingDuration mirrors the server's AttackTime table. CharacterSpeed
// already carries the server-resolved index, including attack-speed effects.
func (a *App) weaponSwingDuration() float64 {
	if a.state == nil {
		return 0
	}
	weapon := a.state.Inventory[[2]uint8{0, 5}]
	def := a.itemsItem(weapon.Type, weapon.TypeID)
	if def == nil {
		return 0
	}
	attackTime := int(min(def.AttackTime, 10))
	if a.state.Self.Speed != nil && a.state.Self.Speed.Attack <= 9 {
		attackTime = int(a.state.Self.Speed.Attack) + 1
	}
	delays := [...]float64{0, 4, 3, 2.75, 2.5, 2.25, 2, 1.75, 1.5, 1.25, 1}
	if attackTime < 1 || attackTime >= len(delays) {
		return 0
	}
	return delays[attackTime]
}

// confirmedAnimation presents network outcomes; it never changes health.
func (a *App) confirmedAnimation(p []byte) {
	op := proto.Op(p)
	v := a.wv
	if v == nil {
		return
	}
	switch op {
	case proto.CharacterMotion:
		m := a.state.LastMotion
		if m.ID == a.state.SelfID && (m.Motion == proto.MotionSitDown || m.Motion == proto.MotionStandUp) {
			v.sitPending = false
		}
		if m.Motion == proto.MotionSitDown || m.Motion == proto.MotionStandUp {
			if actor := a.playerActor(m.ID); actor != nil {
				action := 10
				if m.Motion == proto.MotionStandUp {
					action = 11
				}
				actor.PlayAction(action, false)
			}
		}
	case proto.SkillCastStarted:
		r := a.state.LastCast
		a.loadSkills()
		d := a.skillDefs[sdata.SkillKey{ID: r.ID, Level: r.Level}]
		seconds := float32(d.ReadyTime) * .25
		if r.MountCastSeconds > 0 {
			seconds = float32(r.MountCastSeconds)
		}
		// The server may require preparation even when local SData says
		// instant. Its cast-start supersedes the predicted release and clock.
		if r.Source == a.state.SelfID {
			v.requestedSkill = nil
			v.movementActionID = 0
			v.movementActionUntil = 0
			if seconds > 0 {
				v.skillCastUntil = a.clock + float64(seconds)
			}
		}

		if actor := a.playerActor(r.Source); actor != nil {
			a.faceSkillTarget(r.Source, r.Target)
			if actor.PlayCast(scene.SkillCastAction(d.Animation, d.Effect), seconds) && r.Source == a.state.SelfID {
				v.movementActionID = actor.ActionID()
			}
		}
	case proto.SkillUse, proto.SkillUseNoTarget, proto.SkillUserDrain, proto.SkillMobDrain:
		r := a.state.LastSkill
		predicted := a.matchingRequestedSkill(r)
		if r.Source == a.state.SelfID {
			v.requestedSkill = nil
			v.skillCastUntil = 0
			if r.Result <= 4 && !(r.Result == 3 && r.Target == 0) {
				until := a.clock + skillRecoverySeconds(r.ID, r.Level)
				v.movementActionUntil = max(v.movementActionUntil, until)
			}
		}
		if predicted {
			if r.Result > 4 || r.Result == 3 && r.Target == 0 {
				a.wv.self.ReleaseAction()
				v.movementActionID = 0
			} else {
				return
			}
		}
		if actor := a.playerActor(r.Source); actor != nil {
			actor.CancelCast()
		}
		if r.Result > 4 || r.Result == 3 && r.Target == 0 {
			return
		}
		if actor := a.playerActor(r.Source); actor != nil {
			a.loadSkills()
			d, ok := a.skillDefs[sdata.SkillKey{ID: r.ID, Level: r.Level}]
			if !ok {
				return
			}
			weapon := a.playerWeapon(r.Source)
			a.faceSkillTarget(r.Source, r.Target)
			a.attackVariant++
			if actor.PlayAction(scene.SkillAction(d.Animation, d.Attack, d.Effect, weapon, a.attackVariant), false) && r.Source == a.state.SelfID {
				v.movementActionID = actor.ActionID()
			}
		}
	case proto.CharacterMove:
		if actor := a.playerActor(proto.NewReader(p).U32()); actor != nil {
			if actor != v.self || !actor.CastActive() {
				actor.CancelCast()
			}
			if move, err := proto.DecodeUserMove(p); err == nil && move.Motion&0x3f != 0 {
				cancelGesture(actor)
			}
		}
	case proto.CharacterMobAutoAttack, proto.CharacterUserAutoAttack, proto.MobAttackCharacter:
		result := a.state.LastAttack
		if actor := a.playerActor(result.Source); actor != nil {
			typ := a.playerWeapon(result.Source)
			if result.Source == a.state.SelfID {
				if target := a.state.Entities[result.Target]; target != nil {
					dir := (render.V3{target.X, 0, target.Z}).Sub(render.V3{v.pos[0], 0, v.pos[2]}).Norm()
					if dir.Len() > 0 {
						v.faceCombatTarget(dir)
					}
				}
			}
			a.attackVariant++
			if actor.PlayAction(scene.AttackAction(typ, a.attackVariant), false) {
				if result.Source == a.state.SelfID {
					v.movementActionID = actor.ActionID()
					v.movementActionUntil = max(v.movementActionUntil, a.clock+a.weaponSwingDuration())
				}
				a.queueAttackSounds(result, typ, actor)
			}
		}
		if mob := v.mobs[result.Source]; mob != nil && mob.actor != nil {
			mob.attackTarget = result.Target
			mob.actor.PlayOnce(mob.actor.AttackAction(0))
		}
		if result.HP > 0 {
			if mob := v.mobs[result.Target]; mob != nil && mob.actor != nil {
				mob.actor.PlayOnce(7)
			}
		}
		if result.Source == a.state.SelfID || result.Target == a.state.SelfID || result.Target == a.state.Target {
			a.showCombatNumber(result.Target, result.HP, result.Result == 1, result.Result == 3, false)
		}
	case proto.CharacterDeath:
		id := proto.NewReader(p).U32()
		if c := a.playerActor(id); c != nil {
			c.PlayAction(9, true)
		}
	case proto.CharacterResurrect:
		id := proto.NewReader(p).U32()
		if c := a.playerActor(id); c != nil {
			c.ReleaseAction()
		}
	}
}

func (a *App) faceSkillTarget(source, target uint32) {
	if a.wv == nil || source == target {
		return
	}
	goal, ok := a.effectPosition(target)
	if !ok {
		return
	}
	if source == a.state.SelfID {
		dir := goal.Sub(a.wv.pos)
		dir[1] = 0
		if dir.Len() > .001 {
			a.wv.faceCombatTarget(dir.Norm())
		}
	} else if actor := a.wv.actors[source]; actor != nil && actor.character != nil {
		pos := a.visualEntityPosition(actor.entity)
		dir := goal.Sub(pos)
		dir[1] = 0
		if dir.Len() > .001 {
			actor.facing = dir.Norm()
		}
	}
}

// Facing is predicted on input, before any attack/cast confirmation. Publish
// the stationary direction before the action so peers see the same heading.
func (a *App) faceRequestedTarget(target uint32) bool {
	if a.wv == nil {
		return true
	}
	previous := a.wv.facing
	a.faceSkillTarget(a.state.SelfID, target)
	if a.world != nil && previous.Sub(a.wv.facing).Len() > .0001 {
		if err := a.world.Send(a.wv.movePacket(0)); err != nil {
			a.backToLogin("Unable to turn toward target.")
			return false
		}
	}
	return true
}
