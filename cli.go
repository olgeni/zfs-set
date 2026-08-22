package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"sort"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/olgeni/zfs-set/props"
)

type cliOptions struct {
	list      bool
	local     bool
	get       string
	set       multi
	inherit   multi
	revert    bool
	recursive bool
	noMount   bool
	tree      string
	where     string
	dump      bool
	restore   string
	catalogue bool
	describe  string
	dryRun    bool
	yes       bool
	check     bool
	json      bool
	lenient   bool
}

func (o cliOptions) nonInteractive() bool {
	return o.list || o.get != "" || len(o.set) > 0 || len(o.inherit) > 0 || o.tree != "" || o.where != "" || o.dump || o.restore != "" || o.catalogue || o.describe != "" || o.dryRun || o.check
}

func fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "zfs-set: "+format+"\n", a...)
	return 1
}

// runCLI executes the non-interactive modes and returns the exit status.
func runCLI(dataset string, o cliOptions) int {
	switch {
	case o.catalogue:
		return runCatalogue(o)
	case o.describe != "":
		return runDescribe(o)
	case o.where != "":
		return runWhere(o)
	case o.restore != "":
		return runRestore(o)
	}
	l, err := props.Load(dataset)
	if err != nil {
		return fail("%v", err)
	}
	switch {
	case o.list:
		return runList(l, o)
	case o.get != "":
		return runGet(l, o)
	case o.tree != "":
		return runTree(l, o)
	case o.dump:
		return runDump(l, o)
	case len(o.set) > 0 || len(o.inherit) > 0:
		return runEdit(l, o)
	}
	return fail("nothing to do (-list, -get, -set, -inherit, -tree, -where, -dump, -restore, -catalogue, -describe)")
}

func cliEnv(o cliOptions) props.Env {
	env := props.Env{Root: os.Geteuid() == 0, Lenient: o.lenient, Mountd: props.MountdRunning()}
	if u, err := user.Current(); err == nil {
		env.User = u.Username
	}
	env.Cwd, _ = os.Getwd()
	return env
}

// runEdit builds the edits from -set/-inherit and runs the plan.
func runEdit(l *props.Listing, o cliOptions) int {
	var edits props.Edits
	for _, s := range o.set {
		i := strings.IndexByte(s, '=')
		if i <= 0 {
			return fail("-set wants PROP=VALUE, got %q", s)
		}
		name, value := props.Canonical(strings.TrimSpace(s[:i])), s[i+1:]
		p, ok := props.Lookup(name)
		if !ok && !o.lenient {
			return fail("%s: not a known property (user properties need a colon: module:name); -lenient passes it through", name)
		}
		if ok {
			if p.Family && p.Name == name {
				return fail("%s needs a key: %sNAME", name, name)
			}
			if v, err := p.Validate(value); err != nil && !o.lenient {
				return fail("%v", err)
			} else if err == nil {
				value = v
			}
		}
		edits = edits.Put(props.Edit{Prop: name, Action: props.ActSet, Value: value, Descend: o.recursive, NoMount: o.noMount})
	}
	for _, s := range o.inherit {
		name := props.Canonical(strings.TrimSpace(s))
		if _, ok := props.Lookup(name); !ok && !o.lenient {
			return fail("%s: not a known property", name)
		}
		act := props.ActInherit
		if o.revert {
			act = props.ActRevert
		}
		edits = edits.Put(props.Edit{Prop: name, Action: act, Descend: o.recursive})
	}
	// per-key properties are not in zfs get all: fetch them so the plan and
	// the pre-flight see the current value
	var extra []string
	for _, e := range edits {
		if props.Family(e.Prop) != "" {
			extra = append(extra, e.Prop)
		}
	}
	if vals, err := props.GetProps(l.Dataset.Name, extra); err == nil {
		for _, v := range vals {
			l.Set(v)
		}
	}
	plan := props.Build(l, edits)
	probs := props.Preflight(l, edits, cliEnv(o))
	return runPlan(l, plan, probs, o)
}

// runPlan prints, confirms and runs a plan (the common tail of the editing modes).
func runPlan(l *props.Listing, plan *props.Plan, probs []props.Problem, o cliOptions) int {
	if o.check {
		if plan.Empty() {
			return 0
		}
		return 3
	}
	if o.json && o.dryRun {
		return printJSON(jsonPlan(plan, probs))
	}
	if plan.Empty() {
		fmt.Println("Nothing to do.")
		return 0
	}
	for _, s := range plan.Steps {
		fmt.Println(s)
	}
	fatal := printProblems(probs)
	if o.dryRun {
		return 0
	}
	if fatal > 0 && !o.yes {
		fmt.Fprintf(os.Stderr, "%d of the commands will be refused by zfs.\n", fatal)
	}
	if !o.yes && !confirm(fmt.Sprintf("Run %d command(s) on %s?", len(plan.Steps), l.Dataset.Name)) {
		return 2
	}
	errs := plan.Execute(nil)
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "zfs-set:", e)
	}
	if len(errs) > 0 {
		return 1
	}
	fmt.Fprintf(os.Stderr, "Applied %d command(s).\n", len(plan.Steps))
	return 0
}

func printProblems(probs []props.Problem) int {
	fatal := 0
	for _, p := range probs {
		tag := "note"
		if p.Fatal {
			tag = "warning"
			fatal++
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", tag, p.Msg)
	}
	return fatal
}

func confirm(q string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", q)
	ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(ans)); a == "y" || a == "yes" {
		return true
	}
	fmt.Fprintln(os.Stderr, "Declined.")
	return false
}

// termWidth is the terminal width for the tables (0 when not a terminal).
func termWidth() int {
	if ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil && ws.Col > 0 {
		return int(ws.Col)
	}
	return 0
}

func trunc(s string, w int) string {
	if w <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// runList prints every property in catalogue order, with its meaning.
func runList(l *props.Listing, o cliOptions) int {
	if o.json {
		return printJSON(jsonListing(l, o.local))
	}
	width := termWidth()
	type row struct {
		name, value, source, note string
	}
	var rows []row
	seen := map[string]bool{}
	var headers []int
	add := func(v props.Value, p *props.Prop) {
		if o.local && !v.Local() {
			return
		}
		src := v.SourceLabel()
		if v.Received != "" && v.Source != "received" {
			src += " (received " + v.Received + ")"
		}
		note := ""
		if p != nil {
			note = p.Note
			if p.Kind != props.Settable {
				note = "[" + p.Kind.String() + "] " + note
			}
		}
		rows = append(rows, row{v.Name, v.Value, src, note})
	}
	groups := props.ByGroup()
	for _, g := range props.GroupOrder {
		start := len(rows)
		headers = append(headers, start)
		if g == props.GroupUser {
			for _, v := range l.Props {
				if props.IsUser(v.Name) {
					p, _ := props.Lookup(v.Name)
					add(v, p)
					seen[v.Name] = true
				}
			}
		} else {
			for _, p := range groups[g] {
				if v, ok := l.Get(p.Name); ok {
					seen[v.Name] = true
					add(v, p)
				}
			}
		}
		if len(rows) == start {
			headers = headers[:len(headers)-1]
			continue
		}
		rows = append(rows[:start], append([]row{{name: "# " + g}}, rows[start:]...)...)
	}
	var unknown []row
	for _, v := range l.Props {
		if !seen[v.Name] {
			if o.local && !v.Local() {
				continue
			}
			unknown = append(unknown, row{v.Name, v.Value, v.SourceLabel(), "(not in the catalogue)"})
		}
	}
	if len(unknown) > 0 {
		rows = append(rows, row{name: "# Not in the catalogue"})
		rows = append(rows, unknown...)
	}
	wName, wVal, wSrc := 8, 5, 6
	for _, r := range rows {
		if strings.HasPrefix(r.name, "# ") {
			continue
		}
		wName = max(wName, len([]rune(r.name)))
		wVal = max(wVal, min(40, len([]rune(r.value))))
		wSrc = max(wSrc, min(40, len([]rune(r.source))))
	}
	wNote := 0
	if width > 0 {
		wNote = width - wName - wVal - wSrc - 3
	}
	fmt.Printf("%-*s %-*s %-*s %s\n", wName, "PROPERTY", wVal, "VALUE", wSrc, "SOURCE", "MEANING")
	for _, r := range rows {
		if strings.HasPrefix(r.name, "# ") {
			fmt.Println(r.name)
			continue
		}
		fmt.Printf("%-*s %-*s %-*s %s\n", wName, r.name, wVal, trunc(r.value, 40), wSrc, trunc(r.source, 40), trunc(r.note, wNote))
	}
	return 0
}

// runGet prints the named properties with their meaning.
func runGet(l *props.Listing, o cliOptions) int {
	var names []string
	for _, n := range strings.Split(o.get, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, props.Canonical(n))
		}
	}
	var missing []string
	for _, n := range names {
		if _, ok := l.Get(n); !ok {
			missing = append(missing, n)
		}
	}
	if vals, err := props.GetProps(l.Dataset.Name, missing); err == nil {
		for _, v := range vals {
			l.Set(v)
		}
	} else if len(missing) > 0 {
		return fail("%v", err)
	}
	var out []props.Value
	for _, n := range names {
		v, ok := l.Get(n)
		if !ok {
			return fail("%s: no such property on %s", n, l.Dataset.Name)
		}
		out = append(out, v)
	}
	if o.json {
		ll := &props.Listing{Dataset: l.Dataset, Props: out}
		return printJSON(jsonListing(ll, false))
	}
	for _, v := range out {
		p, _ := props.Lookup(v.Name)
		fmt.Printf("%s\t%s\t%s", v.Name, v.Value, v.SourceLabel())
		if v.Received != "" {
			fmt.Printf("\treceived %s", v.Received)
		}
		fmt.Println()
		if p != nil {
			fmt.Printf("\t%s\n", p.Note)
			if p.Default != "" && !props.IsUser(v.Name) {
				fmt.Printf("\tdefault %s", p.Default)
				if !p.Inherit && p.Kind == props.Settable {
					fmt.Printf(", not inherited")
				}
				if p.Kind != props.Settable {
					fmt.Printf(", %s", p.Kind)
				}
				fmt.Println()
			}
		}
	}
	return 0
}

// runTree prints the property up and down the tree.
func runTree(l *props.Listing, o cliOptions) int {
	prop := props.Canonical(o.tree)
	chain, below, err := props.Tree(prop, l.Dataset.Name)
	if err != nil {
		return fail("%v", err)
	}
	if o.json {
		return printJSON(jsonTree(prop, l.Dataset.Name, chain, below))
	}
	fmt.Printf("%s from the pool down to %s:\n", prop, l.Dataset.Name)
	for _, s := range chain {
		mark := "  "
		if s.Dataset == l.Dataset.Name {
			mark = "> "
		}
		fmt.Printf("%s%-40s %-24s %s\n", mark, s.Dataset, s.Value, s.Source)
	}
	if len(below) == 0 {
		fmt.Printf("No descendant sets %s itself.\n", prop)
		return 0
	}
	fmt.Printf("%d descendant(s) set it themselves:\n", len(below))
	for _, s := range below {
		fmt.Printf("  %-40s %-24s %s\n", s.Dataset, s.Value, s.Source)
	}
	return 0
}

// runWhere lists every dataset that sets the property itself.
func runWhere(o cliOptions) int {
	prop := props.Canonical(o.where)
	if _, ok := props.Lookup(prop); !ok && !o.lenient {
		return fail("%s: not a known property", prop)
	}
	hits, err := props.Where(prop)
	if err != nil {
		return fail("%v", err)
	}
	if o.json {
		return printJSON(jsonWhere(prop, hits))
	}
	if len(hits) == 0 {
		fmt.Printf("No dataset sets %s itself.\n", prop)
		return 0
	}
	for _, s := range hits {
		fmt.Printf("%-40s %-24s %s\n", s.Dataset, s.Value, s.Source)
	}
	return 0
}

// snapshot is the -dump format: the properties a dataset sets itself.
type snapshot struct {
	Dataset    string              `json:"dataset"`
	Type       string              `json:"type"`
	Properties map[string]snapProp `json:"properties"`
}

type snapProp struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

func takeSnapshot(l *props.Listing) snapshot {
	s := snapshot{Dataset: l.Dataset.Name, Type: l.Dataset.Type, Properties: map[string]snapProp{}}
	for _, v := range l.Props {
		if !v.Local() {
			continue
		}
		if p, ok := props.Lookup(v.Name); ok && p.Kind != props.Settable {
			continue
		}
		s.Properties[v.Name] = snapProp{v.Value, v.Source}
	}
	return s
}

// runDump prints the properties the dataset (and, with -r, every
// descendant) sets itself, as a JSON snapshot.
func runDump(l *props.Listing, o cliOptions) int {
	var snaps []snapshot
	if o.recursive {
		list, err := props.Descendants(l.Dataset.Name)
		if err != nil {
			return fail("%v", err)
		}
		for _, d := range list {
			vals, err := props.Props(d.Name)
			if err != nil {
				fmt.Fprintln(os.Stderr, "zfs-set: warning:", err)
				continue
			}
			snaps = append(snaps, takeSnapshot(&props.Listing{Dataset: d, Props: vals}))
		}
	} else {
		snaps = append(snaps, takeSnapshot(l))
	}
	return printJSON(snaps)
}

// runRestore brings every dataset of a -dump snapshot back to the recorded
// local properties: recorded values are set when they differ, local values
// that were not recorded are inherited.
func runRestore(o cliOptions) int {
	b, err := os.ReadFile(o.restore)
	if err != nil {
		return fail("%v", err)
	}
	var snaps []snapshot
	if err := json.Unmarshal(b, &snaps); err != nil {
		return fail("%s: %v", o.restore, err)
	}
	type item struct {
		l    *props.Listing
		plan *props.Plan
	}
	var items []item
	var allProbs []props.Problem
	env := cliEnv(o)
	for _, s := range snaps {
		l, err := props.Load(s.Dataset)
		if err != nil {
			fmt.Fprintln(os.Stderr, "zfs-set: warning:", err)
			continue
		}
		var edits props.Edits
		names := make([]string, 0, len(s.Properties))
		for n := range s.Properties {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			want := s.Properties[n]
			cur, ok := l.Get(n)
			if ok && cur.Local() && cur.Value == want.Value {
				continue
			}
			edits = edits.Put(props.Edit{Prop: n, Action: props.ActSet, Value: want.Value})
		}
		for _, v := range l.Props {
			if !v.Local() {
				continue
			}
			if _, recorded := s.Properties[v.Name]; recorded {
				continue
			}
			if p, ok := props.Lookup(v.Name); ok && p.Kind != props.Settable {
				continue
			}
			edits = edits.Put(props.Edit{Prop: v.Name, Action: props.ActInherit})
		}
		plan := props.Build(l, edits)
		if plan.Empty() {
			continue
		}
		items = append(items, item{l, plan})
		for _, p := range props.Preflight(l, edits, env) {
			p.Msg = s.Dataset + ": " + p.Msg
			allProbs = append(allProbs, p)
		}
	}
	n := 0
	for _, it := range items {
		n += len(it.plan.Steps)
	}
	if o.check {
		if n == 0 {
			return 0
		}
		return 3
	}
	if o.json && o.dryRun {
		all := &props.Plan{}
		for _, it := range items {
			all.Steps = append(all.Steps, it.plan.Steps...)
		}
		return printJSON(jsonPlan(all, allProbs))
	}
	if n == 0 {
		fmt.Println("Nothing to do.")
		return 0
	}
	for _, it := range items {
		for _, s := range it.plan.Steps {
			fmt.Println(s)
		}
	}
	printProblems(allProbs)
	if o.dryRun {
		return 0
	}
	if !o.yes && !confirm(fmt.Sprintf("Run %d command(s) on %d dataset(s)?", n, len(items))) {
		return 2
	}
	failed := 0
	for _, it := range items {
		for _, e := range it.plan.Execute(nil) {
			fmt.Fprintln(os.Stderr, "zfs-set:", e)
			failed++
		}
	}
	if failed > 0 {
		return 1
	}
	fmt.Fprintf(os.Stderr, "Restored %d command(s) on %d dataset(s).\n", n, len(items))
	return 0
}

// runCatalogue prints every property with kind, values and meaning.
func runCatalogue(o cliOptions) int {
	if o.json {
		return printJSON(jsonCatalogue())
	}
	groups := props.ByGroup()
	for _, g := range props.GroupOrder {
		if g == props.GroupUser {
			fmt.Printf("\n%s\n  module:name            settable  any string, inherited, never interpreted by ZFS (zfs inherit removes it)\n", g)
			continue
		}
		fmt.Printf("\n%s\n", g)
		for _, p := range groups[g] {
			kind := p.Kind.String()
			if p.Linux {
				kind += ", no effect on FreeBSD"
			}
			fmt.Printf("  %-22s %-14s %s\n", p.Name, kind, p.Note)
		}
	}
	return 0
}

// runDescribe prints everything about one property.
func runDescribe(o cliOptions) int {
	name := props.Canonical(o.describe)
	p, ok := props.Lookup(name)
	if !ok {
		return fail("%s: not a known property", name)
	}
	if o.json {
		return printJSON(jsonProp(p))
	}
	fmt.Printf("%s  (%s", p.Name, p.Kind)
	if p.Kind == props.Settable {
		if p.Inherit || props.IsUser(name) {
			fmt.Printf(", inherited")
		} else {
			fmt.Printf(", not inherited")
		}
	}
	fmt.Printf("; %s)\n", p.TypesLabel())
	if p.Short != "" {
		fmt.Printf("  alias: %s\n", p.Short)
	}
	fmt.Printf("  %s\n", p.Note)
	if p.Detail != "" {
		fmt.Printf("  %s\n", p.Detail)
	}
	if p.Default != "" && !props.IsUser(name) {
		fmt.Printf("  default: %s\n", p.Default)
	}
	if p.Feature != "" {
		fmt.Printf("  pool feature: %s\n", p.Feature)
	}
	if p.Linux {
		fmt.Printf("  no effect on FreeBSD\n")
	}
	if p.NewData {
		fmt.Printf("  affects only data written after the change\n")
	}
	if ch := p.Choices(); len(ch) > 0 {
		fmt.Printf("  values:\n")
		for _, c := range ch {
			if c.Note != "" {
				fmt.Printf("    %-18s %s\n", c.Value, c.Note)
			} else {
				fmt.Printf("    %s\n", c.Value)
			}
		}
	} else if h := p.Hint(); h != "" {
		fmt.Printf("  value: %s\n", h)
	}
	return 0
}
