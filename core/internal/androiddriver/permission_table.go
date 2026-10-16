package androiddriver

import "dev.atomyx/internal/driver"

// firstRuntimePermissionSDK is the first Android API level on which an
// app asks the user for a permission at run time. Below it a permission
// is fixed at install time, so there is nothing to grant or revoke.
const firstRuntimePermissionSDK = 23

// notificationPermissionSDK is the first Android API level on which
// posting notifications is a runtime permission.
const notificationPermissionSDK = 33

// androidPermissionRule lists the Android permissions a neutral name
// stands for on every API level from sinceSDK upward, until a rule with
// a higher sinceSDK takes over.
type androidPermissionRule struct {
	sinceSDK    int
	permissions []string
}

// androidPermissionTable maps each neutral permission name to its
// Android runtime permissions. Rules are ordered from the highest
// sinceSDK to the lowest, and the first rule the device's API level
// reaches applies. A name with no rule for the level has no runtime
// permission there.
var androidPermissionTable = map[driver.Permission][]androidPermissionRule{
	driver.PermissionCamera: {
		{firstRuntimePermissionSDK, []string{"android.permission.CAMERA"}},
	},
	driver.PermissionMicrophone: {
		{firstRuntimePermissionSDK, []string{"android.permission.RECORD_AUDIO"}},
	},
	driver.PermissionLocation: {
		{firstRuntimePermissionSDK, []string{
			"android.permission.ACCESS_FINE_LOCATION",
			"android.permission.ACCESS_COARSE_LOCATION",
		}},
	},
	driver.PermissionContacts: {
		{firstRuntimePermissionSDK, []string{
			"android.permission.READ_CONTACTS",
			"android.permission.WRITE_CONTACTS",
		}},
	},
	driver.PermissionCalendar: {
		{firstRuntimePermissionSDK, []string{
			"android.permission.READ_CALENDAR",
			"android.permission.WRITE_CALENDAR",
		}},
	},
	driver.PermissionPhotos: {
		{33, []string{
			"android.permission.READ_MEDIA_IMAGES",
			"android.permission.READ_MEDIA_VIDEO",
		}},
		{firstRuntimePermissionSDK, []string{"android.permission.READ_EXTERNAL_STORAGE"}},
	},
	driver.PermissionNotifications: {
		{notificationPermissionSDK, []string{"android.permission.POST_NOTIFICATIONS"}},
	},
}

// androidPermissionsFor returns the Android permissions that name stands
// for on a device of the given API level, and false when the level has
// none.
func androidPermissionsFor(name driver.Permission, sdk int) ([]string, bool) {
	for _, rule := range androidPermissionTable[name] {
		if sdk >= rule.sinceSDK {
			return rule.permissions, true
		}
	}
	return nil, false
}
