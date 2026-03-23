package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// FilePickerConfirmMsg is sent when the user confirms a file selection.
type FilePickerConfirmMsg struct {
	Paths []string // relative paths selected
}

// FilePickerCancelMsg is sent when the user dismisses the picker without selection.
type FilePickerCancelMsg struct{}

// fileEntry is one row in the picker list.
type fileEntry struct {
	rel   string // relative path from workspace root
	isDir bool
	size  int64
}

// scoredEntry is a fileEntry with fuzzy-match metadata.
type scoredEntry struct {
	fileEntry
	score   int
	matches []int // rune indices of matched chars in the display name
}

// FilePickerModel is the @ file picker overlay.
type FilePickerModel struct {
	visible    bool
	currentDir string        // current directory scope ("" = root)
	filter     string        // text typed to fuzzy-filter within currentDir
	cursor     int
	allFiles   []fileEntry   // complete set, built from repo.Index
	filtered   []scoredEntry // view after scoping + filtering
	selected   map[string]bool
	scroll     int
	maxVisible int
	width      int
	workspaceRoot string
}

func newFilePickerModel() FilePickerModel {
	return FilePickerModel{
		selected:   make(map[string]bool),
		maxVisible: 12,
	}
}

// SetFiles populates the picker from a deduplicated list of relative paths.
// It builds directory entries by walking each path's parent chain.
func (fp *FilePickerModel) SetFiles(paths []string, workspaceRoot string) {
	fp.workspaceRoot = workspaceRoot

	seen := make(map[string]bool, len(paths))
	dirsSeen := make(map[string]bool)
	entries := make([]fileEntry, 0, len(paths))

	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true

		size := int64(0)
		if workspaceRoot != "" {
			if info, err := os.Stat(filepath.Join(workspaceRoot, p)); err == nil {
				size = info.Size()
			}
		}
		entries = append(entries, fileEntry{rel: p, isDir: false, size: size})

		// Collect all parent directories.
		dir := filepath.Dir(p)
		for dir != "." && dir != "" && !dirsSeen[dir] {
			dirsSeen[dir] = true
			entries = append(entries, fileEntry{rel: dir, isDir: true})
			dir = filepath.Dir(dir)
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}
		return entries[i].rel < entries[j].rel
	})
	fp.allFiles = entries
}

// Show opens the picker at the root directory.
func (fp *FilePickerModel) Show() {
	fp.visible = true
	fp.currentDir = ""
	fp.filter = ""
	fp.cursor = 0
	fp.scroll = 0
	fp.selected = make(map[string]bool)
	fp.refilter()
}

// Hide closes the picker and resets transient state.
func (fp *FilePickerModel) Hide() {
	fp.visible = false
	fp.currentDir = ""
	fp.filter = ""
	fp.cursor = 0
	fp.scroll = 0
	fp.selected = make(map[string]bool)
	fp.filtered = nil
}

func (fp FilePickerModel) Visible() bool { return fp.visible }

// Update handles keypresses while the picker is open.
func (fp FilePickerModel) Update(msg tea.Msg) (FilePickerModel, tea.Cmd) {
	if !fp.visible {
		return fp, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return fp, nil
	}

	switch key.String() {
	case "esc", "ctrl+c":
		fp.Hide()
		return fp, func() tea.Msg { return FilePickerCancelMsg{} }

	case "enter":
		// Confirm: return selected or highlighted item.
		if len(fp.filtered) == 0 {
			return fp, nil
		}
		paths := fp.confirmPaths(fp.filtered[fp.cursor].rel)
		fp.Hide()
		return fp, func() tea.Msg { return FilePickerConfirmMsg{Paths: paths} }

	case "right":
		// Enter directory under cursor.
		if len(fp.filtered) > 0 && fp.filtered[fp.cursor].isDir {
			fp.currentDir = fp.filtered[fp.cursor].rel
			fp.filter = ""
			fp.cursor = 0
			fp.scroll = 0
			fp.refilter()
		}
		return fp, nil

	case "left":
		// Go up one directory level (no-op at root).
		if fp.currentDir == "" {
			return fp, nil
		}
		parent := filepath.Dir(fp.currentDir)
		if parent == "." {
			fp.currentDir = ""
		} else {
			fp.currentDir = parent
		}
		fp.filter = ""
		fp.cursor = 0
		fp.scroll = 0
		fp.refilter()
		return fp, nil

	case "tab":
		// Toggle multi-select and advance cursor.
		if len(fp.filtered) == 0 {
			return fp, nil
		}
		rel := fp.filtered[fp.cursor].rel
		if fp.selected[rel] {
			delete(fp.selected, rel)
		} else {
			fp.selected[rel] = true
		}
		if fp.cursor < len(fp.filtered)-1 {
			fp.cursor++
			fp.clampScroll()
		}

	case "up", "ctrl+p", "ctrl+k":
		if fp.cursor > 0 {
			fp.cursor--
			fp.clampScroll()
		}

	case "down", "ctrl+n", "ctrl+j":
		if fp.cursor < len(fp.filtered)-1 {
			fp.cursor++
			fp.clampScroll()
		}

	case "ctrl+u":
		fp.filter = ""
		fp.cursor = 0
		fp.scroll = 0
		fp.refilter()

	case "backspace":
		if fp.filter == "" {
			return fp, nil
		}
		_, size := utf8.DecodeLastRuneInString(fp.filter)
		fp.filter = fp.filter[:len(fp.filter)-size]
		fp.cursor = 0
		fp.scroll = 0
		fp.refilter()

	default:
		// Printable character → append to filter.
		r, _ := utf8.DecodeRuneInString(key.String())
		if r != utf8.RuneError && unicode.IsPrint(r) && len(key.String()) == utf8.RuneLen(r) {
			fp.filter += string(r)
			fp.cursor = 0
			fp.scroll = 0
			fp.refilter()
		}
	}

	return fp, nil
}

// confirmPaths returns the paths to attach: multi-selected items if any,
// otherwise just the highlighted item.
func (fp FilePickerModel) confirmPaths(highlighted string) []string {
	if len(fp.selected) > 0 {
		out := make([]string, 0, len(fp.selected))
		for p := range fp.selected {
			out = append(out, p)
		}
		sort.Strings(out)
		return out
	}
	return []string{highlighted}
}

// refilter rebuilds fp.filtered scoped to currentDir and filtered by fp.filter.
func (fp *FilePickerModel) refilter() {
	q := strings.ToLower(fp.filter)
	fp.filtered = fp.filtered[:0]

	for _, e := range fp.allFiles {
		// Scope to currentDir: entry must be a direct child of currentDir.
		if fp.currentDir == "" {
			// Root: only top-level entries (no slash in relative path).
			if strings.Contains(e.rel, string(filepath.Separator)) {
				continue
			}
		} else {
			prefix := fp.currentDir + string(filepath.Separator)
			if !strings.HasPrefix(e.rel, prefix) {
				continue
			}
			// Direct child only: no additional separators after the prefix.
			rest := e.rel[len(prefix):]
			if strings.Contains(rest, string(filepath.Separator)) {
				continue
			}
		}

		name := filepath.Base(e.rel)
		if q == "" {
			fp.filtered = append(fp.filtered, scoredEntry{fileEntry: e})
			continue
		}
		score, matches := fuzzyScore(strings.ToLower(name), q)
		if score < 0 {
			continue
		}
		fp.filtered = append(fp.filtered, scoredEntry{fileEntry: e, score: score, matches: matches})
	}

	if q != "" {
		sort.SliceStable(fp.filtered, func(i, j int) bool {
			// Dirs before files within same score.
			if fp.filtered[i].score != fp.filtered[j].score {
				return fp.filtered[i].score > fp.filtered[j].score
			}
			if fp.filtered[i].isDir != fp.filtered[j].isDir {
				return fp.filtered[i].isDir
			}
			return fp.filtered[i].rel < fp.filtered[j].rel
		})
	} else {
		sort.SliceStable(fp.filtered, func(i, j int) bool {
			if fp.filtered[i].isDir != fp.filtered[j].isDir {
				return fp.filtered[i].isDir
			}
			return fp.filtered[i].rel < fp.filtered[j].rel
		})
	}
}

func (fp *FilePickerModel) clampScroll() {
	if fp.cursor < fp.scroll {
		fp.scroll = fp.cursor
	}
	if fp.cursor >= fp.scroll+fp.maxVisible {
		fp.scroll = fp.cursor - fp.maxVisible + 1
	}
}

// View renders the file picker overlay box.
func (fp FilePickerModel) View(width int) string {
	if !fp.visible {
		return ""
	}
	innerW := width - 4
	if innerW < 20 {
		innerW = 20
	}

	// ── Breadcrumb ────────────────────────────────────────────────────────────
	var breadcrumb string
	if fp.currentDir == "" {
		breadcrumb = StyleAccent.Render("@ ") + StyleDim.Render("(root)")
	} else {
		breadcrumb = StyleAccent.Render("@ ") + StyleDim.Render(fp.currentDir+"/")
	}

	// ── Filter input line ─────────────────────────────────────────────────────
	var filterLine string
	if fp.filter == "" {
		filterLine = StyleDim.Render("  filter: ") + StyleDim.Render("type to search…") + StyleAccent.Render("▌")
	} else {
		filterLine = StyleDim.Render("  filter: ") + fp.filter + StyleAccent.Render("▌")
	}

	sep := StyleDim.Render(strings.Repeat("─", innerW))

	// ── File rows ─────────────────────────────────────────────────────────────
	var rows []string
	total := len(fp.filtered)
	if total == 0 {
		rows = append(rows, StyleDim.Render("  (no matches)"))
	} else {
		end := fp.scroll + fp.maxVisible
		if end > total {
			end = total
		}
		for i := fp.scroll; i < end; i++ {
			rows = append(rows, fp.renderRow(fp.filtered[i], i == fp.cursor, innerW))
		}
		// Scroll hint if there are more rows.
		if total > fp.scroll+fp.maxVisible {
			remaining := total - (fp.scroll + fp.maxVisible)
			rows = append(rows, StyleDim.Render(fmt.Sprintf("  ↓ %d more", remaining)))
		}
	}

	// ── Status bar ────────────────────────────────────────────────────────────
	nSel := len(fp.selected)
	var status string
	if nSel > 0 {
		status = StyleDim.Render(fmt.Sprintf(
			"  %d selected · ↑↓ move · →← dir · Tab toggle · Enter confirm · Esc cancel",
			nSel))
	} else {
		status = StyleDim.Render("  ↑↓ move · → enter dir · ← go up · Tab select · Enter confirm · Esc cancel")
	}

	// ── Box ───────────────────────────────────────────────────────────────────
	inner := lipgloss.JoinVertical(lipgloss.Left,
		append([]string{breadcrumb, filterLine, sep}, append(rows, sep, status)...)...)

	box := StyleFollowUpBox.Width(width - 2).Render(inner)

	// Inject "@ files" title into the top border.
	title := " @ files "
	lines := strings.Split(box, "\n")
	if len(lines) > 0 {
		runesBorder := []rune(lines[0])
		titleRunes := []rune(title)
		if len(runesBorder) > len(titleRunes)+1 {
			for i, r := range titleRunes {
				runesBorder[1+i] = r
			}
			lines[0] = string(runesBorder)
		}
		box = strings.Join(lines, "\n")
	}
	return box
}

// renderRow renders a single picker row.
func (fp FilePickerModel) renderRow(e scoredEntry, active bool, width int) string {
	// Selection indicator.
	var indicator string
	switch {
	case fp.selected[e.rel]:
		indicator = StyleGold.Render("✓ ")
	case active:
		indicator = StyleAccent.Render("● ")
	default:
		indicator = "  "
	}

	name := filepath.Base(e.rel)

	// Directory rows.
	if e.isDir {
		dirText := StyleGold.Render(name+"/") + StyleDim.Render(" →")
		line := indicator + dirText
		if active {
			return StyleWizardItemSelected.Render("  " + line)
		}
		return StyleWizardItem.Render("  " + line)
	}

	// File rows: highlight fuzzy-matched characters.
	fileText := highlightMatches(name, e.matches, 0)
	sizeText := ""
	if e.size > 0 {
		sizeText = "  " + StyleDim.Render(humanSize(e.size))
	}
	line := indicator + fileText + sizeText

	if active {
		return StyleWizardItemSelected.Render("  " + line)
	}
	return StyleWizardItem.Render("  " + line)
}

// highlightMatches renders text with fuzzy-matched positions in accent colour.
func highlightMatches(text string, matches []int, offset int) string {
	if len(matches) == 0 {
		return text
	}
	matchSet := make(map[int]bool, len(matches))
	for _, m := range matches {
		matchSet[m-offset] = true
	}
	var sb strings.Builder
	for i, r := range []rune(text) {
		if matchSet[i] {
			sb.WriteString(StyleAccent.Render(string(r)))
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// fuzzyScore returns a match score (higher=better) and matched rune positions.
// Returns -1 if query is not a subsequence of candidate.
func fuzzyScore(s, q string) (int, []int) {
	if q == "" {
		return 0, nil
	}
	sr := []rune(s)
	qr := []rune(q)
	matches := make([]int, 0, len(qr))
	si := 0
	for _, qc := range qr {
		found := false
		for si < len(sr) {
			if sr[si] == qc {
				matches = append(matches, si)
				si++
				found = true
				break
			}
			si++
		}
		if !found {
			return -1, nil
		}
	}
	score := 0
	for i := 1; i < len(matches); i++ {
		if matches[i] == matches[i-1]+1 {
			score += 5 // consecutive bonus
		}
	}
	if len(matches) > 0 && matches[0] == 0 {
		score += 10 // prefix bonus
	}
	score -= len(sr) / 10
	return score, matches
}

// humanSize formats byte count as a compact string.
func humanSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	}
}
