package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/olgeni/zfs-set/props"
)

func kmsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestEditorEnum: the radio list starts on the current value; enter on a
// value selects it and confirms.
func TestEditorEnum(t *testing.T) {
	l := fakeListing()
	v, _ := l.Get("sync")
	p, _ := props.Lookup("sync")
	ed := newEditor(p, "sync", "tank/home", v, true, props.Edit{}, false, 2, 100, 30)
	if r := ed.row(); r.kind != erChoice || ed.choices[r.idx].Value != "standard" {
		t.Fatalf("cursor not on the current value: %+v", r)
	}
	ed.Update(kmsg("down"))
	if a := ed.Update(kmsg("enter")); a != actOK {
		t.Fatalf("enter on a value: %v", a)
	}
	e := ed.Result()
	if e.Prop != "sync" || e.Action != props.ActSet || e.Value != "always" {
		t.Errorf("result %+v", e)
	}
	// the descend option exists (2 children, inheritable) and toggles
	ed = newEditor(p, "sync", "tank/home", v, true, props.Edit{}, false, 2, 100, 30)
	for ed.row().kind != erDescend {
		ed.Update(kmsg("down"))
	}
	ed.Update(kmsg("space"))
	ed.Update(kmsg("tab"))
	if ed.row().kind != erButtons {
		t.Fatal("tab did not reach the buttons")
	}
	if a := ed.Update(kmsg("enter")); a != actOK || !ed.Result().Descend || ed.Result().Value != "standard" {
		t.Errorf("descend: %v %+v", a, ed.Result())
	}
}

// TestEditorInput: a typed value replaces the pre-filled one and is validated.
func TestEditorInput(t *testing.T) {
	l := fakeListing()
	v, _ := l.Get("quota")
	p, _ := props.Lookup("quota")
	ed := newEditor(p, "quota", "tank/home", v, true, props.Edit{}, false, 2, 100, 30)
	if !ed.onInput() || ed.input != "500G" || !ed.pristine {
		t.Fatalf("start: input %q pristine %v on %v", ed.input, ed.pristine, ed.row())
	}
	ed.Update(kmsg("1"))
	ed.Update(kmsg("0"))
	ed.Update(kmsg("X"))
	if ed.input != "10X" {
		t.Fatalf("typed %q", ed.input)
	}
	if a := ed.Update(kmsg("enter")); a != actNone || ed.errMsg == "" {
		t.Fatalf("bad value accepted: %v %q", a, ed.errMsg)
	}
	ed.Update(kmsg("backspace"))
	ed.Update(kmsg("T"))
	if a := ed.Update(kmsg("ctrl+s")); a != actOK || ed.Result().Value != "10T" {
		t.Errorf("ok: %v %+v", a, ed.Result())
	}
	// quota is not inheritable: the second action is "reset", no descend for set
	if len(ed.actions) != 2 {
		t.Errorf("actions %v", ed.actions)
	}
	ed.Update(kmsg("up"))
	if ed.row().kind != erAction || ed.row().idx != 1 {
		t.Fatalf("not on the inherit action: %+v", ed.row())
	}
	if a := ed.Update(kmsg("enter")); a != actOK || ed.Result().Action != props.ActInherit {
		t.Errorf("inherit via enter: %v %+v", a, ed.Result())
	}
}

// TestEditorRevert: a received value adds the third action.
func TestEditorRevert(t *testing.T) {
	l := fakeListing()
	v, _ := l.Get("compression")
	p, _ := props.Lookup("compression")
	ed := newEditor(p, "compression", "tank/home", v, true, props.Edit{}, false, 2, 100, 30)
	if len(ed.actions) != 3 || ed.actions[2] != props.ActRevert {
		t.Fatalf("actions %v", ed.actions)
	}
	if ed.inline() || ed.row().kind != erPick {
		t.Fatalf("compression should use the picker: %+v", ed.row())
	}
	if a := ed.Update(kmsg("enter")); a != actPickValue {
		t.Fatalf("enter on the value row: %v", a)
	}
	ed.SetValue("gzip-9")
	if a := ed.Update(kmsg("ctrl+s")); a != actOK || ed.Result().Value != "gzip-9" {
		t.Errorf("picked: %v %+v", a, ed.Result())
	}
}

// TestMainRows: the table hides Linux-only properties until x, lands on a
// property (not a header) and shows pending edits.
func TestMainRows(t *testing.T) {
	m := New("tank/home", "")
	m.width, m.height = 120, 40
	m.listing = fakeListing()
	m.rebuildRows()
	m.move(0)
	if r, ok := m.current(); !ok || r.name != "mountpoint" {
		t.Fatalf("first row %+v %v", r, ok)
	}
	names := map[string]bool{}
	for _, r := range m.rows {
		names[r.name] = true
	}
	if names["nbmand"] || names["context"] || !names["com.example:backup"] || !names["used"] || !names["casesensitivity"] {
		t.Errorf("rows: %v", names)
	}
	m.showLinux = true
	m.rebuildRows()
	found := false
	for _, r := range m.rows {
		if r.name == "nbmand" {
			found = true
		}
	}
	if !found {
		t.Error("x did not show nbmand")
	}
	m.filter = "acl"
	m.rebuildRows()
	for _, r := range m.rows {
		if r.header == "" && r.name != "acltype" && r.name != "aclmode" && r.name != "aclinherit" {
			t.Errorf("filter acl shows %s", r.name)
		}
	}
	m.filter = ""
	m.onlyLocal = true
	m.rebuildRows()
	for _, r := range m.rows {
		if r.header == "" && !r.val.Local() {
			t.Errorf("only-local shows %s (%s)", r.name, r.val.Source)
		}
	}
}
