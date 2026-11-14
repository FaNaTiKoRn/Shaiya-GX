package world

import (
	"strconv"
	"strings"

	"github.com/Spelunkern/Shaiya-GX/server/internal/db"
)

// parseGetItemCommand reads the admin chat command /getitem <type> <typeID>.
func parseGetItemCommand(message string) (uint8, uint8, bool, bool) {
	fields := strings.Fields(message)
	if len(fields) == 0 || !strings.EqualFold(fields[0], "/getitem") {
		return 0, 0, false, true
	}
	if len(fields) != 3 {
		return 0, 0, true, false
	}
	typeValue, err := strconv.ParseUint(fields[1], 10, 8)
	if err != nil {
		return 0, 0, true, false
	}
	idValue, err := strconv.ParseUint(fields[2], 10, 8)
	if err != nil {
		return 0, 0, true, false
	}
	return uint8(typeValue), uint8(idValue), true, true
}

func (s *Session) executeGetItemCommand(itemType, typeID uint8) {
	if s.char == nil {
		return
	}
	if !s.isAdmin {
		s.log.Printf("GX Server World rejected /getitem from non-admin account_id=%d character_id=%d", s.accountID, s.char.ID)
		s.commandReply("/getitem es un comando de prueba reservado para cuentas admin.")
		return
	}
	if db.MustDefs().Item(itemType, typeID) == nil {
		s.log.Printf("GX Server World /getitem definition missing type=%d type_id=%d", itemType, typeID)
		s.commandReply("No existe ese item en los datos del servidor.")
		return
	}
	free := freeInventorySlots(s.items)
	if len(free) == 0 {
		s.log.Printf("GX Server World /getitem inventory full character_id=%d type=%d type_id=%d", s.char.ID, itemType, typeID)
		s.commandReply("No hay espacio en inventario")
		return
	}
	item := db.Item{Bag: free[0][0], Slot: free[0][1], Type: itemType, TypeID: typeID, Count: 1}
	if err := db.AddCharacterItem(s.char.ID, &item); err != nil {
		s.log.Printf("GX Server World /getitem failed character_id=%d type=%d type_id=%d: %v", s.char.ID, itemType, typeID, err)
		s.commandReply("No pude agregar el item; revisa la conexion con la base de datos.")
		return
	}
	s.items = append(s.items, item)
	s.send(buildAddItem(&item), "0x0205 /getitem grant")
	s.log.Printf("GX Server World /getitem granted character_id=%d item_uid=%d type=%d type_id=%d bag=%d slot=%d", s.char.ID, item.UID, itemType, typeID, item.Bag, item.Slot)
	s.commandReply("Item agregado al inventario (bag " + strconv.Itoa(int(item.Bag)) + ", slot " + strconv.Itoa(int(item.Slot)) + ").")
}
