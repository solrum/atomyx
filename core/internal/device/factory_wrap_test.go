package device

import (
	"context"
	"net"
	"testing"

	"dev.atomyx/internal/driver"
	"dev.atomyx/internal/process"
	"dev.atomyx/internal/wire"
)

// tappingWrap decorates a driver and records every Tap that reaches it,
// and the order in which wraps were applied.
type tappingWrap struct {
	driver.Driver
	label string
	log   *[]string
}

func (w tappingWrap) Tap(ctx context.Context, p wire.Point) error {
	*w.log = append(*w.log, w.label)
	return w.Driver.Tap(ctx, p)
}

func wrapWith(label string, log *[]string) DriverWrap {
	return func(d driver.Driver) driver.Driver { return tappingWrap{Driver: d, label: label, log: log} }
}

func TestMockDriverFactory_WithDriverWrap_BuiltDriverAndOrchestraUseTheWrappedDriver(t *testing.T) {
	var log []string
	factory := NewMockDriverFactory().WithDriverWrap(wrapWith("observe", &log))

	built, err := factory.Build(context.Background(), Descriptor{ID: "d1", Platform: PlatformAndroid})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := built.Driver.(tappingWrap); !ok {
		t.Fatalf("Built.Driver is %T, want the wrapped driver", built.Driver)
	}
	if err := built.Orchestra.TapAt(context.Background(), wire.Point{X: 1, Y: 1}); err != nil {
		t.Fatalf("TapAt() error = %v", err)
	}
	if len(log) != 1 {
		t.Fatalf("the Orchestra reached the driver %d times through the wrap, want 1", len(log))
	}
}

func TestMockDriverFactory_WithDriverWrap_LeavesTheReceiverUnwrappedAndNestsLaterWrapsOutside(t *testing.T) {
	var log []string
	base := NewMockDriverFactory()
	inner := base.WithDriverWrap(wrapWith("inner", &log))
	outer := inner.(WrappingFactory).WithDriverWrap(wrapWith("outer", &log))

	plain, _ := base.Build(context.Background(), Descriptor{})
	if _, wrapped := plain.Driver.(tappingWrap); wrapped {
		t.Fatal("WithDriverWrap changed the factory it was called on")
	}
	built, _ := outer.Build(context.Background(), Descriptor{})
	_ = built.Driver.Tap(context.Background(), wire.Point{})
	if len(log) != 2 || log[0] != "outer" || log[1] != "inner" {
		t.Fatalf("wrap order = %v, want [outer inner]", log)
	}
}

func TestDriverFactory_Build_AppliesTheWrapBeforeConnect(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	closedPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	var wrapped []string
	factory := NewDriverFactory(DriverFactoryDeps{
		Runner:     process.NewFake(),
		Background: process.NewFakeBackground(),
		WrapDriver: func(d driver.Driver) driver.Driver {
			wrapped = append(wrapped, d.Platform())
			return d
		},
	})
	_, err = factory.Build(context.Background(), Descriptor{ID: "SIM-1", Platform: PlatformIOS, Kind: KindSimulator, Port: closedPort})
	if err == nil {
		t.Fatal("Build() succeeded against a closed port")
	}
	if len(wrapped) != 1 || wrapped[0] != "ios" {
		t.Fatalf("wrap saw %v, want exactly the ios driver", wrapped)
	}
}
