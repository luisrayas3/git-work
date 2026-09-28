package tui

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// setClipboard puts text on the clipboard two ways at once: over OSC 52,
// where the terminal in front does the copying,
// which is the one way that works over ssh and in a multiplexer;
// and through the machine's own clipboard tool
// where there is one and a display to own the clipboard,
// because a terminal may ignore OSC 52 — the VTE 0.76 one this was found
// on, under GNOME, did — and a person at a desktop expects a copy to reach
// the system clipboard whatever terminal they have open.
// Both land the same text, so a terminal that honours OSC 52 copies once.
func setClipboard(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), localClipboard(text))
}

// localClipboard writes through the first clipboard tool on PATH that fits
// the display — wl-copy under Wayland, xclip or xsel under X, pbcopy on a
// Mac — and is nothing where there is none.
// A tool that fails fails silently:
// OSC 52 went out too, and the status line already says copied.
func localClipboard(text string) tea.Cmd {
	tool := clipboardTool()
	if tool == nil {
		return nil
	}
	return func() tea.Msg {
		cmd := exec.Command(tool[0], tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return nil
	}
}

// clipboardTool is the command line that writes the machine's clipboard,
// or nil where nothing on this machine can.
func clipboardTool() []string {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	default:
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			candidates = append(candidates, []string{"wl-copy"})
		}
		if os.Getenv("DISPLAY") != "" {
			candidates = append(candidates,
				[]string{"xclip", "-selection", "clipboard"},
				[]string{"xsel", "--clipboard", "--input"})
		}
	}
	for _, candidate := range candidates {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			return candidate
		}
	}
	return nil
}
