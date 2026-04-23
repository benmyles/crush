package chat

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	uianim "github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

type LoadingStats struct {
	UpChars   int
	DownChars int
}

func countLoadingChars(content string) int {
	return utf8.RuneCountInString(content)
}

func renderLoadingStats(sty *styles.Styles, stats LoadingStats) string {
	up := sty.Tool.LoadingStatUp.Render("up " + formatLoadingCount(stats.UpChars))
	down := sty.Tool.LoadingStatDown.Render("down " + formatLoadingCount(stats.DownChars))
	return sty.Tool.LoadingStats.Render(fmt.Sprintf("%s  %s", up, down))
}

func renderLoadingFooter(sty *styles.Styles, anim *uianim.Anim, stats LoadingStats) string {
	statsView := renderLoadingStats(sty, stats)
	if anim == nil {
		return statsView
	}
	return fmt.Sprintf("%s %s", anim.Render(), statsView)
}

func appendLoadingStats(content string, stats string, compact bool) string {
	if stats == "" {
		return content
	}
	if content == "" {
		return stats
	}
	if compact {
		return content + " " + stats
	}
	return content + "\n" + stats
}

func formatLoadingCount(count int) string {
	if count < 0 {
		count = 0
	}
	if count < 1_000 {
		return strconv.Itoa(count)
	}

	type countUnit struct {
		value  int
		suffix string
	}
	for _, unit := range []countUnit{
		{value: 1_000_000, suffix: "m"},
		{value: 1_000, suffix: "k"},
	} {
		if count < unit.value {
			continue
		}
		scaled := float64(count) / float64(unit.value)
		if scaled >= 10 || count%unit.value == 0 {
			return fmt.Sprintf("%.0f%s", scaled, unit.suffix)
		}
		return fmt.Sprintf("%.1f%s", scaled, unit.suffix)
	}

	return strconv.Itoa(count)
}
