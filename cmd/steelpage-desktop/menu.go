package main

import (
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// buildMenu assembles the native application menu. Kept minimal in v1:
// standard roles plus a Help link. App-specific items (New Doc, Open
// Archive…, Preferences…) land with the preferences milestone.
func buildMenu(app *application.App) *application.Menu {
	menu := app.NewMenu()

	if runtime.GOOS == "darwin" {
		menu.AddRole(application.AppMenu)
	}
	menu.AddRole(application.FileMenu)
	menu.AddRole(application.EditMenu)
	menu.AddRole(application.WindowMenu)

	help := menu.AddSubmenu("Help")
	help.Add("Steelpage on GitHub").OnClick(func(_ *application.Context) {
		_ = app.Browser.OpenURL("https://github.com/markusfluer/steelpage-desktop")
	})

	return menu
}
