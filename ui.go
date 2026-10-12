package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func createUI(app fyne.App) fyne.Window {
	window := app.NewWindow("StenoCode")
	window.Resize(fyne.NewSize(700, 500))

	title := widget.NewLabel("StenoCode")
	title.TextStyle = fyne.TextStyle{
		Bold: true,
	}

	shortcutEntry := widget.NewEntry()
	shortcutEntry.SetPlaceHolder("Shortcut")

	expansionEntry := widget.NewMultiLineEntry()
	expansionEntry.SetPlaceHolder("Expansion")

	addButton := widget.NewButton("Add snippet", func() {
		shortcut := shortcutEntry.Text
		expansion := expansionEntry.Text

		if shortcut == "" || expansion == "" {
			return
		}

		if err := addSnippet(shortcut, expansion); err != nil {
			fmt.Println("Could not add snippet:", err)
			return
		}

		shortcutEntry.SetText("")
		expansionEntry.SetText("")
	})

	form := container.NewVBox(
		title,
		widget.NewSeparator(),
		widget.NewLabel("Shortcut"),
		shortcutEntry,
		widget.NewLabel("Expansion"),
		expansionEntry,
		addButton,
	)

	window.SetContent(
		container.NewPadded(form),
	)

	return window
}
