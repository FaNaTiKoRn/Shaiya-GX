package world

import (
	"strconv"
	"strings"
	"time"
)

// parseMountCommand returns the mount cast time, whether this is a /mount
// command, and whether its optional whole-seconds argument is valid. Omitting
// the argument keeps the command instant for compatibility.
func parseMountCommand(message string) (time.Duration, bool, bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "/mount") {
		return 0, false, true
	}
	if len(fields) > 2 {
		return 0, true, false
	}
	var seconds uint64
	if len(fields) == 2 {
		value, err := strconv.ParseUint(fields[1], 10, 32)
		if err != nil {
			return 0, true, false
		}
		seconds = value
	}
	return time.Duration(seconds) * time.Second, true, true
}

func (s *Session) commandReply(message string) {
	if s.char != nil {
		s.send(buildChatNormal(s.char.ID, message), "0x1101 admin command result")
	}
}

func (s *Session) executeMountCommand(castDuration time.Duration) {
	if s.char == nil {
		return
	}
	if !s.isAdmin {
		s.log.Printf("GX Server World rejected /mount from non-admin account_id=%d character_id=%d", s.accountID, s.char.ID)
		s.commandReply("/mount es un comando de prueba reservado para cuentas admin.")
		return
	}

	s.mountCastDuration = castDuration
	message := s.startMountAction(castDuration, "chat /mount", false)
	s.commandReply(message)
	s.log.Printf("GX Server World /mount character_id=%d cast_time=%s", s.char.ID, castDuration)
}
