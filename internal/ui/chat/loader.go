package chat

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/lucasb-eyer/go-colorful"
)

// loaderWidth is the number of cells in the data meter. The meter fills
// left to right as a tool's input streams in, mirroring the live write
// progress style everywhere instead of an animated spinner.
const loaderWidth = 10

// loaderMeter renders the bracketed gradient meter for the number of
// received characters. The fill position advances every 1000 chars and
// C wraps the meter's width.
func loaderMeter(sty *styles.Styles, charCount int) string {
	completed := charCount % 1000
	filled := completed / (1000 / loaderWidth)
	if completed > 0 && filled == 0 {
		filled = 1
	}

	from, _ := colorful.MakeColor(sty.WorkingGradFromColor)
	to, _ := colorful.MakeColor(sty.WorkingGradToColor)
	var meter strings.Builder
	meter.WriteByte('[')
	for i := range loaderWidth {
		progress := float64(i) / float64(loaderWidth-1)
		segment := "="
		if i >= filled {
			segment = "."
		}
		meter.WriteString(lipgloss.NewStyle().Foreground(from.BlendHcl(to, progress).Clamped()).Render(segment))
	}
	meter.WriteByte(']')
	return meter.String()
}

// loaderDataView renders the received-data meter together with the
// kilo-bucket and character count, the live progress counterpart to a
// spinner: "3k [=====.....] 3,024 chars".
func loaderDataView(sty *styles.Styles, charCount int) string {
	return fmt.Sprintf("%dk %s %s chars",
		charCount/1000,
		loaderMeter(sty, charCount),
		common.FormatCredits(charCount),
	)
}
