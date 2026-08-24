// picker_roles.go — The one list of roles the native model picker can read and
// write.
//
// Adding a role used to mean editing four places that nothing bound together:
// the list response, the write gate, the config-field mapping, and the native
// ModelRole enum. Every role added so far was half-added at least once — tiny,
// analysis, and vision each shipped as a picker entry whose save failed with
// "unknown model role" ("모델 전환에 실패했어요"), and submain never reached the
// picker at all despite being the role that carries the autonomous lane.
//
// Two of those four now derive from this list, and tests bind the other two:
// config persistence must accept every role here, and the native enum's wire
// keys must equal this list exactly.
package modelpicker

import "github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"

// pickerRole is one assignable role. Order is the order the phone shows.
type pickerRole struct {
	role modelrole.Role
	// optIn reports the role only once a model is bound to it, so the picker
	// shows "미설정" instead of implying a default that does not exist. Roles
	// that always have an effective model are reported unconditionally.
	optIn bool
}

// pickerRoles is the canonical list. Keep ConfigModelTab.kt's ModelRole enum in
// the same order — TestNativePickerEnumMatchesPickerRoles fails otherwise.
var pickerRoles = []pickerRole{
	{role: modelrole.RoleMain},
	{role: modelrole.RoleCoding, optIn: true},
	{role: modelrole.RoleVision, optIn: true},
	{role: modelrole.RoleTiny},
	{role: modelrole.RoleLightweight},
	{role: modelrole.RoleFallback},
	// submain carries the autonomous lane (heartbeat turns, phone-event
	// judgment, workflow capabilities), deliberately off the interactive
	// subscription. Opt-in: unset means those lanes run on main.
	{role: modelrole.RoleSubmain, optIn: true},
}

// isPickerRole reports whether role may be assigned from the picker.
func isPickerRole(role string) bool {
	for _, r := range pickerRoles {
		if string(r.role) == role {
			return true
		}
	}
	return false
}
