package world

import "time"

// startSkillActionRecovery uses durations derived from the corresponding
// client Skill.SData and character action ANI files. It is separate from the
// skill cooldown: it only prevents movement/actions through the release clip.
func (s *Session) startSkillActionRecovery(id uint16, level uint8) {
	millis := skillRecoveryMillis[skillRecoveryKey{id: id, level: level}]
	if millis == 0 {
		s.log.Printf("GX Server World missing skill action recovery data skill_id=%d level=%d", id, level)
		return
	}
	until := time.Now().Add(time.Duration(millis) * time.Millisecond)
	if until.After(s.skillActionUntil) {
		s.skillActionUntil = until
	}
}
