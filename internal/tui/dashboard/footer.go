package dashboard

// renderFooter returns the context-sensitive hotkey reference line.
func renderFooter(m Model) string {
	if m.job.active {
		return footerStyle.Render("Busy: " + busySpinner(m.tick) + " " + m.job.label + "  q/Esc quit")
	}

	vlc := ""
	if m.vlcAvailable() {
		vlc = footerStyle.Render("  v vlc  V vlc/all")
	}

	kind := m.viewKind(m.activeView)
	switch kind {
	case "timeline":
		tv := m.timelineView
		switch {
		case tv.concatFocus:
			return footerStyle.Render("←/→ views  ↑/↓ move  u refresh") + vlc + footerStyle.Render("  e/E open  x del  r finalize  o open  ? help  q/Esc quit")
		case tv.focusPanel == 0:
			return footerStyle.Render("←/→ views  ↑/↓ move  u refresh") + vlc + footerStyle.Render("  e edit  r finalize  o open  ? help  q/Esc quit")
		case tv.marked:
			// ↑/↓ still move the cursor here — that is how the partner is
			// named — so the footer keeps them and drops only "views".
			return footerStyle.Render("↑/↓ pick the slot to trade with  s/Enter swap  Esc cancel")
		case tv.cycling:
			// While cycling, ←/→ belong to the slot, not to view switching —
			// so the footer must not still advertise them as "views".
			// Every step is already written, so leaving is "done", never
			// "cancel" — there is nothing left to discard.
			return footerStyle.Render("←/→ change this slot  Enter/s commit  Esc undo")
		default:
			return footerStyle.Render("←/→ views  ↑/↓ move  s change  l lock  S shuffle  u refresh") + vlc + footerStyle.Render("  r finalize  o open  ? help  q/Esc quit")
		}
	case "collection":
		altV := ""
		if m.vlcAvailable() {
			altV = footerStyle.Render("  ⌥v uncut")
		}
		return footerStyle.Render("←/→ views  ↑/↓ move  J/K reorder  a add  d dup  x del  u refresh  f/F fetch/all") + vlc + altV + footerStyle.Render("  e/E edit/ext  r/R render/all  o open  ? help  q/Esc quit")
	case "cache":
		return footerStyle.Render("←/→ views  ↑/↓ move  f filter  e edit  x del  D doctor  u refresh") + vlc + footerStyle.Render("  o open  ? help  q/Esc quit")
	case "tools":
		// No "u refresh" here: the tools view rebinds u to update the
		// selected tool, and r re-detects instead.
		return footerStyle.Render("←/→ views  ↑/↓ move  r refresh  u update  U update all  o open  ? help  q/Esc quit")
	}
	return footerStyle.Render("←/→ views  u refresh  o open  ? help  q/Esc quit")
}

func busySpinner(tick int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if len(frames) == 0 {
		return ""
	}
	if tick < 0 {
		tick = 0
	}
	return frames[tick%len(frames)]
}
