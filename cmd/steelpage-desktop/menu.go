package main

import (
	"log"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/markusfluer/steelpage-desktop/internal/prefs"
)

// buildMenu assembles the native application menu. The Preferences modal
// lives in the SPA — the menu item just dispatches a DOM event into the
// webview; archive switching uses the native directory picker and takes
// effect on next launch.
func buildMenu(app *application.App, win *application.WebviewWindow) *application.Menu {
	menu := app.NewMenu()

	if runtime.GOOS == "darwin" {
		menu.AddRole(application.AppMenu)
	}

	file := menu.AddSubmenu("File")
	file.Add("Open Archive…").SetAccelerator("CmdOrCtrl+O").OnClick(func(_ *application.Context) {
		openArchiveDialog(app)
	})
	file.Add("Preferences…").SetAccelerator("CmdOrCtrl+,").OnClick(func(_ *application.Context) {
		win.ExecJS(`window.dispatchEvent(new CustomEvent("steelpage:open-prefs"))`)
	})
	file.AddSeparator()
	file.Add("Quit").SetAccelerator("CmdOrCtrl+Q").OnClick(func(_ *application.Context) {
		app.Quit()
	})

	menu.AddRole(application.EditMenu)

	view := menu.AddSubmenu("View")
	view.Add("Search").SetAccelerator("CmdOrCtrl+K").OnClick(func(_ *application.Context) {
		win.ExecJS(`window.dispatchEvent(new CustomEvent("steelpage:open-search"))`)
	})

	menu.AddRole(application.WindowMenu)

	help := menu.AddSubmenu("Help")
	help.Add("Steelpage on GitHub").OnClick(func(_ *application.Context) {
		_ = app.Browser.OpenURL("https://github.com/markusfluer/steelpage-desktop")
	})

	return menu
}

// openArchiveDialog lets the user pick a different content directory. The
// choice is persisted to prefs.json and applied on next launch — live
// re-pointing of git/index/comments is out of scope for v1.
func openArchiveDialog(app *application.App) {
	dir, err := app.Dialog.OpenFile().
		SetTitle("Choose archive folder").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
	if err != nil || dir == "" {
		return
	}
	p, err := prefs.Load()
	if err != nil {
		log.Printf("prefs load: %v", err)
		return
	}
	p.ContentDir = dir
	if err := p.Save(); err != nil {
		log.Printf("prefs save: %v", err)
		return
	}
	app.Dialog.Info().
		SetTitle("Archive changed").
		SetMessage("The new archive will be used the next time Steelpage starts.").
		Show()
}
