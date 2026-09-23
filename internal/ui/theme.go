// Package ui owns everything about how caxxxd looks: the palette, the chrome
// drawn around every step of the flow, and the pterm printers the steps render
// through. Hex values live here and nowhere else.
package ui

import (
	"github.com/pterm/pterm"
	"github.com/pterm/pterm/putils"
)

// The palette starts at International Klein Blue and never leaves it. Klein
// itself is reserved for filled runs — the status bar, the progress bar, the
// selection marks — where light text sits on top of it and the blue is at its
// most legible. Lift and Glow are the same blue raised until it reads as text
// on a dark terminal; Mist is the quiet end of that ramp. Everything else is a
// neutral or one of the three states a step can end in.
const (
	HexKlein = "#002FA7" // International Klein Blue: the primary accent
	HexLift  = "#4775E6" // Klein raised: borders and rules
	HexGlow  = "#5B87FF" // Klein as accent text
	HexMist  = "#A9BEFF" // Klein as labels and secondary text
	HexInk   = "#F4F7FF" // primary text, and text on top of Klein
	HexSlate = "#A2ACC2" // hints and help

	HexSuccess = "#3DD68C"
	HexWarning = "#FFC66D"
	HexDanger  = "#FF7A8A"
)

// Theme carries the palette and the few composed styles the chrome reuses.
type Theme struct {
	Klein pterm.RGB
	Lift  pterm.RGB
	Glow  pterm.RGB
	Mist  pterm.RGB
	Ink   pterm.RGB
	Slate pterm.RGB

	Success pterm.RGB
	Warning pterm.RGB
	Danger  pterm.RGB

	// OnKlein is light text on a solid Klein fill: the status bar's look.
	OnKlein pterm.RGBStyle
}

// NewTheme builds caxxxd's only theme.
func NewTheme() Theme {
	klein := mustRGB(HexKlein)
	ink := mustRGB(HexInk)

	return Theme{
		Klein:   klein,
		Lift:    mustRGB(HexLift),
		Glow:    mustRGB(HexGlow),
		Mist:    mustRGB(HexMist),
		Ink:     ink,
		Slate:   mustRGB(HexSlate),
		Success: mustRGB(HexSuccess),
		Warning: mustRGB(HexWarning),
		Danger:  mustRGB(HexDanger),
		OnKlein: pterm.NewRGBStyle(ink, klein),
	}
}

// mustRGB parses a palette constant. The inputs are literals from this file, so
// a failure is a typo in the palette rather than anything a user can cause.
func mustRGB(hex string) pterm.RGB {
	rgb, err := putils.RGBFromHEX(hex)
	if err != nil {
		panic("ui: invalid palette colour " + hex + ": " + err.Error())
	}
	return rgb
}
