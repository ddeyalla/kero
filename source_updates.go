package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/updater"
	"kero/internal/maintenance"
	"kero/internal/proto"
)

type maintenanceState struct {
	busy   bool
	status string
}

func (a *App) startSourceUpdate() {
	if a.maintenance.busy {
		return
	}
	ctx := a.usage.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, 45*time.Minute)
	cl := a.cl
	a.maintenance = maintenanceState{busy: true, status: "Checking Kero and building Shibu…"}
	a.toast("Update check started in GitHub. Your sessions keep running.")
	go func() {
		defer func() {
			timedOut := ctx.Err() != nil && parent.Err() == nil
			cancel()
			if timedOut {
				a.update(func() {
					a.maintenance.busy = false
					a.maintenance.status = "Update monitoring timed out; check GitHub Actions. Current app unchanged."
					a.toast(a.maintenance.status)
				})
			}
		}()
		service, err := maintenance.New()
		var result maintenance.Result
		if err == nil {
			result, err = service.Run(ctx)
		}
		if ctx.Err() != nil {
			return // app closed; never create a repair session after quitting
		}
		if err == nil && result.Failed {
			a.update(func() {
				a.maintenance.status = "Update needs repair; preparing a separate Codex session…"
			})
			var dir string
			path, readErr := os.ReadFile(filepath.Join(proto.Dir(), "source-checkout"))
			if readErr != nil {
				err = readErr
			} else {
				dir, err = maintenance.PrepareRepair(ctx, strings.TrimSpace(string(path)))
			}
			if err == nil && cl != nil && ctx.Err() == nil {
				var session proto.Session
				err = cl.Call("create", proto.CreateParams{Name: "Shibu update repair", Dir: dir,
					Command: maintenance.RepairCommand(result.URL)}, &session)
			} else if err == nil {
				err = context.Canceled
			}
			if ctx.Err() != nil {
				return
			}
			a.update(func() {
				a.maintenance.busy = false
				a.maintenance.status = "Update needs repair. Current app unchanged."
				if err != nil {
					a.toast("Update paused. Could not open Codex repair; see GitHub Actions and source-checkout configuration.")
				} else {
					a.toast("A background Codex repair session is ready. Review its changes before publishing.")
				}
			})
			return
		}
		a.update(func() {
			a.maintenance.busy = false
			if err != nil {
				a.maintenance.status = err.Error()
				a.toast(err.Error())
				return
			}
			a.maintenance.status = "Source check passed. Signed update check opened."
			// Use the existing updater's single install session and restart
			// controls rather than racing its bundle replacement from here.
			updater.CheckForUpdates()
		})
	}()
}
