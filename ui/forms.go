package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/olgeni/zfs-set/props"
)

// form is a one-group huh form that remembers its group so resize can fix
// the group's height (huh sizes the group's viewport once, at its default
// width).
type form struct {
	*huh.Form
	group *huh.Group
}

func newForm(fields ...huh.Field) *form {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	g := huh.NewGroup(fields...)
	return &form{Form: huh.NewForm(g).WithTheme(huhTheme()).WithKeyMap(km).WithShowHelp(true), group: g}
}

func (f *form) resize(width int) {
	f.Form.WithWidth(width)
	h := lipgloss.Height(f.group.Content())
	for _, s := range []string{f.group.Header(), f.group.Footer()} {
		if s != "" {
			h += lipgloss.Height(s)
		}
	}
	f.Form.WithHeight(h)
}

// propNameForm asks for a property to add: a user property (module:name) or
// a per-key one (userquota@NAME …).
func propNameForm(value *string, existing func(string) bool) *form {
	in := huh.NewInput().
		Title("Property name").
		Description("A user property (module:name, e.g. com.example:backup) or a per-key one: userquota@USER, groupquota@GROUP, projectquota@ID, userobjquota@USER …").
		Placeholder("com.example:backup").
		Value(value).
		Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return fmt.Errorf("type a name")
			}
			if props.IsUser(s) {
				if !props.ValidUserName(s) {
					return fmt.Errorf("lowercase letters, digits, : - . _ with a colon, at most 256 characters")
				}
			} else {
				p, ok := props.Lookup(s)
				if !ok {
					return fmt.Errorf("not a property; user properties need a colon (module:name)")
				}
				if p.Family && p.Name == s {
					return fmt.Errorf("add the key: %sNAME", p.Name)
				}
				if !p.Family {
					return fmt.Errorf("%s is already in the list — edit it there", p.Name)
				}
				if p.Kind == props.Readonly {
					return fmt.Errorf("%s is read-only", p.Name)
				}
			}
			if existing(s) {
				return fmt.Errorf("%s is already in the list", s)
			}
			return nil
		})
	return newForm(in)
}

// confirmForm is a yes/no question.
func confirmForm(title, desc string, value *bool) *form {
	c := huh.NewConfirm().Title(title).Description(desc).Affirmative("Yes").Negative("No").Value(value)
	return newForm(c)
}
