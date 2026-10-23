package main

import (
	"github.com/egoist/mygo/ui"
	"kero/locales"
)

// sourceUpdatesRow keeps source builds separate from published app
// downloads. A running check remains visible when Settings is reopened.
func (a *App) sourceUpdatesRow(c *ui.Context, pal *palette) {
	detail := locales.SetSourceUpdatesDetail()
	if a.maintenance.status != "" {
		detail += "\n" + a.maintenance.status
	}
	settingRow(c, pal, locales.SetSourceUpdates(), detail, func() {
		if a.maintenance.busy {
			ui.Text(c, locales.SetSourceBuilding()).FontSize(13).TextColor(pal.textMuted)
		} else if button(c, "source-update", locales.SetSourceBuild(), "", false, pal) {
			a.startSourceUpdate()
		}
	})
}
