package props

import (
	"fmt"
	"strings"
)

// Action is what an edit does to a property.
type Action int

const (
	ActSet     Action = iota // zfs set prop=value
	ActInherit               // zfs inherit prop: clear the local value, inherit or default
	ActRevert                // zfs inherit -S prop: back to the received value
)

func (a Action) String() string {
	switch a {
	case ActInherit:
		return "inherit"
	case ActRevert:
		return "revert to received"
	}
	return "set"
}

// Edit is one pending change to one property of the dataset.
type Edit struct {
	Prop    string
	Action  Action
	Value   string // for ActSet
	Descend bool   // inherit: zfs inherit -r (the dataset and every descendant); set: the children then inherit it (zfs inherit -r on each child)
	NoMount bool   // zfs set -u: change mountpoint/sharenfs/sharesmb without mounting or sharing
}

// Edits is the ordered working set of pending changes, one per property.
type Edits []Edit

// Get returns the pending edit of prop.
func (es Edits) Get(prop string) (Edit, bool) {
	for _, e := range es {
		if e.Prop == prop {
			return e, true
		}
	}
	return Edit{}, false
}

// Put adds or replaces the edit of e.Prop, keeping the order.
func (es Edits) Put(e Edit) Edits {
	for i := range es {
		if es[i].Prop == e.Prop {
			es[i] = e
			return es
		}
	}
	return append(es, e)
}

// Drop removes the edit of prop.
func (es Edits) Drop(prop string) Edits {
	var res Edits
	for _, e := range es {
		if e.Prop != prop {
			res = append(res, e)
		}
	}
	return res
}

// Clone copies the edits.
func (es Edits) Clone() Edits { return append(Edits(nil), es...) }

// Step is one zfs invocation.
type Step struct {
	Args []string // argv after "zfs"
	Desc string
}

// String renders the step as a shell command line.
func (s Step) String() string { return CommandLine(s.Args) }

// CommandLine renders "zfs ARGS…" with shell quoting.
func CommandLine(args []string) string {
	parts := []string{"zfs"}
	for _, a := range args {
		parts = append(parts, ShellQuote(a))
	}
	return strings.Join(parts, " ")
}

// Plan is the ordered list of commands that apply a set of edits.
type Plan struct {
	Dataset string
	Steps   []Step
}

// Empty reports whether there is nothing to do.
func (p *Plan) Empty() bool { return p == nil || len(p.Steps) == 0 }

// Commands returns the steps as shell command lines.
func (p *Plan) Commands() []string {
	var res []string
	for _, s := range p.Steps {
		res = append(res, s.String())
	}
	return res
}

func (p *Plan) add(desc string, args ...string) {
	p.Steps = append(p.Steps, Step{Args: args, Desc: desc})
}

// Build turns the edits into zfs set / zfs inherit commands on the listing's
// dataset: one command per edit (so a refused value fails alone and the plan
// reads one line per change), in edit order. Edits that would change
// nothing (setting the value the dataset already has locally, inheriting a
// property that is not local) are skipped.
func Build(l *Listing, edits Edits) *Plan {
	ds := l.Dataset.Name
	p := &Plan{Dataset: ds}
	for _, e := range edits {
		cur, have := l.Get(e.Prop)
		switch e.Action {
		case ActSet:
			if have && cur.Local() && cur.Value == e.Value && !e.Descend && !e.NoMount {
				continue
			}
			args := []string{"set"}
			if e.NoMount {
				args = append(args, "-u")
			}
			args = append(args, e.Prop+"="+e.Value, ds)
			was := "unset"
			if have {
				was = cur.Value + ", " + cur.SourceLabel()
			}
			desc := fmt.Sprintf("set %s to %s (was %s)", e.Prop, e.Value, was)
			if e.NoMount {
				desc += "; property only, nothing is mounted or shared now"
			}
			p.add(desc, args...)
			if e.Descend {
				for _, c := range l.Children {
					p.add(fmt.Sprintf("%s and everything below it inherit %s again", c.Name, e.Prop), "inherit", "-r", e.Prop, c.Name)
				}
			}
		case ActInherit, ActRevert:
			if have && !cur.Local() && !e.Descend && e.Action == ActInherit {
				continue
			}
			args := []string{"inherit"}
			pr, known := Lookup(e.Prop)
			resettable := known && !pr.Inherit && !IsUser(e.Prop) // zfs inherit refuses non-inheritable properties: -S resets them to the default
			if e.Action == ActRevert || resettable {
				args = append(args, "-S")
			}
			if e.Descend {
				args = append(args, "-r")
			}
			args = append(args, e.Prop, ds)
			var desc string
			if e.Action == ActRevert {
				desc = fmt.Sprintf("revert %s to the received value", e.Prop)
				if have && cur.Received != "" {
					desc += " " + cur.Received
				}
			} else {
				desc = fmt.Sprintf("clear %s here", e.Prop)
				if have && cur.Local() {
					desc = fmt.Sprintf("clear the local %s=%s", e.Prop, cur.Value)
				}
				if resettable {
					desc += " (not inheritable: back to the default, or the received value)"
				} else {
					desc += " (inherit from the parent, or the default)"
				}
			}
			if e.Descend {
				desc += ", on the dataset and every descendant"
			}
			p.add(desc, args...)
		}
	}
	return p
}

// Execute runs the steps in order; it does not stop at the first failure
// (later steps are independent) and returns all errors.
func (p *Plan) Execute(progress func(done, total int)) []error {
	var errs []error
	for i, s := range p.Steps {
		if _, err := Run(s.Args...); err != nil {
			errs = append(errs, fmt.Errorf("%s: %v", s, err))
		}
		if progress != nil {
			progress(i+1, len(p.Steps))
		}
	}
	return errs
}

// Apply returns the listing as it will look after the edits (for what-if
// displays): set values become local, inherited ones take the parent's
// value when known, else "?".
func Apply(l *Listing, edits Edits, parent *Listing) *Listing {
	n := l.Clone()
	for _, e := range edits {
		cur, have := n.Get(e.Prop)
		if !have {
			cur = Value{Name: e.Prop, Source: "none"}
		}
		switch {
		case e.Action == ActSet:
			cur.Value, cur.Raw, cur.Source, cur.From = e.Value, e.Value, "local", ""
		case e.Action == ActRevert && cur.Received != "":
			cur.Value, cur.Raw, cur.Source, cur.From = cur.Received, cur.Received, "received", ""
		default: // inherit (or revert without a received value)
			pr, _ := Lookup(e.Prop)
			if parent != nil {
				if pv, ok := parent.Get(e.Prop); ok && (pr == nil || pr.Inherit || IsUser(e.Prop)) {
					cur.Value, cur.Raw = pv.Value, pv.Raw
					if pv.Local() {
						cur.Source, cur.From = "inherited", parent.Dataset.Name
					} else {
						cur.Source, cur.From = pv.Source, pv.From
					}
					break
				}
			}
			if pr != nil && pr.Default != "" && !IsUser(e.Prop) {
				cur.Value, cur.Raw, cur.Source, cur.From = pr.Default, pr.Default, "default", ""
			} else {
				cur.Value, cur.Raw, cur.Source, cur.From = "?", "?", "inherited", "?"
			}
		}
		n.Set(cur)
	}
	return n
}
