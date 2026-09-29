package sidecar

import (
	"context"

	"dev.atomyx/internal/driver"
)

// appService runs app-lifecycle operations against the currently
// selected device's driver. Every method fails with
// errNoDeviceSelected when no device is selected, keeping that check
// out of the handler layer.
type appService struct {
	session *Session
}

func newAppService(session *Session) *appService {
	return &appService{session: session}
}

func (a *appService) List(ctx context.Context) ([]driver.InstalledApp, error) {
	drv, err := a.driver()
	if err != nil {
		return nil, err
	}
	return drv.ListApps(ctx)
}

func (a *appService) Launch(ctx context.Context, bundleID string, args driver.LaunchArgs) error {
	drv, err := a.driver()
	if err != nil {
		return err
	}
	return drv.LaunchApp(ctx, bundleID, args)
}

func (a *appService) CurrentForeground(ctx context.Context) (driver.ForegroundInfo, error) {
	drv, err := a.driver()
	if err != nil {
		return driver.ForegroundInfo{}, err
	}
	return drv.CurrentForeground(ctx)
}

func (a *appService) driver() (driver.Driver, error) {
	selection, err := requireDeviceOrWireError(a.session)
	if err != nil {
		return nil, err
	}
	return selection.Driver, nil
}
