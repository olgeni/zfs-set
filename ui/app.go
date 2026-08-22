// Package ui is the bubbletea front-end of zfs-set.
package ui

import (
	"fmt"
	"os"
	"os/user"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/olgeni/zfs-set/props"
)

type screen int

const (
	scrPick screen = iota
	scrMain
	scrEditor
	scrForm
	scrPlan
	scrBusy
	scrView   // help, keys, tree (viewport)
	scrPicker // filterable list (long value lists)
)

type (
	datasetsMsg struct {
		list []props.Dataset
		err  error
	}
	loadedMsg struct {
		l, parent *props.Listing
		err       error
	}
	appliedMsg struct {
		errs []error
		n    int
	}
	treeMsg struct {
		prop         string
		chain, below []props.Spot
		err          error
	}
	propMsg struct { // a per-key property fetched for "add"
		name string
		val  props.Value
		have bool
		err  error
	}
)

// uiRow is one line of the main table: a group header or a property.
type uiRow struct {
	header string
	prop   *props.Prop
	name   string
	val    props.Value
	have   bool
}

// Model is the root bubbletea model.
type Model struct {
	scr, prevScr screen
	width        int
	height       int

	// dataset picker
	datasets   []props.Dataset
	pickCursor int
	pickOffset int
	pickFilter string
	pickTyping bool
	pickDef    string // dataset to start on (the cwd's)
	fromPicker bool   // the session started on the picker: q/esc on the main screen go back to it

	// dataset
	dataset   string
	listing   *props.Listing
	parent    *props.Listing
	extra     []props.Value // per-key properties added in this session (userquota@bob)
	edits     props.Edits
	history   []props.Edits
	rows      []uiRow
	cursor    int
	offset    int
	filter    string
	typing    bool
	showLinux bool
	onlyLocal bool

	// editor
	ed *editor

	// list picker
	pk      *picker
	pkDone  func(m *Model, value string) tea.Cmd
	pkAbort func(m *Model)

	// forms
	form      *form
	formDone  func(m *Model) tea.Cmd
	formAbort func(m *Model)
	formVals  struct {
		s   string
		yes bool
	}

	// viewport screens and plan
	vp      viewport.Model
	vpTitle string
	plan    *props.Plan
	probs   []props.Problem

	env props.Env

	status, errMsg string
	busyMsg        string
	quitting       bool
	fatal          string
}

// New creates the model; dataset "" starts on the picker with pickDef selected.
func New(dataset, pickDef string) *Model {
	m := &Model{dataset: dataset, pickDef: pickDef, width: 80, height: 24}
	m.vp = viewport.New(80, 20)
	if dataset == "" {
		m.scr = scrPick
		m.fromPicker = true
	} else {
		m.scr = scrMain
	}
	m.env = computeEnv()
	return m
}

func computeEnv() props.Env {
	env := props.Env{Root: os.Geteuid() == 0, Mountd: props.MountdRunning()}
	if u, err := user.Current(); err == nil {
		env.User = u.Username
	}
	env.Cwd, _ = os.Getwd()
	return env
}

func (m *Model) Init() tea.Cmd {
	if m.dataset == "" {
		return loadDatasets
	}
	return m.load()
}

func loadDatasets() tea.Msg {
	l, err := props.Datasets()
	return datasetsMsg{l, err}
}

func (m *Model) load() tea.Cmd {
	ds := m.dataset
	return func() tea.Msg {
		l, err := props.Load(ds)
		if err != nil {
			return loadedMsg{nil, nil, err}
		}
		var parent *props.Listing
		if p := props.ParentOf(ds); p != "" {
			parent, _ = props.Load(p)
		}
		return loadedMsg{l, parent, nil}
	}
}

func (m *Model) setStatus(s string) { m.status, m.errMsg = s, "" }
func (m *Model) setError(s string)  { m.errMsg, m.status = s, "" }

func (m *Model) modified() bool { return len(m.edits) > 0 }

func (m *Model) push() {
	m.history = append(m.history, m.edits.Clone())
	if len(m.history) > 100 {
		m.history = m.history[1:]
	}
}

// rebuildRows recomputes the table: the catalogue groups that apply to the
// dataset, the user properties, the per-key extras, then whatever the
// kernel reported that the catalogue does not know.
func (m *Model) rebuildRows() {
	m.rows = m.rows[:0]
	if m.listing == nil {
		return
	}
	l := m.listing
	f := strings.ToLower(strings.TrimSpace(m.filter))
	match := func(name string, p *props.Prop) bool {
		if f != "" && !strings.Contains(strings.ToLower(name), f) && (p == nil || !strings.Contains(strings.ToLower(p.Note), f)) {
			return false
		}
		if m.onlyLocal {
			v, ok := l.Get(name)
			if _, pending := m.edits.Get(name); !pending && (!ok || !v.Local() || p != nil && p.Kind == props.Readonly) {
				return false
			}
		}
		return true
	}
	seen := map[string]bool{}
	groups := props.ByGroup()
	for _, g := range props.GroupOrder {
		var rows []uiRow
		if g == props.GroupUser {
			var names []string
			for _, v := range l.Props {
				if props.IsUser(v.Name) {
					names = append(names, v.Name)
				}
			}
			for _, e := range m.edits {
				if props.IsUser(e.Prop) {
					if _, ok := l.Get(e.Prop); !ok {
						names = append(names, e.Prop)
					}
				}
			}
			sort.Strings(names)
			for _, n := range names {
				if seen[n] {
					continue
				}
				seen[n] = true
				p, _ := props.Lookup(n)
				if !match(n, p) {
					continue
				}
				v, ok := l.Get(n)
				rows = append(rows, uiRow{prop: p, name: n, val: v, have: ok})
			}
		} else {
			for _, p := range groups[g] {
				if p.Family {
					// the per-key members known in this session
					for _, v := range m.extra {
						if props.Family(v.Name) == p.Name && !seen[v.Name] && match(v.Name, p) {
							seen[v.Name] = true
							rows = append(rows, uiRow{prop: p, name: v.Name, val: v, have: true})
						}
					}
					for _, e := range m.edits {
						if props.Family(e.Prop) == p.Name && !seen[e.Prop] && match(e.Prop, p) {
							seen[e.Prop] = true
							rows = append(rows, uiRow{prop: p, name: e.Prop})
						}
					}
					continue
				}
				if !p.AppliesTo(l.Dataset.Type) {
					continue
				}
				if p.Linux && !m.showLinux {
					continue
				}
				v, ok := l.Get(p.Name)
				if !ok && p.Kind == props.Readonly {
					continue // statistics the kernel did not report for this type
				}
				seen[p.Name] = true
				if !match(p.Name, p) {
					continue
				}
				rows = append(rows, uiRow{prop: p, name: p.Name, val: v, have: ok})
			}
		}
		if len(rows) > 0 {
			m.rows = append(m.rows, uiRow{header: g})
			m.rows = append(m.rows, rows...)
		}
	}
	// unknown to the catalogue (a newer OpenZFS)
	var unknown []uiRow
	for _, v := range l.Props {
		if seen[v.Name] || props.IsUser(v.Name) {
			continue
		}
		if _, ok := props.Lookup(v.Name); ok {
			continue // a Linux-only or non-applicable one, hidden
		}
		p := &props.Prop{Name: v.Name, Kind: props.Settable, Type: props.TString, Group: props.GroupOther, Note: "not in the catalogue (a newer OpenZFS?); edited as free text"}
		if match(v.Name, p) {
			unknown = append(unknown, uiRow{prop: p, name: v.Name, val: v, have: true})
		}
	}
	if len(unknown) > 0 {
		m.rows = append(m.rows, uiRow{header: "Not in the catalogue"})
		m.rows = append(m.rows, unknown...)
	}
	m.clampCursor()
}

func (m *Model) listHeight() int { return max(3, m.height-9) }

func (m *Model) clampCursor() {
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// move moves the cursor by delta, skipping group headers.
func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	i := m.cursor + delta
	if i < 0 {
		i = 0
	}
	if i >= len(m.rows) {
		i = len(m.rows) - 1
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for i >= 0 && i < len(m.rows) && m.rows[i].header != "" {
		i += step
	}
	if i < 0 || i >= len(m.rows) {
		// ran off the end: the nearest property in the other direction
		i = m.cursor
	}
	m.cursor = i
	m.clampCursor()
}

func (m *Model) current() (uiRow, bool) {
	if m.cursor < len(m.rows) && m.rows[m.cursor].header == "" {
		return m.rows[m.cursor], true
	}
	return uiRow{}, false
}

// openPicker shows a filterable list; done gets the chosen value.
func (m *Model) openPicker(title, desc string, items []pickItem, initial string, done func(m *Model, value string) tea.Cmd, abort func(m *Model)) tea.Cmd {
	m.pk = newPicker(title, desc, items, initial, m.width, m.height)
	m.pkDone, m.pkAbort = done, abort
	m.prevScr, m.scr = m.scr, scrPicker
	return nil
}

func (m *Model) openForm(f *form, done func(m *Model) tea.Cmd, abort func(m *Model)) tea.Cmd {
	m.form, m.formDone, m.formAbort = f, done, abort
	m.form.resize(min(m.width, 110))
	m.prevScr, m.scr = m.scr, scrForm
	return m.form.Init()
}

// ---------------------------------------------------------------- update

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.Width, m.vp.Height = msg.Width, max(3, msg.Height-3)
		if m.ed != nil {
			m.ed.setSize(msg.Width, msg.Height)
		}
		if m.pk != nil {
			m.pk.setSize(msg.Width, msg.Height)
		}
		if m.form != nil {
			m.form.resize(min(msg.Width, 110))
		}
		m.clampCursor()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case datasetsMsg:
		if msg.err != nil {
			m.fatal = msg.err.Error()
			m.quitting = true
			return m, tea.Quit
		}
		m.datasets = msg.list
		m.pickCursor = 0
		for i, d := range msg.list {
			if d.Name == m.pickDef {
				m.pickCursor = i
			}
		}
		m.clampPick()
		return m, nil
	case loadedMsg:
		if msg.err != nil {
			if m.listing == nil {
				m.fatal = msg.err.Error()
				m.quitting = true
				return m, tea.Quit
			}
			m.scr = scrMain
			m.setError(msg.err.Error())
			return m, nil
		}
		m.listing, m.parent = msg.l, msg.parent
		m.dataset = msg.l.Dataset.Name
		m.edits, m.history = nil, nil
		// refresh the per-key extras
		if len(m.extra) > 0 {
			var names []string
			for _, v := range m.extra {
				names = append(names, v.Name)
			}
			if vals, err := props.GetProps(m.dataset, names); err == nil {
				m.extra = vals
			}
		}
		m.rebuildRows()
		m.move(0)
		if m.scr == scrBusy {
			m.scr = scrMain
		}
		return m, nil
	case appliedMsg:
		if len(msg.errs) > 0 {
			var b strings.Builder
			for _, e := range msg.errs {
				b.WriteString(e.Error() + "\n")
			}
			m.showView("Errors", styleErr.Render(fmt.Sprintf("%d of %d command(s) failed:", len(msg.errs), msg.n))+"\n\n"+b.String()+
				"\n"+styleMuted.Render("The state below is reloaded from the kernel."))
			m.prevScr = scrMain
		} else {
			m.scr = scrMain
			m.setStatus(fmt.Sprintf("Applied %d command(s)", msg.n))
		}
		return m, m.load()
	case treeMsg:
		m.scr = scrMain
		if msg.err != nil {
			m.setError(msg.err.Error())
			return m, nil
		}
		m.showView("Inheritance of "+msg.prop, m.treeText(msg))
		return m, nil
	case propMsg:
		m.scr = scrMain
		if msg.err != nil {
			m.setError(msg.err.Error())
			return m, nil
		}
		if msg.have {
			found := false
			for i, v := range m.extra {
				if v.Name == msg.name {
					m.extra[i], found = msg.val, true
				}
			}
			if !found {
				m.extra = append(m.extra, msg.val)
			}
		}
		p, _ := props.Lookup(msg.name)
		return m, m.openEditor(p, msg.name, msg.val, msg.have)
	}

	switch m.scr {
	case scrPick:
		return m.updatePick(msg)
	case scrMain:
		return m.updateMain(msg)
	case scrEditor:
		return m.updateEditor(msg)
	case scrPicker:
		switch m.pk.Update(msg) {
		case pickDone:
			m.scr = m.prevScr
			return m, m.pkDone(m, m.pk.Value())
		case pickCancel:
			m.scr = m.prevScr
			if m.pkAbort != nil {
				m.pkAbort(m)
			}
		}
		return m, nil
	case scrForm:
		return m.updateForm(msg)
	case scrPlan:
		return m.updatePlan(msg)
	case scrView:
		if k, ok := msg.(tea.KeyMsg); ok {
			switch k.String() {
			case "esc", "q", "?", "h", "t", "enter":
				m.scr = m.prevScr
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	case scrBusy:
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------- dataset picker

func (m *Model) pickVisible() []int {
	f := strings.ToLower(m.pickFilter)
	var idx []int
	for i, d := range m.datasets {
		if f == "" || strings.Contains(strings.ToLower(d.Name), f) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *Model) clampPick() {
	vis := m.pickVisible()
	if m.pickCursor >= len(vis) {
		m.pickCursor = len(vis) - 1
	}
	if m.pickCursor < 0 {
		m.pickCursor = 0
	}
	h := max(3, m.height-6)
	if m.pickCursor < m.pickOffset {
		m.pickOffset = m.pickCursor
	}
	if m.pickCursor >= m.pickOffset+h {
		m.pickOffset = m.pickCursor - h + 1
	}
}

func (m *Model) updatePick(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if m.pickTyping {
		switch k.String() {
		case "esc":
			m.pickFilter, m.pickTyping = "", false
		case "enter", "up", "down":
			m.pickTyping = false
			if k.String() != "enter" {
				return m.updatePick(msg)
			}
			return m.pickEnter()
		case "backspace":
			if m.pickFilter != "" {
				m.pickFilter = m.pickFilter[:len(m.pickFilter)-1]
			}
		default:
			if k.Type == tea.KeyRunes {
				m.pickFilter += k.String()
				m.pickCursor = 0
			}
		}
		m.clampPick()
		return m, nil
	}
	switch k.String() {
	case "esc", "q":
		if m.pickFilter != "" {
			m.pickFilter = ""
			m.clampPick()
			return m, nil
		}
		if m.listing != nil && !m.fromPicker {
			m.scr = scrMain
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "/":
		m.pickTyping = true
	case "up", "k":
		m.pickCursor--
	case "down", "j":
		m.pickCursor++
	case "pgup":
		m.pickCursor -= max(3, m.height-6)
	case "pgdown":
		m.pickCursor += max(3, m.height-6)
	case "home", "g":
		m.pickCursor = 0
	case "end", "G":
		m.pickCursor = len(m.pickVisible()) - 1
	case "enter":
		return m.pickEnter()
	case "?":
		m.showView("Help", helpText)
	}
	m.clampPick()
	return m, nil
}

func (m *Model) pickEnter() (tea.Model, tea.Cmd) {
	vis := m.pickVisible()
	if len(vis) == 0 {
		return m, nil
	}
	d := m.datasets[vis[m.pickCursor]]
	m.dataset = d.Name
	m.cursor, m.offset = 0, 0
	m.extra = nil
	m.scr, m.busyMsg = scrBusy, "Reading the properties of "+d.Name+"…"
	return m, m.load()
}

func (m *Model) pickView() string {
	var b strings.Builder
	b.WriteString(styleTitle.Width(m.width).Render("zfs-set — choose a dataset") + "\n")
	if m.datasets == nil {
		return b.String() + "\n  Listing datasets…\n"
	}
	f := ""
	if m.pickFilter != "" || m.pickTyping {
		f = "   filter: " + styleFocus.Render("/"+m.pickFilter)
		if m.pickTyping {
			f += styleFocus.Render("▏")
		}
	}
	b.WriteString(" " + styleMuted.Render(fmt.Sprintf("%d datasets (file systems and volumes)", len(m.datasets))) + f + "\n")
	vis := m.pickVisible()
	wName := 20
	for _, i := range vis {
		d := m.datasets[i]
		n := len([]rune(d.Name))
		if m.pickFilter == "" && d.Depth() > 0 {
			n = 2*d.Depth() + 2 + len([]rune(d.Name[strings.LastIndexByte(d.Name, '/')+1:]))
		}
		wName = max(wName, n)
	}
	wName = min(wName, max(20, m.width-2-11-24))
	wMount := max(10, m.width-wName-2-11-1)
	b.WriteString(styleHeader.Render(" "+fit("Dataset", wName)+" "+fit("Type", 10)+" "+fit("Mount point", wMount)) + "\n")
	h := max(3, m.height-6)
	for i := m.pickOffset; i < len(vis) && i < m.pickOffset+h; i++ {
		d := m.datasets[vis[i]]
		indent := strings.Repeat("  ", d.Depth())
		if m.pickFilter != "" {
			indent = ""
		}
		name := indent + d.Name
		if m.pickFilter == "" && d.Depth() > 0 {
			name = indent + "└ " + d.Name[strings.LastIndexByte(d.Name, '/')+1:]
		}
		typ := d.Type
		mp := d.Mountpoint
		if d.IsVolume() {
			typ, mp = "volume", "—"
		} else if d.Mounted {
			mp = d.Mountpoint
		} else if d.Mountpoint != "none" && d.Mountpoint != "legacy" {
			mp = d.Mountpoint + " (not mounted)"
		}
		if d.Name == m.pickDef {
			mp += " ●"
		}
		line := " " + fit(name, wName) + " " + fit(typ, 10) + " " + fit(mp, wMount)
		if i == m.pickCursor {
			line = styleSelected.Width(m.width).Render(line)
		} else if d.Name == m.pickDef {
			line = styleFocus.Render(line)
		}
		b.WriteString(line + "\n")
	}
	for i := len(vis) - m.pickOffset; i < h; i++ {
		b.WriteString("\n")
	}
	def := ""
	if m.pickDef != "" {
		def = "● = the dataset of the current directory   "
	}
	back := []string{"q/esc", "quit"}
	if m.listing != nil && !m.fromPicker {
		back = []string{"q/esc", "back to " + m.dataset}
	}
	b.WriteString(" " + styleMuted.Render(def) + helpLine(append([]string{"enter", "open", "/", "filter", "?", "help"}, back...)...))
	return b.String()
}

// ---------------------------------------------------------------- main

func (m *Model) updateMain(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok || m.listing == nil {
		return m, nil
	}
	if m.typing {
		switch k.String() {
		case "esc":
			m.filter, m.typing = "", false
			m.rebuildRows()
		case "enter", "up", "down":
			m.typing = false
			if k.String() != "enter" {
				return m.updateMain(msg)
			}
		case "backspace":
			if m.filter != "" {
				r := []rune(m.filter)
				m.filter = string(r[:len(r)-1])
				m.rebuildRows()
			}
		default:
			if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
				m.filter += k.String()
				m.cursor = 0
				m.rebuildRows()
				m.move(0)
			}
		}
		return m, nil
	}
	if k.Type == tea.KeyRunes && len(k.Runes) > 1 && k.Runes[0] == '/' {
		// a pasted "/text": filter by it
		m.filter, m.typing = string(k.Runes[1:]), true
		m.cursor = 0
		m.rebuildRows()
		m.move(0)
		return m, nil
	}
	switch k.String() {
	case "q", "esc", "backspace":
		if m.filter != "" {
			m.filter = ""
			m.rebuildRows()
			return m, nil
		}
		if m.modified() {
			return m, m.openForm(confirmForm("Discard pending edits?", fmt.Sprintf("%d edit(s) were not applied.", len(m.edits)), &m.formVals.yes),
				func(m *Model) tea.Cmd {
					if m.formVals.yes {
						m.edits, m.history = nil, nil
						m.rebuildRows()
						if m.fromPicker {
							return m.toPicker()
						}
						m.quitting = true
						return tea.Quit
					}
					m.scr = scrMain
					return nil
				}, nil)
		}
		if m.fromPicker {
			return m, m.toPicker()
		}
		m.quitting = true
		return m, tea.Quit
	case "?", "f1":
		m.showView("Help", helpText)
	case "h":
		m.showView("Keys", keymapView(m.width))
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-m.listHeight())
	case "pgdown":
		m.move(m.listHeight())
	case "home", "g":
		m.cursor = 0
		m.move(0)
	case "end", "G":
		m.cursor = len(m.rows) - 1
		m.move(0)
	case "/":
		m.typing = true
	case "x":
		m.showLinux = !m.showLinux
		m.rebuildRows()
		m.move(0)
	case "l":
		m.onlyLocal = !m.onlyLocal
		m.rebuildRows()
		m.move(0)
	case "enter", "e":
		if r, ok := m.current(); ok {
			return m, m.openEditor(r.prop, r.name, r.val, r.have)
		}
	case "i", "S":
		r, ok := m.current()
		if !ok {
			return m, nil
		}
		if r.prop.Kind != props.Settable {
			m.setError(r.name + " is " + r.prop.Kind.String())
			return m, nil
		}
		act := props.ActInherit
		if k.String() == "S" {
			if r.val.Received == "" {
				m.setError(r.name + " has no received value")
				return m, nil
			}
			act = props.ActRevert
		}
		if act == props.ActInherit && r.have && !r.val.Local() {
			if _, pending := m.edits.Get(r.name); !pending {
				m.setStatus(r.name + " is not set here (" + r.val.SourceLabel() + "): nothing to inherit")
				return m, nil
			}
		}
		m.push()
		m.edits = m.edits.Put(props.Edit{Prop: r.name, Action: act})
		m.rebuildRows()
		m.setStatus(fmt.Sprintf("%s: %s pending — A to apply", r.name, act))
	case "d":
		if r, ok := m.current(); ok {
			if _, pending := m.edits.Get(r.name); pending {
				m.push()
				m.edits = m.edits.Drop(r.name)
				m.rebuildRows()
				m.setStatus("Dropped the pending edit of " + r.name)
			} else {
				m.setStatus("No pending edit on " + r.name)
			}
		}
	case "a":
		m.formVals.s = ""
		return m, m.openForm(propNameForm(&m.formVals.s, func(n string) bool {
			for _, r := range m.rows {
				if r.name == n {
					return true
				}
			}
			return false
		}), func(m *Model) tea.Cmd {
			name := strings.TrimSpace(m.formVals.s)
			if props.IsUser(name) {
				p, _ := props.Lookup(name)
				m.scr = scrMain
				return m.openEditor(p, name, props.Value{Name: name, Source: "none"}, false)
			}
			// a per-key property: fetch its current value first
			ds := m.dataset
			m.scr, m.busyMsg = scrBusy, "Reading "+name+"…"
			return func() tea.Msg {
				vals, err := props.GetProps(ds, []string{name})
				if err != nil {
					return propMsg{name: name, err: err}
				}
				if len(vals) == 0 {
					return propMsg{name: name, val: props.Value{Name: name, Source: "none"}}
				}
				return propMsg{name: name, val: vals[0], have: true}
			}
		}, func(m *Model) { m.scr = scrMain })
	case "u":
		if n := len(m.history); n > 0 {
			m.edits = m.history[n-1]
			m.history = m.history[:n-1]
			m.rebuildRows()
			m.setStatus("Undone")
		} else {
			m.setStatus("Nothing to undo")
		}
	case "r":
		m.scr, m.busyMsg = scrBusy, "Reloading…"
		return m, m.load()
	case "A":
		return m, m.startApply()
	case "t":
		r, ok := m.current()
		if !ok {
			return m, nil
		}
		if r.prop.Kind == props.Readonly {
			m.setError(r.name + " is a statistic, not inherited")
			return m, nil
		}
		name, ds := r.name, m.dataset
		m.scr, m.busyMsg = scrBusy, "Looking up "+name+" in the tree…"
		return m, func() tea.Msg {
			chain, below, err := props.Tree(name, ds)
			return treeMsg{name, chain, below, err}
		}
	case "D":
		if m.modified() {
			m.setError("apply or undo the pending edits before switching dataset")
			return m, nil
		}
		return m, m.toPicker()
	}
	m.clampCursor()
	return m, nil
}

// toPicker shows the dataset list with the cursor on the current dataset.
func (m *Model) toPicker() tea.Cmd {
	m.pickDef = m.dataset
	m.pickFilter, m.pickTyping = "", false
	m.scr = scrPick
	if m.datasets == nil {
		return loadDatasets
	}
	for i, d := range m.datasets {
		if d.Name == m.dataset {
			m.pickCursor = i
		}
	}
	m.clampPick()
	return nil
}

func (m *Model) openEditor(p *props.Prop, name string, val props.Value, have bool) tea.Cmd {
	pending, hasPending := m.edits.Get(name)
	m.ed = newEditor(p, name, m.dataset, val, have, pending, hasPending, len(m.listing.Children), m.width, m.height)
	m.scr = scrEditor
	return nil
}

func (m *Model) updateEditor(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.ed.Update(msg) {
	case actCancel:
		m.scr = scrMain
	case actPickValue:
		var items []pickItem
		for _, o := range m.ed.choices {
			label := fmt.Sprintf("%-16s %s", o.Value, o.Note)
			if m.ed.have && o.Value == m.ed.cur.Value {
				label += "  ← current"
			}
			items = append(items, pickItem{strings.TrimRight(label, " "), o.Value})
		}
		return m, m.openPicker(m.ed.name+" value", m.ed.prop.Note, items, m.ed.value(), func(m *Model, v string) tea.Cmd {
			m.ed.SetValue(v)
			m.scr = scrEditor
			return nil
		}, func(m *Model) { m.scr = scrEditor })
	case actOK:
		e := m.ed.Result()
		m.push()
		// an edit that restores the current state is just dropped
		if e.Action == props.ActSet && m.ed.have && m.ed.cur.Local() && m.ed.cur.Value == e.Value && !e.Descend && !e.NoMount {
			m.edits = m.edits.Drop(e.Prop)
			m.setStatus(e.Prop + " already is " + e.Value + " here")
		} else {
			m.edits = m.edits.Put(e)
			m.setStatus(fmt.Sprintf("%s: %s pending — A to apply", e.Prop, describeEdit(e)))
		}
		m.rebuildRows()
		for i, r := range m.rows {
			if r.name == e.Prop {
				m.cursor = i
			}
		}
		m.clampCursor()
		m.scr = scrMain
	}
	return m, nil
}

func describeEdit(e props.Edit) string {
	switch e.Action {
	case props.ActInherit:
		if e.Descend {
			return "inherit (and every descendant)"
		}
		return "inherit"
	case props.ActRevert:
		return "revert to received"
	}
	s := "set to " + e.Value
	if e.Descend {
		s += " (children inherit it)"
	}
	if e.NoMount {
		s += " (-u)"
	}
	return s
}

func (m *Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "esc" {
		m.scr = m.prevScr
		if m.formAbort != nil {
			m.formAbort(m)
		}
		return m, nil
	}
	f, cmd := m.form.Update(msg)
	if hf, ok := f.(*huh.Form); ok {
		m.form.Form = hf
	}
	switch m.form.State {
	case huh.StateCompleted:
		m.scr = m.prevScr
		return m, m.formDone(m)
	case huh.StateAborted:
		m.scr = m.prevScr
		if m.formAbort != nil {
			m.formAbort(m)
		}
		return m, nil
	}
	return m, cmd
}

// ---------------------------------------------------------------- plan

func (m *Model) startApply() tea.Cmd {
	if !m.modified() {
		m.setStatus("No pending changes")
		return nil
	}
	l := m.listing.Clone()
	l.Props = append(l.Props, m.extra...)
	m.plan = props.Build(l, m.edits)
	m.probs = props.Preflight(l, m.edits, m.env)
	m.vp.SetContent(m.planText(l))
	m.vp.GotoTop()
	m.prevScr, m.scr = scrMain, scrPlan
	return nil
}

func (m *Model) updatePlan(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "n", "q":
			m.scr = scrMain
			return m, nil
		case "enter", "y":
			if m.plan.Empty() {
				m.scr = scrMain
				m.edits = nil
				m.rebuildRows()
				m.setStatus("Nothing to do")
				return m, nil
			}
			plan := m.plan
			n := len(plan.Steps)
			m.scr, m.busyMsg = scrBusy, fmt.Sprintf("Running %d zfs command(s)…", n)
			return m, func() tea.Msg {
				return appliedMsg{plan.Execute(nil), n}
			}
		}
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *Model) planText(l *props.Listing) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %d command(s) on %s\n\n", styleHeader.Render("Plan:"), len(m.plan.Steps), m.dataset)
	if m.plan.Empty() {
		b.WriteString(styleMuted.Render("  nothing: the pending edits change nothing") + "\n")
	}
	for i, s := range m.plan.Steps {
		fmt.Fprintf(&b, "%s %s\n      %s\n", styleFocus.Render(fmt.Sprintf("[%d/%d]", i+1, len(m.plan.Steps))), s, styleMuted.Render(wrapText(s.Desc, m.width-8, "      ")))
	}
	if len(m.probs) > 0 {
		b.WriteString("\n")
		fatal := 0
		for _, p := range m.probs {
			if p.Fatal {
				fatal++
			}
		}
		if fatal > 0 {
			b.WriteString(styleErr.Render(fmt.Sprintf("⚠ %d problem(s) that will make zfs refuse a command:", fatal)) + "\n")
		} else {
			b.WriteString(styleWarn.Render("Pre-flight notes:") + "\n")
		}
		for _, p := range m.probs {
			st := styleWarn
			if p.Fatal {
				st = styleErr
			}
			b.WriteString(st.Render("  • ") + wrapText(p.Msg, m.width-6, "    ") + "\n")
		}
	}
	b.WriteString("\n" + styleMuted.Render("Resulting values:") + "\n")
	after := props.Apply(l, m.edits, m.parent)
	for _, e := range m.edits {
		old, had := l.Get(e.Prop)
		nv, _ := after.Get(e.Prop)
		was := "not set"
		if had {
			was = old.Value + " (" + old.SourceLabel() + ")"
		}
		fmt.Fprintf(&b, "  %s %s → %s\n", fit(e.Prop, 24), styleMuted.Render(was), styleValue.Render(nv.Value)+styleMuted.Render(" ("+nv.SourceLabel()+")"))
	}
	return b.String()
}

// wrapText wraps s to width, indenting continuation lines.
func wrapText(s string, width int, indent string) string {
	if width < 20 {
		return s
	}
	words := strings.Fields(s)
	var b strings.Builder
	line := 0
	for i, w := range words {
		if i > 0 && line+1+len([]rune(w)) > width {
			b.WriteString("\n" + indent)
			line = len(indent)
		} else if i > 0 {
			b.WriteString(" ")
			line++
		}
		b.WriteString(w)
		line += len([]rune(w))
	}
	return b.String()
}

// ---------------------------------------------------------------- views

func (m *Model) showView(title, text string) {
	m.vpTitle = title
	m.vp.SetContent(text)
	m.vp.GotoTop()
	if m.scr != scrView {
		m.prevScr = m.scr
	}
	m.scr = scrView
}

func (m *Model) treeText(t treeMsg) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s from the pool down to %s:\n\n", styleHeader.Render(t.prop), m.dataset)
	for _, s := range t.chain {
		mark := "  "
		if s.Dataset == m.dataset {
			mark = "▶ "
		}
		line := fmt.Sprintf("%s%s %s", mark, fit(s.Dataset, 40), styleValue.Render(fit(s.Value, 24)))
		src := s.Source
		if s.Source == "local" || s.Source == "received" {
			line += " " + styleFocus.Render(src)
		} else {
			line += " " + styleMuted.Render(src)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	if len(t.below) == 0 {
		b.WriteString(styleMuted.Render("No descendant of "+m.dataset+" sets "+t.prop+" itself: they all inherit it.") + "\n")
	} else {
		fmt.Fprintf(&b, "%s\n", styleHeader.Render(fmt.Sprintf("%d descendant(s) set it themselves (a \"children inherit it\" edit would clear these):", len(t.below))))
		for _, s := range t.below {
			fmt.Fprintf(&b, "  %s %s %s\n", fit(s.Dataset, 40), styleValue.Render(fit(s.Value, 24)), styleMuted.Render(s.Source))
		}
	}
	return b.String()
}

func (m *Model) View() string {
	if m.quitting {
		if m.fatal != "" {
			return styleErr.Render("zfs-set: "+m.fatal) + "\n"
		}
		return ""
	}
	switch m.scr {
	case scrPick:
		return m.pickView()
	case scrEditor:
		return m.ed.View()
	case scrPicker:
		return m.pk.View()
	case scrForm:
		return m.frame(m.formTitle(), m.form.View(), "")
	case scrPlan:
		return m.frame("Apply changes to "+m.dataset, m.vp.View(), helpLine("y/enter", "run the commands", "esc", "back", "↑/↓", "scroll"))
	case scrView:
		return m.frame(m.vpTitle, m.vp.View(), helpLine("esc", "back", "↑/↓", "scroll"))
	case scrBusy:
		return m.frame("zfs-set", "\n  "+m.busyMsg+"\n", "")
	}
	return m.mainView()
}

func (m *Model) formTitle() string {
	if m.listing != nil {
		return "zfs-set — " + m.dataset
	}
	return "zfs-set"
}

func (m *Model) frame(title, body, help string) string {
	return styleTitle.Width(m.width).Render(title) + "\n" + body + "\n" + help
}

// sourceStyle colours a source: local/received stand out, the rest is muted.
func sourceStyle(v props.Value) string {
	s := v.SourceLabel()
	switch v.Source {
	case "local":
		return styleFocus.Render(s)
	case "received":
		return styleOK.Render(s)
	case "temporary":
		return styleWarn.Render(s)
	}
	return styleMuted.Render(s)
}

func (m *Model) mainView() string {
	if m.listing == nil {
		return styleTitle.Width(m.width).Render("zfs-set") + "\n\n  Loading " + m.dataset + "…\n"
	}
	var b strings.Builder
	b.WriteString(styleTitle.Width(m.width).Render("Properties of "+m.dataset) + "\n")
	d := m.listing.Dataset
	info := d.Type
	switch {
	case d.IsVolume():
		if v, ok := m.listing.Get("volsize"); ok {
			info = "volume, " + v.Value
		}
	case d.IsSnapshot():
		info = d.Type + " (only user properties can be set)"
	case d.Mounted:
		info += ", mounted at " + d.Mountpoint
	default:
		info += ", not mounted (mountpoint " + d.Mountpoint + ")"
	}
	var stats []string
	for _, n := range []string{"used", "available", "referenced", "compressratio"} {
		if v, ok := m.listing.Get(n); ok {
			stats = append(stats, n+" "+v.Value)
		}
	}
	b.WriteString(styleLabel.Render(" Dataset: ") + styleValue.Render(d.Name) + styleMuted.Render("   ("+info+")") + "\n")
	b.WriteString(styleLabel.Render(" Space:   ") + styleMuted.Render(fit(strings.Join(stats, "  ·  "), m.width-11)) + "\n")
	hdr := " Properties"
	if m.modified() {
		hdr += styleWarn.Render(fmt.Sprintf("  ● %d pending edit(s) — press A to apply", len(m.edits)))
	}
	var flags []string
	if m.filter != "" || m.typing {
		f := "filter: /" + m.filter
		if m.typing {
			f += "▏"
		}
		flags = append(flags, styleFocus.Render(f))
	}
	if m.onlyLocal {
		flags = append(flags, styleFocus.Render("only set here (l)"))
	}
	if m.showLinux {
		flags = append(flags, styleFocus.Render("showing Linux-only (x)"))
	}
	if len(flags) > 0 {
		hdr += "   " + strings.Join(flags, "  ")
	}
	b.WriteString(hdr + "\n")
	wName, wVal, wSrc := 24, 12, 22
	for _, r := range m.rows {
		if r.header == "" && r.have {
			wVal = max(wVal, min(30, len([]rune(r.val.Value))+2))
		}
		if e, ok := m.edits.Get(r.name); ok && e.Action == props.ActSet {
			wVal = max(wVal, min(30, len([]rune(e.Value))+4))
		}
	}
	wNote := m.width - wName - wVal - wSrc - 5
	if wNote < 16 {
		wNote = 0
		wVal = max(12, m.width-wName-wSrc-4)
	}
	head := " " + fit("Property", wName) + " " + fit("Value", wVal) + " " + fit("Source", wSrc)
	if wNote > 0 {
		head += " " + fit("Meaning", wNote)
	}
	b.WriteString(styleHeader.Render(head) + "\n")
	h := m.listHeight()
	for i := m.offset; i < len(m.rows) && i < m.offset+h; i++ {
		r := m.rows[i]
		if r.header != "" {
			b.WriteString(styleFocus.Render(" "+r.header) + "\n")
			continue
		}
		// plain and styled forms of the value and the source
		pval, psrc := r.val.Value, r.val.SourceLabel()
		val, src := pval, sourceStyle(r.val)
		if !r.have {
			pval, psrc = "not set", "-"
			val, src = styleMuted.Render(pval), styleMuted.Render(psrc)
		}
		if r.val.Received != "" && r.val.Source != "received" {
			psrc += " (received " + r.val.Received + ")"
			src += styleMuted.Render(" (received " + r.val.Received + ")")
		}
		if e, ok := m.edits.Get(r.name); ok {
			switch e.Action {
			case props.ActSet:
				pval = "→ " + e.Value
			case props.ActInherit:
				pval = "→ inherit"
			case props.ActRevert:
				pval = "→ " + r.val.Received + " (received)"
			}
			if e.Descend {
				pval += " ↓"
			}
			val = styleWarn.Render(fit(pval, wVal))
		} else {
			val = fitAnsi(val, wVal)
		}
		name := r.name
		if r.prop.Kind != props.Settable {
			name += " ⊘"
		}
		note := ""
		if wNote > 0 {
			note = " " + fit(r.prop.Note, wNote)
		}
		var line string
		switch {
		case i == m.cursor:
			line = styleSelected.Width(m.width).Render(" " + fit(name, wName) + " " + fit(pval, wVal) + " " + fit(psrc, wSrc) + note)
		case r.prop.Kind != props.Settable:
			line = " " + styleMuted.Render(fit(name, wName)) + " " + val + " " + fitAnsi(src, wSrc) + styleMuted.Render(note)
		default:
			line = " " + fit(name, wName) + " " + val + " " + fitAnsi(src, wSrc) + styleMuted.Render(note)
		}
		b.WriteString(line + "\n")
	}
	if len(m.rows) == 0 {
		b.WriteString(styleMuted.Render("   (nothing matches)") + "\n")
	}
	for i := max(len(m.rows)-m.offset, 1); i < h; i++ {
		b.WriteString("\n")
	}
	// detail of the current row
	detail := ""
	if r, ok := m.current(); ok {
		detail = r.prop.Note
		var extra []string
		if r.prop.Kind == props.CreateOnly {
			extra = append(extra, "fixed at creation")
		}
		if r.prop.Kind == props.Readonly {
			extra = append(extra, "read-only")
		}
		if r.prop.Default != "" && !props.IsUser(r.name) && r.prop.Kind == props.Settable {
			extra = append(extra, "default "+r.prop.Default)
		}
		if r.prop.Feature != "" {
			extra = append(extra, "pool feature "+r.prop.Feature)
		}
		if r.prop.Linux {
			extra = append(extra, "no effect on FreeBSD")
		}
		if r.prop.NewData {
			extra = append(extra, "new data only")
		}
		if r.val.Received != "" {
			extra = append(extra, "received "+r.val.Received)
		}
		if len(extra) > 0 {
			detail += "  [" + strings.Join(extra, " · ") + "]"
		}
	}
	b.WriteString(" " + styleMuted.Render(fit(detail, m.width-2)) + "\n")
	status := m.status
	if m.errMsg != "" {
		status = styleErr.Render(m.errMsg)
	} else if status != "" {
		status = styleOK.Render(status)
	}
	if status != "" {
		b.WriteString(" " + status + "\n")
	} else {
		b.WriteString(helpLine("enter", "edit", "i", "inherit", "S", "received", "a", "add", "A", "apply", "u", "undo", "t", "tree", "/", "filter", "l", "local", "D", "datasets", "?", "help", "q/esc", m.backLabel()) + "\n")
	}
	return b.String()
}

// fitAnsi pads a possibly styled string to w cells by its visible width
// (it never truncates styled text; plain text is cut by fit).
func fitAnsi(s string, w int) string {
	if !strings.ContainsRune(s, '\x1b') {
		return fit(s, w)
	}
	vis := visibleLen(s)
	if vis >= w {
		return s
	}
	return s + strings.Repeat(" ", w-vis)
}

// visibleLen counts the cells of s without its SGR sequences.
func visibleLen(s string) int {
	n := 0
	in := false
	for _, r := range s {
		switch {
		case in:
			if r == 'm' {
				in = false
			}
		case r == '\x1b':
			in = true
		default:
			n++
		}
	}
	return n
}

func (m *Model) backLabel() string {
	if m.fromPicker {
		return "back"
	}
	return "quit"
}

// Run starts the TUI. dataset "" opens the picker with pickDef preselected.
func Run(dataset, pickDef string) error {
	m := New(dataset, pickDef)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(*Model); ok && fm.fatal != "" {
		fmt.Fprintln(os.Stderr, "zfs-set:", fm.fatal)
		os.Exit(1)
	}
	return nil
}
