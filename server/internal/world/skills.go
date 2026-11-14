package world

import (
	"fmt"
	"sort"
	"time"

	"github.com/Spelunkern/Shaiya-GX/server/internal/db"
	"github.com/Spelunkern/Shaiya-GX/server/internal/packet"
)

const (
	skillBarCount     = 5
	skillBarSlotCount = 10
	quickSlotSkillBag = 100
	maxQuickSlots     = 128

	mountSlot      = 13
	mountItemType  = 42
	unmountedShape = 0x00
)

type pendingMountCast struct {
	readyAt  time.Time
	mountUID int64
}

func buildCharacterSkills(skills []db.CharacterSkill) []byte {
	w := packet.New(packet.CharacterSkills)
	skills = skills[:min(len(skills), 255)]
	w.U8(uint8(len(skills)))
	for _, s := range skills {
		w.U16(s.SkillID)
		w.U8(s.Level)
		w.U8(s.Number)
		w.I32(int32(s.Delay))
	}
	return w.MustFinish()
}

func isValidQuickSlot(q db.QuickSlot) bool {
	return q.QuickBar < skillBarCount && q.QuickSlot < skillBarSlotCount &&
		((q.Bag <= InventoryLastBag && q.Number < 24) || (q.Bag == quickSlotSkillBag && q.Number > 0))
}

func buildCharacterSkillBar(slots []db.QuickSlot) []byte {
	var valid []db.QuickSlot
	for _, q := range slots {
		if isValidQuickSlot(q) && len(valid) < maxQuickSlots {
			valid = append(valid, q)
		}
	}
	w := packet.New(packet.CharacterSkillBar)
	w.U8(uint8(len(valid)))
	for _, q := range valid {
		w.U8(q.QuickBar)
		w.U8(q.QuickSlot)
		w.U8(q.Bag)
		w.U16(q.Number)
	}
	return w.MustFinish()
}

// parseSkillBar (0x010B): count, then bar, slot, bag, number:u16.
func parseSkillBar(p []byte) []db.QuickSlot {
	if len(p) < 3 {
		return nil
	}
	var out []db.QuickSlot
	off := 3
	for range min(int(p[2]), maxQuickSlots) {
		if len(p) < off+5 {
			break
		}
		n, _ := packet.U16(p[off+3:])
		q := db.QuickSlot{QuickBar: p[off], QuickSlot: p[off+1], Bag: p[off+2], Number: n}
		if isValidQuickSlot(q) {
			out = append(out, q)
		}
		off += 5
	}
	return out
}

func (s *Session) handleSkillBar(p []byte) {
	if s.char == nil {
		return
	}
	s.quickSlots = parseSkillBar(p)
	if err := db.SaveQuickSlots(s.char.ID, s.quickSlots); err != nil {
		s.log.Printf("GX Server World failed to save quick slots: %v", err)
		return
	}
	s.log.Printf("GX Server World saved %d quick slots", len(s.quickSlots))
}

// buildLearnSkillError (0x0209): reason, skill id, level.
func buildLearnSkillError(reason uint8, skillID uint16, level uint8) []byte {
	w := packet.New(packet.LearnNewSkill)
	w.U8(reason)
	w.U8(0)
	w.U16(skillID)
	w.U8(level)
	return w.MustFinish()
}

// skillCountryAllows is EnableLearnSkill's Country byte by family (0 human,
// 1 elf, 2 vail, 3 deatheater).
func skillCountryAllows(country, family uint8) bool {
	switch country {
	case 0:
		return family == 0
	case 1:
		return family == 1
	case 2:
		return family == 0 || family == 1
	case 3:
		return family == 3
	case 4:
		return family == 2
	case 5:
		return family == 2 || family == 3
	}
	return true
}

// skillGrowAllows: easy mode learns only Grow 0 skills, normal up to 1,
// hard all but 3.
func skillGrowAllows(grow, mode uint8) bool {
	switch mode {
	case 0:
		return grow == 0
	case 1:
		return grow <= 1
	case 2:
		return grow != 3
	}
	return true
}

func buildLearnSkillResult(ok bool, number uint8, skillID uint16, level uint8) []byte {
	w := packet.New(packet.LearnNewSkill)
	w.U8(resultByte(ok))
	w.U8(number)
	w.U16(skillID)
	w.U8(level)
	return w.MustFinish()
}

// handleLearnSkill (0x0209): skill id:u16, level.
func (s *Session) handleLearnSkill(p []byte) {
	if len(p) < 5 || s.char == nil {
		return
	}
	ch := s.char
	skillID, _ := packet.U16(p[2:])
	level := p[4]
	// CUser::LearnSkill / EnableLearnSkill (0x004842e0 / 0x00484080):
	// levels 1-3; a new skill starts at level 1 and a known one goes to
	// any higher level; then skill points (error 1), level (2), and the
	// country, job, growth mode and TypeShow (3). No prerequisite skill.
	reject := func(code uint8, why string) {
		s.log.Printf("GX Server World rejected learn skill skill_id=%d level=%d: %s", skillID, level, why)
		s.send(buildLearnSkillError(code, skillID, level), "0x0209 learn skill rejected")
	}
	def := db.MustDefs().Skill(skillID, level)
	if level < 1 || level > 3 || def == nil {
		reject(3, "no such skill/level")
		return
	}
	existing := -1
	for i, k := range s.skills {
		if k.SkillID == skillID {
			existing = i
		}
	}
	if existing >= 0 {
		if s.skills[existing].Level >= level {
			reject(3, "level already known")
			return
		}
	} else if level != 1 {
		return
	}
	cost := uint16(def.SkillPointCost)
	switch {
	case ch.SkillPoint < cost:
		reject(1, "not enough skill points")
		return
	case ch.Level < def.ReqLevel:
		reject(2, "level too low")
		return
	case !skillCountryAllows(def.Country, ch.Race):
		s.log.Printf("GX Server World rejected learn skill skill_id=%d: country %d", skillID, def.Country)
		return
	case int(ch.Class) >= len(def.ClassEligible) || !def.ClassEligible[ch.Class] || !skillGrowAllows(def.Grow, ch.Mode) || def.TypeShow == 0:
		reject(3, "job, growth mode or hidden skill")
		return
	}
	var learned db.CharacterSkill
	var err error
	if existing >= 0 {
		learned = s.skills[existing]
		learned.Level, learned.Delay = level, 0
		err = db.SaveCharacterSkill(ch.ID, learned)
		s.skills[existing] = learned
	} else {
		number := uint8(0)
		for _, k := range s.skills {
			number = max(number, k.Number)
		}
		learned = db.CharacterSkill{SkillID: skillID, Level: level, Number: number + 1}
		err = db.AddCharacterSkill(ch.ID, learned)
		s.skills = append(s.skills, learned)
		sort.Slice(s.skills, func(i, j int) bool { return s.skills[i].Number < s.skills[j].Number })
	}
	if err != nil {
		s.log.Printf("GX Server World failed to save learned skill: %v", err)
		reject(3, "database error")
		return
	}
	ch.SkillPoint -= cost
	s.persist()
	s.log.Printf("GX Server World learned skill number=%d skill_id=%d level=%d remaining_skill_point=%d", learned.Number, skillID, level, ch.SkillPoint)
	s.send(buildLearnSkillResult(true, learned.Number, skillID, level), "0x0209 learn skill ok")
}

// handleSetStatPoint (0x0208): six u16 deltas str, dex, rec, int, wis, luc.
func (s *Session) handleSetStatPoint(p []byte) {
	if len(p) < 14 || s.char == nil {
		return
	}
	ch := s.char
	var d [6]uint16
	total := uint32(0)
	for i := range d {
		d[i], _ = packet.U16(p[2+2*i:])
		total += uint32(d[i])
	}
	if total == 0 || total > uint32(ch.StatPoint) {
		s.log.Printf("GX Server World rejected stat points total=%d available=%d", total, ch.StatPoint)
		return
	}
	add := func(v *uint16, n uint16) { *v = uint16(min(uint32(*v)+uint32(n), 65535)) }
	add(&ch.Str, d[0])
	add(&ch.Dex, d[1])
	add(&ch.Rec, d[2])
	add(&ch.Int, d[3])
	add(&ch.Wis, d[4])
	add(&ch.Luc, d[5])
	ch.StatPoint -= uint16(total)
	s.persist()
	stats := s.stats()
	w := packet.New(packet.SetStatPoint)
	for _, v := range d {
		w.U16(v)
	}
	s.send(w.MustFinish(), "0x0208 stat point applied")
	if d[2] > 0 {
		s.send(buildMaxEnergy(ch, 0, stats.MaxHP), "0x050B max hp")
	}
	if d[1] > 0 {
		s.send(buildMaxEnergy(ch, 1, stats.MaxSP), "0x050B max sp")
	}
	if d[4] > 0 {
		s.send(buildMaxEnergy(ch, 2, stats.MaxMP), "0x050B max mp")
	}
	s.log.Printf("GX Server World applied stat points character_id=%d deltas=%v remaining=%d", ch.ID, d, ch.StatPoint)
}

// handleUseVehicle (0x0216) toggles the mount equipped in slot 13.
func (s *Session) handleUseVehicle() {
	if s.char == nil {
		return
	}
	message := s.startMountAction(s.mountCastDuration, "0x0216 use vehicle", true)
	if message != "" {
		s.commandReply(message)
	}
}

// startMountAction is shared by the vehicle key and the admin chat command.
// The server owns the cast deadline and the item-specific reuse timer.
func (s *Session) startMountAction(castDuration time.Duration, label string, acknowledge bool) string {
	if s.char == nil {
		return ""
	}
	if s.mounted {
		reuseSeconds := int64(s.mountedReuse / time.Second)
		s.setMountState(false, unmountedShape, label)
		if reuseSeconds > 0 {
			return fmt.Sprintf("Desmontado. Esta montura queda en recarga durante %d s.", reuseSeconds)
		}
		return "Desmontado."
	}
	if s.mountCast != nil {
		if acknowledge {
			s.send(buildUseVehicleReply(false, false), label+" rejected during mount cast")
		}
		return "Ya hay un casteo de montura en curso."
	}
	mount, def, ok := s.equippedMountDefinition()
	if !ok {
		if acknowledge {
			s.send(buildUseVehicleReply(false, false), label+" rejected: no mount equipped")
		}
		return "Equipa una montura en el slot 13 antes de invocarla."
	}
	if remaining := s.mountCooldownRemaining(time.Now()); remaining > 0 {
		if acknowledge {
			s.send(buildUseVehicleReply(false, false, durationMilliseconds(remaining)), label+" rejected: mount cooldown")
		}
		seconds := int64((remaining + time.Second - 1) / time.Second)
		return fmt.Sprintf("Esta montura sigue en recarga: %d s.", seconds)
	}
	now := time.Now()
	if s.cast != nil || s.actionLockUntil.After(now) || s.actionBlocked() {
		if acknowledge {
			s.send(buildUseVehicleReply(false, false), label+" rejected during another action")
		}
		return "No se puede iniciar la montura mientras hay otra accion en curso."
	}
	shape := mountShape(def.Range)
	if castDuration <= 0 {
		s.autoAttack, s.autoAttackTarget, s.autoAttackUser = false, 0, false
		s.send(packet.New(packet.AutoAttackStop).MustFinish(), "0x0212 auto attack stop (mount)")
		s.setMountState(true, shape, label)
		return "Montura activada."
	}
	s.autoAttack = false
	s.mountCast = &pendingMountCast{readyAt: now.Add(castDuration), mountUID: mount.UID}
	s.send(packet.New(packet.AutoAttackStop).MustFinish(), "0x0212 auto attack stop (mount cast)")
	s.broadcastNear(buildMountCastStarted(s.char.ID, uint32(castDuration/time.Second)), "0x0510 mount cast started", true)
	if acknowledge {
		// Confirm the request without making the client think it is mounted.
		s.send(buildUseVehicleReply(true, false), label+" mount cast accepted")
	}
	s.log.Printf("GX Server World mount cast started character_id=%d item_uid=%d cast_time=%s reuse_seconds=%d", s.char.ID, mount.UID, castDuration, def.ReqIg)
	return fmt.Sprintf("Casteo de montura iniciado durante %d s.", int64(castDuration/time.Second))
}

func buildUseVehicleReply(ok, on bool, cooldownMS ...uint32) []byte {
	w := packet.New(packet.UseVehicle)
	w.Bool(ok)
	w.Bool(on)
	if len(cooldownMS) != 0 {
		w.U32(cooldownMS[0])
	}
	return w.MustFinish()
}

func durationMilliseconds(d time.Duration) uint32 {
	if d <= 0 {
		return 0
	}
	return uint32(d / time.Millisecond)
}

func buildMountCastStarted(characterID uint32, seconds uint32) []byte {
	w := packet.New(packet.SkillCastStarted)
	w.U32(characterID)
	w.U32(characterID)
	w.U16(43) // Detection: visual-only cast animation in the bundled EP5 Skill.SData.
	w.U8(1)
	w.U32(seconds) // GX extension keeps the animation aligned with the server deadline.
	return w.MustFinish()
}

func (s *Session) equippedMountShape() (uint8, bool) {
	_, d, ok := s.equippedMountDefinition()
	if !ok {
		return 0, false
	}
	return mountShape(d.Range), true
}

func mountShape(rangeValue int16) uint8 {
	r := uint8(max(rangeValue, 0))
	if r < 2 {
		return r * 2
	}
	return r + 7
}

func (s *Session) equippedMountDefinition() (*db.Item, *db.ItemDef, bool) {
	m := equippedItem(s.items, mountSlot)
	if m == nil || m.Type != mountItemType {
		return nil, nil, false
	}
	d := db.MustDefs().Item(m.Type, m.TypeID)
	if d == nil {
		return nil, nil, false
	}
	return m, d, true
}

func (s *Session) mountCooldownRemaining(now time.Time) time.Duration {
	if !s.mountCooldownUntil.After(now) {
		s.mountCooldownUntil = time.Time{}
		return 0
	}
	return s.mountCooldownUntil.Sub(now)
}

func (s *Session) startMountedItemCooldown(now time.Time) time.Duration {
	remaining := s.mountCooldownRemaining(now)
	if remaining > 0 {
		s.mountedMountUID, s.mountedReuse = 0, 0
		return remaining
	}
	duration := s.mountedReuse
	if s.mountedReuse > 0 {
		s.mountCooldownUntil = now.Add(s.mountedReuse)
		s.log.Printf("GX Server World mount-slot cooldown started character_id=%d item_uid=%d duration=%s", s.char.ID, s.mountedMountUID, s.mountedReuse)
	}
	s.mountedMountUID, s.mountedReuse = 0, 0
	return duration
}

func (s *Session) setMountState(mounted bool, shape uint8, label string) {
	if s.char == nil {
		return
	}
	var cooldown time.Duration
	if !mounted && s.mounted {
		cooldown = s.startMountedItemCooldown(time.Now())
	}
	if mounted {
		if item, def, ok := s.equippedMountDefinition(); ok {
			s.mountedMountUID = item.UID
			s.mountedReuse = time.Duration(def.ReqIg) * time.Second
		}
	}
	s.mounted = mounted
	s.send(buildUseVehicleReply(true, mounted, durationMilliseconds(cooldown)), label)
	if s.player != nil {
		s.player.setMountShape(shape)
	}
	w := packet.New(packet.CharacterMountShape)
	w.U32(s.char.ID)
	w.U8(shape)
	frame := w.MustFinish()
	s.send(frame, "0x051D character mount shape")
	s.sendToViewers(frame, "0x051D character mount shape")
	s.log.Printf("GX Server World character_id=%d mounted=%t", s.char.ID, mounted)
}

func (s *Session) processPendingMountCast(now time.Time) {
	pending := s.mountCast
	if pending == nil {
		return
	}
	if s.dead || s.char == nil {
		s.mountCast = nil
		return
	}
	if pending.readyAt.After(now) {
		return
	}
	s.mountCast = nil
	item, def, ok := s.equippedMountDefinition()
	if !ok || item.UID != pending.mountUID {
		s.send(buildUseVehicleReply(false, false), "0x0216 mount cast failed: mount changed")
		s.log.Printf("GX Server World mount cast failed character_id=%d reason=mount_changed", s.char.ID)
		s.commandReply("Casteo cancelado: la montura equipada cambio durante la preparacion.")
		return
	}
	shape := mountShape(def.Range)
	s.setMountState(true, shape, "0x0216 mount cast complete")
}

// dismount is StatusCastingStop's vehicle cancel: a hit takes the rider
// off its mount (shape 0 to the viewers).
func (s *Session) dismount() {
	if !s.mounted || s.char == nil {
		return
	}
	cooldown := s.startMountedItemCooldown(time.Now())
	s.mounted = false
	s.send(buildUseVehicleReply(true, false, durationMilliseconds(cooldown)), "0x0216 dismounted by a hit")
	if s.player != nil {
		s.player.setMountShape(unmountedShape)
	}
	w := packet.New(packet.CharacterMountShape)
	w.U32(s.char.ID)
	w.U8(unmountedShape)
	frame := w.MustFinish()
	s.send(frame, "0x051D dismounted by a hit")
	s.sendToViewers(frame, "0x051D dismounted by a hit")
}
