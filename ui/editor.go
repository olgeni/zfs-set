package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/olgeni/zfs-set/props"
)

// edAction is what the editor asks the root model to do after a key.
type edAction int

const (
	actNone edAction = iota
	actCancel
	actOK
	actPickValue // open the filterable picker for a long value list
)

type rowKind int

const (
	erAction  rowKind = iota // one of the actions (set / inherit / revert)
	erChoice                 // one allowed value (radio)
	erPick                   // "Value: X (enter to choose)" for long lists
	erInput                  // typed value
	erDescend                // [ ] children inherit it / every descendant
	erNoMount                // [ ] zfs set -u
	erButtons                // OK / Cancel
)

type edRow struct {
	kind rowKind
	idx  int // action index or choice index
}

// maxInline is the longest value list shown as radio rows; longer ones
// (compression) open the picker.
const maxInline = 14

// editor edits one property: the action, the value, the options.
type editor struct {
	prop    *props.Prop
	name    string // full name (userquota@bob, com.example:x)
	dataset string
	cur     props.Value
	have    bool
	edit    props.Edit
	nChild  int
	choices []props.Option
	actions []props.Action

	rows      []edRow
	cursor    int
	offset    int
	input     string
	pristine  bool // the input still holds the pre-filled current value: typing replaces it
	choiceIdx int  // selected choice, -1 none
	btn       int  // focused button: 0 OK, 1 Cancel
	errMsg    string
	width     int
	height    int
}

func newEditor(p *props.Prop, name, dataset string, cur props.Value, have bool, pending props.Edit, hasPending bool, nChild, w, h int) *editor {
	e := &editor{prop: p, name: name, dataset: dataset, cur: cur, have: have, nChild: nChild, width: w, height: h, choiceIdx: -1}
	e.choices = p.Choices()
	e.actions = []props.Action{props.ActSet}
	if p.Kind == props.Settable {
		e.actions = append(e.actions, props.ActInherit)
		if cur.Received != "" {
			e.actions = append(e.actions, props.ActRevert)
		}
	}
	e.edit = props.Edit{Prop: name, Action: props.ActSet}
	if hasPending {
		e.edit = pending
	}
	// the starting value: the pending edit's, else the current one
	start := ""
	if hasPending && pending.Action == props.ActSet {
		start = pending.Value
	} else if have {
		start = cur.Value
	}
	if e.choices != nil {
		for i, o := range e.choices {
			if o.Value == start {
				e.choiceIdx = i
			}
		}
		if e.choiceIdx < 0 && start != "" && !p.FreeForm() {
			// a value outside the list (e.g. sharenfs options): keep it as typed text
			e.input = start
		}
	} else {
		e.input = start
	}
	if p.FreeForm() && p.Type != props.TBool && p.Type != props.TEnum && p.Type != props.TCompress && p.Type != props.TDedup && p.Type != props.TVersion {
		e.input = start
	}
	e.pristine = e.input != ""
	e.rebuild()
	// cursor on the current choice when editing an enum, else on the first value row
	for i, r := range e.rows {
		if r.kind == erChoice && r.idx == e.choiceIdx || r.kind == erInput || r.kind == erPick {
			e.cursor = i
			break
		}
	}
	e.clamp()
	return e
}

func (e *editor) setSize(w, h int) { e.width, e.height = w, h; e.clamp() }

func (e *editor) canDescend() bool {
	if e.prop.Kind != props.Settable {
		return false
	}
	if e.edit.Action == props.ActSet {
		return (e.prop.Inherit || props.IsUser(e.name)) && e.nChild > 0
	}
	return e.nChild > 0
}

func (e *editor) canNoMount() bool {
	return e.edit.Action == props.ActSet && (e.name == "mountpoint" || e.name == "sharenfs" || e.name == "sharesmb")
}

// inline reports whether the value list is shown as radio rows.
func (e *editor) inline() bool { return e.choices != nil && len(e.choices) <= maxInline }

func (e *editor) rebuild() {
	e.rows = e.rows[:0]
	for i := range e.actions {
		e.rows = append(e.rows, edRow{erAction, i})
	}
	if e.edit.Action == props.ActSet && e.prop.Kind == props.Settable {
		switch {
		case e.inline():
			for i := range e.choices {
				e.rows = append(e.rows, edRow{erChoice, i})
			}
			if e.prop.Type == props.TShare || e.prop.Type == props.TKeyloc {
				e.rows = append(e.rows, edRow{erInput, 0})
			}
		case e.choices != nil:
			e.rows = append(e.rows, edRow{erPick, 0})
		default:
			e.rows = append(e.rows, edRow{erInput, 0})
		}
	}
	if e.canDescend() {
		e.rows = append(e.rows, edRow{erDescend, 0})
	}
	if e.canNoMount() {
		e.rows = append(e.rows, edRow{erNoMount, 0})
	}
	e.rows = append(e.rows, edRow{erButtons, 0})
	e.clamp()
}

func (e *editor) headerLines() int {
	n := 1 + len(e.descLines()) + 2 // title, description, current/received line, blank
	if e.errMsg != "" {
		n++
	}
	return n
}

func (e *editor) listHeight() int { return max(3, e.height-e.headerLines()-2) }

func (e *editor) clamp() {
	if e.cursor >= len(e.rows) {
		e.cursor = len(e.rows) - 1
	}
	if e.cursor < 0 {
		e.cursor = 0
	}
	h := e.listHeight()
	if e.cursor < e.offset {
		e.offset = e.cursor
	}
	if e.cursor >= e.offset+h {
		e.offset = e.cursor - h + 1
	}
	if e.offset < 0 {
		e.offset = 0
	}
}

func (e *editor) row() edRow {
	if e.cursor < len(e.rows) {
		return e.rows[e.cursor]
	}
	return edRow{erButtons, 0}
}

// onInput reports whether the cursor is on the typed-value row (keys go to it).
func (e *editor) onInput() bool { return e.row().kind == erInput }

// SetValue sets the value chosen in the picker.
func (e *editor) SetValue(v string) {
	for i, o := range e.choices {
		if o.Value == v {
			e.choiceIdx = i
		}
	}
	e.edit.Action = props.ActSet
	e.errMsg = ""
}

// value is the value the editor would set: the radio choice, else the text.
func (e *editor) value() string {
	if e.inline() || e.choices != nil {
		if e.choiceIdx >= 0 && e.choiceIdx < len(e.choices) && (e.input == "" || !e.inline()) {
			return e.choices[e.choiceIdx].Value
		}
	}
	return strings.TrimSpace(e.input)
}

// Result is the edit to record (valid only after actOK).
func (e *editor) Result() props.Edit {
	r := e.edit
	r.Prop = e.name
	if r.Action == props.ActSet {
		r.Value = e.value()
	} else {
		r.Value = ""
	}
	if !e.canDescend() {
		r.Descend = false
	}
	if !e.canNoMount() {
		r.NoMount = false
	}
	return r
}

func (e *editor) ok() edAction {
	if e.edit.Action == props.ActSet {
		if e.prop.Kind != props.Settable {
			e.errMsg = e.name + " cannot be set"
			return actNone
		}
		v := e.value()
		if v == "" {
			e.errMsg = "choose or type a value"
			return actNone
		}
		nv, err := e.prop.Validate(v)
		if err != nil {
			e.errMsg = err.Error()
			return actNone
		}
		if e.choices == nil || !e.inline() && e.choiceIdx < 0 || e.input != "" && e.inline() {
			e.input = nv
		}
	}
	return actOK
}

// Update handles a key.
func (e *editor) Update(msg tea.Msg) edAction {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return actNone
	}
	key := k.String()
	if e.onInput() {
		switch key {
		case "backspace":
			if e.pristine {
				e.input, e.pristine = "", false
			} else if e.input != "" {
				r := []rune(e.input)
				e.input = string(r[:len(r)-1])
			}
			e.errMsg = ""
			e.clamp()
			return actNone
		case "ctrl+u":
			e.input = ""
			return actNone
		case "ctrl+w":
			e.input = strings.TrimRight(e.input, " ")
			if i := strings.LastIndexByte(e.input, ' '); i >= 0 {
				e.input = e.input[:i+1]
			} else {
				e.input = ""
			}
			return actNone
		}
		if k.Type == tea.KeyRunes || k.Type == tea.KeySpace {
			if e.pristine {
				e.input, e.pristine = "", false
			}
			e.input += key
			e.errMsg = ""
			if e.inline() {
				e.choiceIdx = -1 // typed text wins over the radio
			}
			return actNone
		}
	}
	switch key {
	case "esc", "ctrl+c":
		return actCancel
	case "q":
		if !e.onInput() {
			return actCancel
		}
	case "ctrl+s", "f10":
		return e.ok()
	case "up", "ctrl+p":
		e.cursor--
	case "down", "ctrl+n":
		e.cursor++
	case "k":
		if !e.onInput() {
			e.cursor--
		}
	case "j":
		if !e.onInput() {
			e.cursor++
		}
	case "pgup":
		e.cursor -= e.listHeight()
	case "pgdown":
		e.cursor += e.listHeight()
	case "home", "g":
		if !e.onInput() {
			e.cursor = 0
		}
	case "end", "G":
		if !e.onInput() {
			e.cursor = len(e.rows) - 1
		}
	case "tab":
		// jump to the buttons (or from them back to the top)
		if e.row().kind == erButtons {
			e.cursor = 0
		} else {
			e.cursor = len(e.rows) - 1
		}
	case "left", "right", "h", "l":
		if e.row().kind == erButtons {
			e.btn = 1 - e.btn
		}
	case " ", "enter":
		return e.activate(key == "enter")
	}
	e.clamp()
	return actNone
}

// activate selects/toggles the row under the cursor; enter also confirms
// where that is the natural thing.
func (e *editor) activate(enter bool) edAction {
	r := e.row()
	e.errMsg = ""
	switch r.kind {
	case erAction:
		e.edit.Action = e.actions[r.idx]
		e.rebuild()
		if enter {
			if e.edit.Action == props.ActSet {
				// move to the first value row
				for i, rr := range e.rows {
					if rr.kind == erChoice || rr.kind == erInput || rr.kind == erPick {
						e.cursor = i
						break
					}
				}
			} else {
				return e.ok()
			}
		}
	case erChoice:
		e.choiceIdx = r.idx
		e.input = ""
		if enter {
			return e.ok()
		}
	case erPick:
		return actPickValue
	case erInput:
		if enter {
			return e.ok()
		}
	case erDescend:
		e.edit.Descend = !e.edit.Descend
	case erNoMount:
		e.edit.NoMount = !e.edit.NoMount
	case erButtons:
		if e.btn == 0 {
			return e.ok()
		}
		return actCancel
	}
	e.clamp()
	return actNone
}

// cut truncates s to w cells (by rune count) without padding.
func cut(s string, w int) string {
	if r := []rune(s); len(r) > w {
		return fit(s, w)
	}
	return s
}

// descLines wraps the note and the detail to the width.
func (e *editor) descLines() []string {
	w := max(30, e.width-3)
	var lines []string
	for _, para := range []string{e.prop.Note, e.prop.Detail} {
		if para == "" {
			continue
		}
		lines = append(lines, strings.Split(wrapText(para, w, ""), "\n")...)
	}
	extra := []string{}
	if e.prop.Kind == props.CreateOnly {
		extra = append(extra, "fixed at creation")
	}
	if e.prop.Kind == props.Readonly {
		extra = append(extra, "read-only")
	}
	if e.prop.Default != "" && !props.IsUser(e.name) {
		extra = append(extra, "default "+e.prop.Default)
	}
	if !e.prop.Inherit && e.prop.Kind == props.Settable && !props.IsUser(e.name) {
		extra = append(extra, "not inherited")
	}
	if e.prop.Feature != "" {
		extra = append(extra, "pool feature "+e.prop.Feature)
	}
	if e.prop.Linux {
		extra = append(extra, "no effect on FreeBSD")
	}
	if e.prop.NewData {
		extra = append(extra, "new data only")
	}
	if e.prop.Types != "" && e.prop.Types != "all" {
		extra = append(extra, e.prop.TypesLabel())
	}
	if len(extra) > 0 {
		lines = append(lines, strings.Join(extra, " · "))
	}
	if len(lines) > 6 {
		lines = lines[:6]
	}
	return lines
}

func (e *editor) View() string {
	var b strings.Builder
	b.WriteString(styleTitle.Width(e.width).Render(e.name+" on "+e.dataset) + "\n")
	for _, l := range e.descLines() {
		b.WriteString(" " + styleMuted.Render(l) + "\n")
	}
	cur := styleLabel.Render(" Current: ")
	if e.have {
		cur += styleValue.Render(e.cur.Value) + styleMuted.Render(" ("+e.cur.SourceLabel()+")")
		if e.cur.Received != "" {
			cur += styleMuted.Render("   received: ") + e.cur.Received
		}
	} else {
		cur += styleMuted.Render("not set")
	}
	b.WriteString(cur + "\n")
	if e.errMsg != "" {
		b.WriteString(" " + styleErr.Render(e.errMsg) + "\n")
	}
	b.WriteString("\n")
	h := e.listHeight()
	for i := e.offset; i < len(e.rows) && i < e.offset+h; i++ {
		r := e.rows[i]
		var line string
		switch r.kind {
		case erAction:
			mark := "( )"
			if e.edit.Action == e.actions[r.idx] {
				mark = "(•)"
			}
			var txt string
			switch e.actions[r.idx] {
			case props.ActSet:
				txt = "Set a value"
				if e.prop.Kind != props.Settable {
					txt = "Set a value — not possible: " + e.prop.Kind.String()
				}
			case props.ActInherit:
				if e.prop.Inherit || props.IsUser(e.name) {
					txt = "Inherit from the parent (zfs inherit: clears the value set here; the default if no ancestor sets it)"
				} else {
					txt = "Reset to the default (zfs inherit -S: " + e.name + " is not inheritable)"
				}
			case props.ActRevert:
				txt = "Revert to the received value " + e.cur.Received + " (zfs inherit -S)"
			}
			line = " " + mark + " " + txt
		case erChoice:
			mark := "( )"
			if e.choiceIdx == r.idx && e.input == "" {
				mark = "(•)"
			}
			o := e.choices[r.idx]
			line = "     " + mark + " " + fit(o.Value, 16)
			tail := ""
			if e.have && o.Value == e.cur.Value {
				tail = "  ← current"
			}
			if o.Note != "" {
				line += " " + styleMuted.Render(cut(o.Note, e.width-len([]rune(line))-len([]rune(tail))-2))
			}
			line += styleMuted.Render(tail)
		case erPick:
			v := e.value()
			if v == "" {
				v = "(choose)"
			}
			line = "     Value: " + styleValue.Render(v) + styleMuted.Render("   enter to choose from the list")
		case erInput:
			label := "     Value: "
			if e.inline() {
				label = "     or type: "
			}
			if e.pristine {
				line = label + styleMuted.Render(e.input)
			} else {
				line = label + styleFocus.Render(e.input)
			}
			if i == e.cursor {
				line += styleFocus.Render("▏")
			}
			if e.pristine && i == e.cursor {
				line += styleMuted.Render(" (type to replace)")
			}
			if hint := e.prop.Hint(); hint != "" {
				line += styleMuted.Render("   " + hint)
			}
		case erDescend:
			mark := "[ ]"
			if e.edit.Descend {
				mark = "[x]"
			}
			if e.edit.Action == props.ActSet {
				line = fmt.Sprintf(" %s The %d child dataset(s) and everything below inherit it too (zfs inherit -r %s on each child: their own values are cleared)", mark, e.nChild, e.name)
			} else {
				line = fmt.Sprintf(" %s On every descendant too (zfs inherit -r: %d child dataset(s) and everything below)", mark, e.nChild)
			}
		case erNoMount:
			mark := "[ ]"
			if e.edit.NoMount {
				mark = "[x]"
			}
			what := "mounting or unmounting"
			if e.name != "mountpoint" {
				what = "sharing or unsharing"
			}
			line = " " + mark + " Change the property only, without " + what + " now (zfs set -u)"
		case erButtons:
			ok, cancel := styleButton.Render("OK"), styleButton.Render("Cancel")
			if i == e.cursor && e.btn == 0 {
				ok = styleButtonOn.Render("OK")
			} else if i == e.cursor {
				cancel = styleButtonOn.Render("Cancel")
			}
			line = "   " + ok + " " + cancel
		}
		if r.kind == erAction || r.kind == erDescend || r.kind == erNoMount {
			line = fit(line, e.width-1)
		}
		if i == e.cursor && r.kind != erButtons && r.kind != erInput {
			line = styleSelected.Width(e.width).Render(line)
		}
		b.WriteString(line + "\n")
	}
	for i := max(len(e.rows)-e.offset, 1); i < h; i++ {
		b.WriteString("\n")
	}
	b.WriteString(helpLine("↑/↓", "move", "space", "select", "enter", "select+OK", "^S", "OK", "esc", "cancel"))
	return b.String()
}
