package main

import (
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"sort"
	"strings"
)

type snippet struct {
	Shortcut  string
	Expansion string
}

func loadSnippets() ([]snippet, error) {
	rows, err := db.Query(`
		SELECT shortcut, expansion
		FROM snippets
		ORDER BY shortcut
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snippets []snippet

	for rows.Next() {
		var s snippet
		if err := rows.Scan(&s.Shortcut, &s.Expansion); err != nil {
			return nil, err
		}
		snippets = append(snippets, s)
	}

	return snippets, rows.Err()
}

func createUI(app fyne.App) fyne.Window {
	window := app.NewWindow("StenoCode")
	window.Resize(fyne.NewSize(720, 520))

	title := widget.NewLabelWithStyle(
		"StenoCode",
		fyne.TextAlignLeading,
		fyne.TextStyle{Bold: true},
	)

	status := widget.NewLabel("Loading shortcuts...")

	enabledSwitch := widget.NewCheck(
		"Enable stenography",
		func(enabled bool) {
			setStenographyEnabled(enabled)
			if enabled {
				status.SetText("Stenography enabled")
			} else {
				status.SetText("Stenography disabled")
			}
		},
	)
	enabledSwitch.SetChecked(isStenographyEnabled())

	shortcutEntry := widget.NewEntry()
	shortcutEntry.SetPlaceHolder("Shortcut (e.g. hel)")

	expansionEntry := widget.NewMultiLineEntry()
	expansionEntry.SetPlaceHolder("Expansion (e.g. hello)")
	expansionEntry.SetMinRowsVisible(3)

	var snippets []snippet
	selected := -1

	list := widget.NewList(
		func() int {
			return len(snippets)
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("Shortcut -> Expansion")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(snippets) {
				return
			}
			s := snippets[id]
			obj.(*widget.Label).SetText(
				s.Shortcut + "  ->  " + s.Expansion,
			)
		},
	)

	refresh := func() {
		loaded, err := loadSnippets()
		if err != nil {
			status.SetText("Could not load shortcuts: " + err.Error())
			return
		}

		sort.Slice(loaded, func(i, j int) bool {
			return loaded[i].Shortcut < loaded[j].Shortcut
		})

		snippets = loaded
		selected = -1
		list.UnselectAll()
		list.Refresh()
		status.SetText(fmt.Sprintf("%d shortcuts loaded", len(snippets)))
	}

	list.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(snippets) {
			return
		}
		selected = id
		shortcutEntry.SetText(snippets[id].Shortcut)
		expansionEntry.SetText(snippets[id].Expansion)
	}

	addButton := widget.NewButton("Add shortcut", func() {
		shortcut := strings.TrimSpace(shortcutEntry.Text)
		expansion := expansionEntry.Text

		if shortcut == "" || strings.TrimSpace(expansion) == "" {
			status.SetText("Enter both a shortcut and an expansion.")
			return
		}

		if err := addSnippet(shortcut, expansion); err != nil {
			status.SetText("Could not add shortcut: " + err.Error())
			return
		}

		shortcutEntry.SetText("")
		expansionEntry.SetText("")
		refresh()
	})

	deleteButton := widget.NewButton("Delete selected", func() {
		if selected < 0 || selected >= len(snippets) {
			status.SetText("Select a shortcut to delete.")
			return
		}

		if err := deleteSnippet(snippets[selected].Shortcut); err != nil {
			status.SetText("Could not delete shortcut: " + err.Error())
			return
		}

		shortcutEntry.SetText("")
		expansionEntry.SetText("")
		refresh()
	})

	clearButton := widget.NewButton("Clear form", func() {
		selected = -1
		list.UnselectAll()
		shortcutEntry.SetText("")
		expansionEntry.SetText("")
	})

	form := container.NewVBox(
		widget.NewLabel("Shortcut"),
		shortcutEntry,
		widget.NewLabel("Expansion"),
		expansionEntry,
		container.NewGridWithColumns(2, addButton, deleteButton),
		clearButton,
	)

	left := container.NewBorder(
		widget.NewLabel("Saved shortcuts"),
		nil, nil, nil,
		list,
	)

	right := container.NewVBox(
		widget.NewLabel("Manage shortcuts"),
		form,
	)

	split := container.NewHSplit(left, right)
	split.SetOffset(0.48)

	content := container.NewBorder(
		container.NewVBox(
			title,
			enabledSwitch,
			widget.NewSeparator(),
		),
		status,
		nil, nil,
		split,
	)

	window.SetContent(container.NewPadded(content))

	refresh()

	return window
}
