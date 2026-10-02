//go:build !darwin

package lease

// isLocalFS reports every path as local. Hosts without a mount-level
// local flag keep the directory they resolved.
func isLocalFS(string) (bool, error) {
	return true, nil
}
