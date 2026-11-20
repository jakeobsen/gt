package app

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"

	"github.com/jakeobsen/gt/kit"
)

// styles holds every style the UI uses, picked for a light or dark terminal
// background. Rebuild it when the background is known (tea.BackgroundColorMsg).
type styles struct {
	isDark bool

	brand       lipgloss.Style
	divider     lipgloss.Style
	section     lipgloss.Style
	title       lipgloss.Style
	subtitle    lipgloss.Style
	accessory   lipgloss.Style
	match       lipgloss.Style
	marker      lipgloss.Style
	empty       lipgloss.Style
	emptyTitle  lipgloss.Style
	footerKey   lipgloss.Style
	footerLabel lipgloss.Style
	status      lipgloss.Style
	statusError lipgloss.Style
	success     lipgloss.Style
	warning     lipgloss.Style
	modalTitle  lipgloss.Style

	// selectedBg is laid under every part of the selected row.
	selectedBg lipgloss.Style
}

func newStyles(isDark bool) styles {
	ld := lipgloss.LightDark(isDark)
	accent := ld(lipgloss.Color("#6D28D9"), lipgloss.Color("#A78BFA"))
	text := ld(lipgloss.Color("#111827"), lipgloss.Color("#E5E7EB"))
	dim := ld(lipgloss.Color("#6B7280"), lipgloss.Color("#9CA3AF"))
	faint := ld(lipgloss.Color("#D1D5DB"), lipgloss.Color("#374151"))
	selected := ld(lipgloss.Color("#EDE9FE"), lipgloss.Color("#2E2747"))
	danger := ld(lipgloss.Color("#B91C1C"), lipgloss.Color("#F87171"))
	green := ld(lipgloss.Color("#15803D"), lipgloss.Color("#4ADE80"))
	orange := ld(lipgloss.Color("#C2410C"), lipgloss.Color("#FB923C"))

	return styles{
		isDark:      isDark,
		brand:       lipgloss.NewStyle().Bold(true).Foreground(accent),
		divider:     lipgloss.NewStyle().Foreground(faint),
		section:     lipgloss.NewStyle().Foreground(dim).Bold(true),
		title:       lipgloss.NewStyle().Foreground(text),
		subtitle:    lipgloss.NewStyle().Foreground(dim),
		accessory:   lipgloss.NewStyle().Foreground(dim),
		match:       lipgloss.NewStyle().Foreground(accent).Bold(true),
		marker:      lipgloss.NewStyle().Foreground(accent).Bold(true),
		empty:       lipgloss.NewStyle().Foreground(dim),
		emptyTitle:  lipgloss.NewStyle().Foreground(text).Bold(true),
		footerKey:   lipgloss.NewStyle().Foreground(text).Bold(true),
		footerLabel: lipgloss.NewStyle().Foreground(dim),
		status:      lipgloss.NewStyle().Foreground(dim),
		statusError: lipgloss.NewStyle().Foreground(danger),
		success:     lipgloss.NewStyle().Foreground(green),
		warning:     lipgloss.NewStyle().Foreground(orange),
		modalTitle:  lipgloss.NewStyle().Foreground(text).Bold(true),
		selectedBg:  lipgloss.NewStyle().Background(selected),
	}
}

// onSelected returns s with the selected-row background added when sel is true.
func (st styles) onSelected(s lipgloss.Style, sel bool) lipgloss.Style {
	if !sel {
		return s
	}
	return s.Background(st.selectedBg.GetBackground())
}

// colour maps a plugin's semantic colour to a style.
func (st styles) colour(c kit.Color) lipgloss.Style {
	switch c {
	case kit.Red:
		return st.statusError
	case kit.Orange:
		return st.warning
	case kit.Green:
		return st.success
	}
	return st.accessory
}

// inputStyles are the text input styles: the defaults for the background,
// with a steady cursor (no blink timer redrawing the screen).
func (st styles) inputStyles() textinput.Styles {
	s := textinput.DefaultStyles(st.isDark)
	s.Cursor.Blink = false
	return s
}
