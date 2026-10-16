package driver

import (
	"fmt"
	"sort"
	"strings"

	"dev.atomyx/internal/diag"
)

// Permission is a platform-neutral name for a group of app permissions.
// Each adapter maps it to the permissions its platform defines.
type Permission string

const (
	PermissionCamera        Permission = "camera"
	PermissionLocation      Permission = "location"
	PermissionMicrophone    Permission = "microphone"
	PermissionPhotos        Permission = "photos"
	PermissionContacts      Permission = "contacts"
	PermissionCalendar      Permission = "calendar"
	PermissionNotifications Permission = "notifications"
)

// PermissionState is the state SetPermissions puts a permission in.
type PermissionState string

const (
	// PermissionAllow grants the permission without a prompt.
	PermissionAllow PermissionState = "allow"
	// PermissionDeny revokes the permission, so the app sees it as
	// not granted.
	PermissionDeny PermissionState = "deny"
)

// AllPermissions returns every permission name in a stable order.
func AllPermissions() []Permission {
	return []Permission{
		PermissionCamera,
		PermissionLocation,
		PermissionMicrophone,
		PermissionPhotos,
		PermissionContacts,
		PermissionCalendar,
		PermissionNotifications,
	}
}

// AllPermissionStates returns every permission state in a stable order.
func AllPermissionStates() []PermissionState {
	return []PermissionState{PermissionAllow, PermissionDeny}
}

// ParsePermission returns the Permission named by name, or false when no
// permission has that name.
func ParsePermission(name string) (Permission, bool) {
	for _, p := range AllPermissions() {
		if string(p) == name {
			return p, true
		}
	}
	return "", false
}

// ParsePermissionState returns the PermissionState named by name, or
// false when no state has that name.
func ParsePermissionState(name string) (PermissionState, bool) {
	for _, s := range AllPermissionStates() {
		if string(s) == name {
			return s, true
		}
	}
	return "", false
}

// ValidatePermissions rejects a permissions map that is empty, names a
// permission no adapter knows, or holds an unknown state. The error
// carries diag.CodeInvalidArgument and lists the accepted names.
func ValidatePermissions(permissions map[Permission]PermissionState) error {
	if len(permissions) == 0 {
		return invalidPermissions("at least one permission is required")
	}
	for _, p := range SortedPermissions(permissions) {
		if _, ok := ParsePermission(string(p)); !ok {
			return invalidPermissions(fmt.Sprintf("%q is not a known permission, use one of: %s", p, joinPermissionNames()))
		}
		if _, ok := ParsePermissionState(string(permissions[p])); !ok {
			return invalidPermissions(fmt.Sprintf("%q is not a permission state for %s, use allow or deny", permissions[p], p))
		}
	}
	return nil
}

// SortedPermissions returns the keys of permissions in ascending order,
// so every adapter applies a map in one deterministic sequence.
func SortedPermissions(permissions map[Permission]PermissionState) []Permission {
	keys := make([]Permission, 0, len(permissions))
	for p := range permissions {
		keys = append(keys, p)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}

func joinPermissionNames() string {
	names := make([]string, 0, len(AllPermissions()))
	for _, p := range AllPermissions() {
		names = append(names, string(p))
	}
	return strings.Join(names, ", ")
}

func invalidPermissions(message string) error {
	return diag.New(diag.CodeInvalidArgument, message, diag.InvalidArgumentRemedy, nil)
}
