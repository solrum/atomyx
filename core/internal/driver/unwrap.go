package driver

// Unwrapper is implemented by a Driver that decorates another Driver.
// Unwrap returns the decorated driver, so a caller can reach a
// capability the decorator does not forward.
type Unwrapper interface {
	Unwrap() Driver
}

// As reports whether d, or any driver it decorates, implements T. It
// checks d first, then follows Unwrap one layer at a time, so a caller
// behaves identically whether or not a decorator wraps the driver.
func As[T any](d Driver) (T, bool) {
	for d != nil {
		if t, ok := d.(T); ok {
			return t, true
		}
		u, ok := d.(Unwrapper)
		if !ok {
			break
		}
		d = u.Unwrap()
	}
	var zero T
	return zero, false
}
