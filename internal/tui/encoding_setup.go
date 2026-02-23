package tui

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"powerhour/internal/tools"
)

// EncodingSetupResult holds the values selected by the user in the carousel.
type EncodingSetupResult struct {
	Cancelled    bool
	VideoCodec   string
	Width        int
	Height       int
	FPS          int
	CRF          int
	Preset       string
	VideoBitrate string
	Container    string
	AudioCodec   string
	AudioBitrate string
	SampleRate   int
	Channels     int
	LoudnormEnabled bool
}

var codecHints = map[string]string{
	"h264_videotoolbox": "Apple VideoToolbox",
	"hevc_videotoolbox": "Apple VideoToolbox",
	"h264_nvenc":        "NVIDIA NVENC",
	"hevc_nvenc":        "NVIDIA NVENC",
	"av1_nvenc":         "NVIDIA NVENC",
	"h264_amf":          "AMD AMF",
	"hevc_amf":          "AMD AMF",
	"av1_amf":           "AMD AMF",
	"libx264":           "software",
	"libx265":           "software",
	"libvpx-vp9":        "software",
	"libsvtav1":         "software (SVT)",
	"librav1e":          "software (rav1e)",
	"libaom-av1":        "software (reference)",
}

var containerInfo = []struct {
	name string
	desc string
}{
	{"mp4", "Most compatible. Works on all devices, players, and\nstreaming platforms. Required by some services."},
	{"mkv", "Flexible container. Supports more codecs, subtitles,\nand chapters. Good for local archiving."},
	{"mov", "Apple's native format. Preferred for editing\nworkflows on macOS and iOS."},
}

var videoBitrateInfo = []struct{ name, desc string }{
	{"4M", "Low — suitable for SD or fast web uploads"},
	{"8M", "Good — solid for 1080p"},
	{"16M", "High — strong 1080p or light 4K"},
	{"24M", "Very high — broadcast quality"},
}

const videoBitrateNote = "Higher bitrate only helps up to your source footage\n" +
	"quality. If source clips are already compressed,\n" +
	"raising this mostly increases file size."

var audioCodecInfo = []struct {
	name string
	desc string
}{
	{"aac", "Universal. Works with every container and player.\n" +
		"Solid quality. Best default for most use cases."},
	{"libopus", "Better quality at lower bitrates. Best paired with\n" +
		"mkv. Some older mp4 players don't support it."},
}

var audioBitrateInfo = []struct{ name, desc string }{
	{"128k", "Good — clear audio for most content"},
	{"192k", "Standard — recommended for music"},
	{"256k", "High — richer detail on quality source audio"},
	{"320k", "Maximum — beyond audible difference for most"},
}

const audioBitrateNote = "Audio bitrate is a ceiling, not an upgrade — if your\n" +
	"source was compressed at 128k, encoding at 320k\n" +
	"adds file size without recovering lost quality."

var presetInfo = []struct{ name, desc string }{
	{"fast", "Quickest encode, slightly larger files"},
	{"medium", "Balanced speed and compression"},
	{"slow", "Better compression, noticeably slower"},
}

const presetNote = "Only applies to software encoders (libx264, libx265,\n" +
	"libsvtav1, etc.). Hardware encoders like VideoToolbox\n" +
	"and NVENC ignore this setting entirely."

var resolutionInfo = []struct{ name, desc string }{
	{"1280×720", "HD — smaller files, fine for most clips"},
	{"1920×1080", "Full HD — recommended for most projects"},
	{"3840×2160", "4K UHD — large files, requires capable hardware"},
}

var fpsInfo = []struct{ name, desc string }{
	{"24", "Cinematic — film standard"},
	{"30", "Standard — broadcast / web video"},
	{"60", "Smooth — high-motion or gaming content"},
}

const fpsNote = "Source clips are resampled to this rate.\n" +
	"Higher FPS increases file size proportionally."

var crfInfo = []struct{ name, desc string }{
	{"18", "Near-lossless — excellent quality, large files"},
	{"20", "High quality — recommended for most use cases"},
	{"23", "Medium quality — default for libx264"},
	{"28", "Lower quality — smaller files, noticeable at motion"},
}

const crfNote = "CRF (Constant Rate Factor) controls quality.\n" +
	"Lower = better quality and larger files.\n" +
	"Ignored when a video bitrate cap is used."

var sampleRateInfo = []struct{ name, desc string }{
	{"44100", "CD quality — compatible with everything"},
	{"48000", "Broadcast standard — recommended for video"},
}

var channelsInfo = []struct{ name, desc string }{
	{"1", "Mono — single-channel, half the audio data"},
	{"2", "Stereo — standard for music and video"},
}

var loudnormInfo = []struct{ name, desc string }{
	{"enabled", "Normalize loudness to EBU R128 (-14 LUFS)\nReduces volume jumps between clips"},
	{"disabled", "Preserve original levels from source clips\nVolume may vary between segments"},
}

// probeResultMsg carries the result of the initial hardware probe.
type probeResultMsg struct {
	profile tools.EncodingProfile
	err     error
}

type encodingTickMsg struct{}

type carouselRow struct {
	label   string
	options []string
	current int
}

type encodingSetupModel struct {
	rows       []carouselRow
	focused    int
	done       bool
	cancelled  bool
	current    tools.EncodingDefaults // stored to pre-select rows after probe
	ffmpegPath string
	probing    bool
	probeErr   error
	probeFrame int
}

func newEncodingSetupModel(ffmpegPath string, current tools.EncodingDefaults) encodingSetupModel {
	// Start with placeholder rows; populated when the probe completes.
	placeholder := []string{"..."}
	return encodingSetupModel{
		rows: []carouselRow{
			{label: "Video codec", options: placeholder},
			{label: "Resolution", options: placeholder},
			{label: "FPS", options: placeholder},
			{label: "CRF", options: placeholder},
			{label: "Preset", options: placeholder},
			{label: "Video bitrate", options: placeholder},
			{label: "Container", options: placeholder},
			{label: "Audio codec", options: placeholder},
			{label: "Audio bitrate", options: placeholder},
			{label: "Sample rate", options: placeholder},
			{label: "Channels", options: placeholder},
			{label: "Loudnorm", options: placeholder},
		},
		current:    current,
		ffmpegPath: ffmpegPath,
		probing:    true,
	}
}

func (m encodingSetupModel) Init() tea.Cmd {
	return tea.Batch(doProbe(m.ffmpegPath), encodingTick())
}

func doProbe(ffmpegPath string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		profile, err := tools.ProbeEncoders(ctx, ffmpegPath)
		if err == nil {
			_ = tools.SaveEncodingProfile(profile)
		}
		return probeResultMsg{profile: profile, err: err}
	}
}

func encodingTick() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(_ time.Time) tea.Msg {
		return encodingTickMsg{}
	})
}

func populateRows(profile tools.EncodingProfile, current tools.EncodingDefaults) []carouselRow {
	codecs := profile.AvailableAll()
	if len(codecs) == 0 {
		codecs = []string{"libx264"}
	}
	resolutions := []string{"1280×720", "1920×1080", "3840×2160"}
	fpsList := []string{"24", "30", "60"}
	crfList := []string{"18", "20", "23", "28"}
	presets := []string{"fast", "medium", "slow"}
	videoBitrates := []string{"4M", "8M", "16M", "24M"}
	containers := []string{"mp4", "mkv", "mov"}
	audioCodecs := []string{"aac", "libopus"}
	audioBitrates := []string{"128k", "192k", "256k", "320k"}
	sampleRates := []string{"44100", "48000"}
	channelsList := []string{"1", "2"}
	loudnormList := []string{"enabled", "disabled"}

	currentRes := ""
	if current.Width > 0 && current.Height > 0 {
		currentRes = fmt.Sprintf("%d×%d", current.Width, current.Height)
	}
	currentFPS := strconv.Itoa(current.FPS)
	currentCRF := strconv.Itoa(current.CRF)
	currentSR := strconv.Itoa(current.SampleRate)
	currentCh := strconv.Itoa(current.Channels)
	currentLN := "enabled"
	if current.LoudnormEnabled != nil && !*current.LoudnormEnabled {
		currentLN = "disabled"
	}

	return []carouselRow{
		{label: "Video codec", options: codecs, current: findIdx(codecs, current.VideoCodec, 0)},
		{label: "Resolution", options: resolutions, current: findIdx(resolutions, currentRes, 1)},
		{label: "FPS", options: fpsList, current: findIdx(fpsList, currentFPS, 1)},
		{label: "CRF", options: crfList, current: findIdx(crfList, currentCRF, 1)},
		{label: "Preset", options: presets, current: findIdx(presets, current.Preset, 0)},
		{label: "Video bitrate", options: videoBitrates, current: findIdx(videoBitrates, current.VideoBitrate, 1)},
		{label: "Container", options: containers, current: findIdx(containers, current.Container, 0)},
		{label: "Audio codec", options: audioCodecs, current: findIdx(audioCodecs, current.AudioCodec, 0)},
		{label: "Audio bitrate", options: audioBitrates, current: findIdx(audioBitrates, current.AudioBitrate, 1)},
		{label: "Sample rate", options: sampleRates, current: findIdx(sampleRates, currentSR, 1)},
		{label: "Channels", options: channelsList, current: findIdx(channelsList, currentCh, 1)},
		{label: "Loudnorm", options: loudnormList, current: findIdx(loudnormList, currentLN, 0)},
	}
}

func findIdx(options []string, value string, defaultIdx int) int {
	if value == "" {
		return defaultIdx
	}
	for i, o := range options {
		if o == value {
			return i
		}
	}
	return defaultIdx
}

func (m encodingSetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case probeResultMsg:
		m.probing = false
		if msg.err != nil {
			m.probeErr = msg.err
		} else {
			m.rows = populateRows(msg.profile, m.current)
		}
		return m, nil

	case encodingTickMsg:
		if m.probing {
			m.probeFrame++
			return m, encodingTick()
		}
		return m, nil

	case tea.KeyMsg:
		if m.probing {
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			if m.focused > 0 {
				m.focused--
			}
		case "down", "j":
			if m.focused < len(m.rows)-1 {
				m.focused++
			}
		case "left", "h":
			row := m.rows[m.focused]
			row.current = (row.current - 1 + len(row.options)) % len(row.options)
			m.rows[m.focused] = row
		case "right", "l":
			row := m.rows[m.focused]
			row.current = (row.current + 1) % len(row.options)
			m.rows[m.focused] = row
		case "enter":
			m.done = true
			return m, tea.Quit
		case "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m encodingSetupModel) View() string {
	faint := lipgloss.NewStyle().Faint(true)

	if m.done {
		var sb strings.Builder
		sb.WriteString("\n")
		for _, row := range m.rows {
			sb.WriteString(fmt.Sprintf("%s %s\n",
				faint.Render(fmt.Sprintf("  %-14s", row.label)),
				row.options[row.current],
			))
		}
		sb.WriteString("\n")
		return sb.String()
	}

	if m.cancelled {
		return faint.Render("  cancelled") + "\n"
	}

	focused := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))

	var sb strings.Builder
	sb.WriteString("\n")

	for i, row := range m.rows {
		var prefix, label, value string
		if m.probing {
			prefix = "  "
			label = faint.Render(fmt.Sprintf("%-14s", row.label))
			value = faint.Render(fmt.Sprintf("%-20s", row.options[row.current]))
		} else if i == m.focused {
			prefix = "▸ "
			label = focused.Render(fmt.Sprintf("%-14s", row.label))
			value = fmt.Sprintf("%-20s", row.options[row.current])
		} else {
			prefix = "  "
			label = faint.Render(fmt.Sprintf("%-14s", row.label))
			value = fmt.Sprintf("%-20s", row.options[row.current])
		}
		sb.WriteString(fmt.Sprintf("%s%s ←  %s→\n", prefix, label, value))
	}

	sb.WriteString("\n")
	sb.WriteString(m.renderHelpPanel())
	sb.WriteString("\n")

	if !m.probing {
		sb.WriteString(faint.Render("  [↑↓] Navigate  [←→] Change  [Enter] Save  [Esc] Cancel"))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (m encodingSetupModel) renderHelpPanel() string {
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		BorderForeground(lipgloss.Color("8"))

	if m.probing {
		frame := spinnerFrames[m.probeFrame%len(spinnerFrames)]
		if m.probeErr != nil {
			red := lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
			return panelStyle.Render(red.Render("Probe failed: " + m.probeErr.Error()))
		}
		return panelStyle.Render(fmt.Sprintf("%s Probing encoders...", frame))
	}

	switch m.focused {
	case 0: // Video codec
		return panelStyle.Render(m.codecPanelContent())
	case 1: // Resolution
		return panelStyle.Render(genericListPanel(m.rows[1].options[m.rows[1].current], resolutionInfo, ""))
	case 2: // FPS
		return panelStyle.Render(genericListPanel(m.rows[2].options[m.rows[2].current], fpsInfo, fpsNote))
	case 3: // CRF
		return panelStyle.Render(genericListPanel(m.rows[3].options[m.rows[3].current], crfInfo, crfNote))
	case 4: // Preset
		return panelStyle.Render(genericListPanel(m.rows[4].options[m.rows[4].current], presetInfo, presetNote))
	case 5: // Video bitrate
		return panelStyle.Render(bitrateListPanel(m.rows[5].options[m.rows[5].current], videoBitrateInfo, videoBitrateNote))
	case 6: // Container
		return panelStyle.Render(m.containerPanelContent())
	case 7: // Audio codec
		return panelStyle.Render(m.audioCodecPanelContent())
	case 8: // Audio bitrate
		return panelStyle.Render(bitrateListPanel(m.rows[8].options[m.rows[8].current], audioBitrateInfo, audioBitrateNote))
	case 9: // Sample rate
		return panelStyle.Render(genericListPanel(m.rows[9].options[m.rows[9].current], sampleRateInfo, ""))
	case 10: // Channels
		return panelStyle.Render(genericListPanel(m.rows[10].options[m.rows[10].current], channelsInfo, ""))
	case 11: // Loudnorm
		return panelStyle.Render(genericListPanel(m.rows[11].options[m.rows[11].current], loudnormInfo, ""))
	}
	return ""
}

func (m encodingSetupModel) codecPanelContent() string {
	faint := lipgloss.NewStyle().Faint(true)
	bold := lipgloss.NewStyle().Bold(true)

	// Rebuild profile view from the current row options by matching back to families.
	var sb strings.Builder
	sb.WriteString(bold.Render("Encoders found by family:"))
	sb.WriteString("\n\n")

	// Pull available-by-family from the saved profile on disk (already probed).
	profile := tools.LoadEncodingProfile()
	for _, family := range tools.CodecFamilies {
		var available []string
		if profile != nil {
			available = profile.AvailableByFamily[family.Name]
		}
		familyLabel := fmt.Sprintf("  %-16s", family.Name)
		if len(available) == 0 {
			sb.WriteString(familyLabel + faint.Render("(none)") + "\n")
		} else {
			for i, codec := range available {
				if i == 0 {
					sb.WriteString(familyLabel)
				} else {
					sb.WriteString(fmt.Sprintf("  %-16s", ""))
				}
				hint := codecHints[codec]
				if hint != "" {
					sb.WriteString(fmt.Sprintf("%s  %s", codec, faint.Render("("+hint+")")))
				} else {
					sb.WriteString(codec)
				}
				sb.WriteString("\n")
			}
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (m encodingSetupModel) containerPanelContent() string {
	faint := lipgloss.NewStyle().Faint(true)
	bold := lipgloss.NewStyle().Bold(true)
	current := m.rows[6].options[m.rows[6].current]

	var sb strings.Builder
	for i, info := range containerInfo {
		if i > 0 {
			sb.WriteString("\n")
		}
		prefix, nameStr := "  ", faint.Render(fmt.Sprintf("%-4s", info.name))
		if info.name == current {
			prefix, nameStr = "▸ ", bold.Render(fmt.Sprintf("%-4s", info.name))
		}
		for j, line := range strings.Split(info.desc, "\n") {
			if j == 0 {
				sb.WriteString(fmt.Sprintf("%s%s  %s\n", prefix, nameStr, line))
			} else {
				sb.WriteString(fmt.Sprintf("       %s\n", line))
			}
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (m encodingSetupModel) audioCodecPanelContent() string {
	faint := lipgloss.NewStyle().Faint(true)
	bold := lipgloss.NewStyle().Bold(true)
	current := m.rows[7].options[m.rows[7].current]

	var sb strings.Builder
	for i, info := range audioCodecInfo {
		if i > 0 {
			sb.WriteString("\n")
		}
		prefix, nameStr := "  ", faint.Render(fmt.Sprintf("%-8s", info.name))
		if info.name == current {
			prefix, nameStr = "▸ ", bold.Render(fmt.Sprintf("%-8s", info.name))
		}
		for j, line := range strings.Split(info.desc, "\n") {
			if j == 0 {
				sb.WriteString(fmt.Sprintf("%s%s  %s\n", prefix, nameStr, line))
			} else {
				sb.WriteString(fmt.Sprintf("          %s\n", line))
			}
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func bitrateListPanel(current string, items []struct{ name, desc string }, note string) string {
	faint := lipgloss.NewStyle().Faint(true)
	bold := lipgloss.NewStyle().Bold(true)
	var sb strings.Builder
	for _, info := range items {
		prefix, nameStr := "  ", faint.Render(fmt.Sprintf("%-5s", info.name))
		if info.name == current {
			prefix, nameStr = "▸ ", bold.Render(fmt.Sprintf("%-5s", info.name))
		}
		sb.WriteString(fmt.Sprintf("%s%s  %s\n", prefix, nameStr, info.desc))
	}
	sb.WriteString("\n")
	for _, line := range strings.Split(note, "\n") {
		sb.WriteString(faint.Render("  "+line) + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func genericListPanel(current string, items []struct{ name, desc string }, note string) string {
	faint := lipgloss.NewStyle().Faint(true)
	bold := lipgloss.NewStyle().Bold(true)
	var sb strings.Builder
	for _, info := range items {
		prefix, nameStr := "  ", faint.Render(fmt.Sprintf("%-8s", info.name))
		if info.name == current {
			prefix, nameStr = "▸ ", bold.Render(fmt.Sprintf("%-8s", info.name))
		}
		sb.WriteString(fmt.Sprintf("%s%s  %s\n", prefix, nameStr, info.desc))
	}
	sb.WriteString("\n")
	for _, line := range strings.Split(note, "\n") {
		sb.WriteString(faint.Render("  "+line) + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (m encodingSetupModel) result() EncodingSetupResult {
	if m.cancelled || m.probing {
		return EncodingSetupResult{Cancelled: true}
	}
	w, h := parseResolution(m.rows[1].options[m.rows[1].current])
	fps, _ := strconv.Atoi(m.rows[2].options[m.rows[2].current])
	crf, _ := strconv.Atoi(m.rows[3].options[m.rows[3].current])
	sr, _ := strconv.Atoi(m.rows[9].options[m.rows[9].current])
	ch, _ := strconv.Atoi(m.rows[10].options[m.rows[10].current])
	return EncodingSetupResult{
		VideoCodec:      m.rows[0].options[m.rows[0].current],
		Width:           w,
		Height:          h,
		FPS:             fps,
		CRF:             crf,
		Preset:          m.rows[4].options[m.rows[4].current],
		VideoBitrate:    m.rows[5].options[m.rows[5].current],
		Container:       m.rows[6].options[m.rows[6].current],
		AudioCodec:      m.rows[7].options[m.rows[7].current],
		AudioBitrate:    m.rows[8].options[m.rows[8].current],
		SampleRate:      sr,
		Channels:        ch,
		LoudnormEnabled: m.rows[11].options[m.rows[11].current] == "enabled",
	}
}

func parseResolution(s string) (int, int) {
	parts := strings.SplitN(s, "×", 2)
	if len(parts) != 2 {
		return 1920, 1080
	}
	w, _ := strconv.Atoi(parts[0])
	h, _ := strconv.Atoi(parts[1])
	if w <= 0 {
		w = 1920
	}
	if h <= 0 {
		h = 1080
	}
	return w, h
}

// RunEncodingSetup probes encoders and runs the interactive carousel.
// The probe happens inside the TUI; the terminal is grayed out until ready.
func RunEncodingSetup(w io.Writer, ffmpegPath string, current tools.EncodingDefaults) (EncodingSetupResult, error) {
	model := newEncodingSetupModel(ffmpegPath, current)
	p := tea.NewProgram(model, tea.WithOutput(w))
	finalModel, err := p.Run()
	if err != nil {
		return EncodingSetupResult{}, err
	}
	return finalModel.(encodingSetupModel).result(), nil
}
