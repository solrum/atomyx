package sessiontest

import (
	"context"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
)

// Wrapping makes any test factory satisfy device.WrappingFactory, which
// session.Open requires so its capture observer sits in front of the
// driver. A test factory implements WithDriverWrap as
//
//	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
//
// The wrap is applied to Built.Driver only. The Orchestra the inner
// factory built keeps acting on the driver it was built over, so a test
// that needs the Orchestra's captures to reach the observer must build
// its Orchestra over the wrapped driver itself, as the mock factories
// of the device package do.
func Wrapping(inner device.Factory) device.WrappingFactory {
	return &wrappedFactory{inner: inner}
}

type wrappedFactory struct {
	inner device.Factory
	wrap  device.DriverWrap
}

func (f *wrappedFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	built, err := f.inner.Build(ctx, desc)
	if err == nil && f.wrap != nil {
		built.Driver = f.wrap(built.Driver)
	}
	return built, err
}

func (f *wrappedFactory) WithDriverWrap(outer device.DriverWrap) device.Factory {
	chained := outer
	if inner := f.wrap; inner != nil {
		chained = func(d driver.Driver) driver.Driver { return outer(inner(d)) }
	}
	return &wrappedFactory{inner: f.inner, wrap: chained}
}
