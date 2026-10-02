package sessiontest

import (
	"context"

	"dev.atomyx/internal/device"
	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/orchestra"
)

// OrchestraBuilder makes the Orchestra a test factory's driver is acted
// on through.
type OrchestraBuilder func(driver.Driver) *orchestra.Orchestra

// Wrapping makes any test factory satisfy device.WrappingFactory, which
// session.Open requires so its capture observer sits in front of the
// driver. A test factory implements WithDriverWrap as
//
//	return sessiontest.Wrapping(f).WithDriverWrap(wrap)
//
// The wrap is applied to Built.Driver, and the Orchestra the inner
// factory built is replaced by one over the wrapped driver on the
// system clock, so a runner, a tool handler or a CLI command that acts
// through the Orchestra publishes its captures like a production
// session does. A test whose Orchestra needs another clock or logger
// uses WrappingWith. A Built with no Orchestra keeps none.
func Wrapping(inner device.Factory) device.WrappingFactory {
	return WrappingWith(inner, func(d driver.Driver) *orchestra.Orchestra {
		return orchestra.New(orchestra.OrchestraDeps{Driver: d, Clock: orchestra.SystemClock{}})
	})
}

// WrappingWith is Wrapping with the Orchestra built by rebuild over the
// wrapped driver.
func WrappingWith(inner device.Factory, rebuild OrchestraBuilder) device.WrappingFactory {
	return &wrappedFactory{inner: inner, rebuild: rebuild}
}

type wrappedFactory struct {
	inner   device.Factory
	rebuild OrchestraBuilder
	wrap    device.DriverWrap
}

func (f *wrappedFactory) Build(ctx context.Context, desc device.Descriptor) (device.Built, error) {
	built, err := f.inner.Build(ctx, desc)
	if err == nil && f.wrap != nil {
		built.Driver = f.wrap(built.Driver)
		if built.Orchestra != nil {
			built.Orchestra = f.rebuild(built.Driver)
		}
	}
	return built, err
}

func (f *wrappedFactory) WithDriverWrap(outer device.DriverWrap) device.Factory {
	chained := outer
	if inner := f.wrap; inner != nil {
		chained = func(d driver.Driver) driver.Driver { return outer(inner(d)) }
	}
	return &wrappedFactory{inner: f.inner, rebuild: f.rebuild, wrap: chained}
}
