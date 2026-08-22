package props

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// ZFSCommand and ZpoolCommand are the binaries; tests may point them elsewhere.
var (
	ZFSCommand   = "zfs"
	ZpoolCommand = "zpool"
)

// Run runs zfs with args and returns its standard output; on failure the
// error carries zfs's message.
func Run(args ...string) (string, error) { return run(ZFSCommand, args...) }

func run(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		if errors.Is(err, exec.ErrNotFound) {
			msg = bin + " command not found"
		}
		return out.String(), fmt.Errorf("%s", msg)
	}
	return out.String(), nil
}

// Dataset is a dataset as listed by zfs list.
type Dataset struct {
	Name       string
	Type       string // filesystem, volume, snapshot, bookmark
	Mountpoint string // "-" for volumes, "none"/"legacy" or a path
	Mounted    bool
}

// IsVolume reports whether the dataset is a zvol.
func (d Dataset) IsVolume() bool { return d.Type == "volume" }

// IsSnapshot reports whether the dataset is a snapshot or bookmark.
func (d Dataset) IsSnapshot() bool { return d.Type == "snapshot" || d.Type == "bookmark" }

// Depth is the number of / in the name (pool = 0).
func (d Dataset) Depth() int { return strings.Count(d.Name, "/") }

// Pool is the pool the dataset belongs to.
func (d Dataset) Pool() string { return PoolOf(d.Name) }

// PoolOf returns the pool part of a dataset name.
func PoolOf(name string) string {
	if i := strings.IndexAny(name, "/@#"); i >= 0 {
		return name[:i]
	}
	return name
}

// ParentOf returns the parent dataset name ("" for a pool). A snapshot's
// parent is its file system.
func ParentOf(name string) string {
	if i := strings.IndexAny(name, "@#"); i >= 0 {
		return name[:i]
	}
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[:i]
	}
	return ""
}

// Ancestors returns the ancestor dataset names, nearest first.
func Ancestors(name string) []string {
	var res []string
	for p := ParentOf(name); p != ""; p = ParentOf(p) {
		res = append(res, p)
	}
	return res
}

func parseList(out string) []Dataset {
	var res []Dataset
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 4 {
			continue
		}
		res = append(res, Dataset{Name: f[0], Type: f[1], Mountpoint: f[2], Mounted: f[3] == "yes"})
	}
	return res
}

// Datasets lists every file system and volume, sorted by name.
func Datasets() ([]Dataset, error) {
	out, err := Run("list", "-H", "-p", "-o", "name,type,mountpoint,mounted", "-t", "filesystem,volume", "-s", "name")
	if err != nil {
		return nil, err
	}
	return parseList(out), nil
}

// Descendants lists name and every file system and volume below it, sorted.
func Descendants(name string) ([]Dataset, error) {
	out, err := Run("list", "-H", "-p", "-r", "-o", "name,type,mountpoint,mounted", "-t", "filesystem,volume", "-s", "name", name)
	if err != nil {
		return nil, err
	}
	return parseList(out), nil
}

// Children lists the direct children (file systems and volumes) of name.
func Children(name string) ([]Dataset, error) {
	out, err := Run("list", "-H", "-p", "-d", "1", "-o", "name,type,mountpoint,mounted", "-t", "filesystem,volume", "-s", "name", name)
	if err != nil {
		return nil, err
	}
	var res []Dataset
	for _, d := range parseList(out) {
		if d.Name != name {
			res = append(res, d)
		}
	}
	return res, nil
}

// Info returns the dataset's type and mount point, or an error if it does
// not exist. Snapshots and bookmarks are accepted.
func Info(name string) (Dataset, error) {
	out, err := Run("list", "-H", "-p", "-o", "name,type,mountpoint,mounted", "-t", "all", name)
	if err != nil {
		return Dataset{}, err
	}
	l := parseList(out)
	if len(l) == 0 {
		return Dataset{}, fmt.Errorf("%s: not a dataset", name)
	}
	return l[0], nil
}

// Value is one property of a dataset as zfs get reports it.
type Value struct {
	Name     string
	Value    string // human form (128K, 10.5G, on)
	Raw      string // zfs get -p form (bytes, epoch seconds); == Value for strings
	Source   string // local, default, inherited, temporary, received, none ("-")
	From     string // the ancestor for inherited
	Received string // the received value, "" if none
}

// Local reports whether the value is set on the dataset itself (local or
// received — both are cleared by zfs inherit).
func (v Value) Local() bool { return v.Source == "local" || v.Source == "received" }

// SourceLabel is the source as zfs get prints it.
func (v Value) SourceLabel() string {
	switch v.Source {
	case "inherited":
		return "inherited from " + v.From
	case "none":
		return "-"
	}
	return v.Source
}

// Listing is what the tool works on: a dataset and its properties in zfs
// get order, plus the pool features (for the pre-flight).
type Listing struct {
	Dataset  Dataset
	Props    []Value
	Features map[string]string // feature name -> disabled/enabled/active
	Children []Dataset         // direct children (for "descendants inherit" edits)
}

// Get returns the property by name (aliases resolved).
func (l *Listing) Get(name string) (Value, bool) {
	name = Canonical(name)
	for _, v := range l.Props {
		if v.Name == name {
			return v, true
		}
	}
	return Value{}, false
}

func (l *Listing) has(name string) bool { _, ok := l.Get(name); return ok }

// Set replaces or appends a property value in the listing.
func (l *Listing) Set(v Value) {
	for i := range l.Props {
		if l.Props[i].Name == v.Name {
			l.Props[i] = v
			return
		}
	}
	l.Props = append(l.Props, v)
}

// Clone copies the listing (its property slice).
func (l *Listing) Clone() *Listing {
	c := *l
	c.Props = append([]Value(nil), l.Props...)
	return &c
}

// ParseGet parses "zfs get -H -o property,value,source,received" output
// (one dataset) into values; raw is the matching -p output (property,value)
// and may be "".
func ParseGet(out, raw string) []Value {
	rawv := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		f := strings.SplitN(sc.Text(), "\t", 2)
		if len(f) == 2 {
			rawv[f[0]] = f[1]
		}
	}
	var res []Value
	sc = bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 3 {
			continue
		}
		v := Value{Name: f[0], Value: f[1], Source: f[2]}
		if len(f) > 3 && f[3] != "-" {
			v.Received = f[3]
		}
		if strings.HasPrefix(v.Source, "inherited from ") {
			v.From = strings.TrimPrefix(v.Source, "inherited from ")
			v.Source = "inherited"
		}
		if v.Source == "-" {
			v.Source = "none"
		}
		if r, ok := rawv[v.Name]; ok {
			v.Raw = r
		} else {
			v.Raw = v.Value
		}
		res = append(res, v)
	}
	return res
}

// Props reads every property of the dataset.
func Props(name string) ([]Value, error) {
	out, err := Run("get", "-H", "-o", "property,value,source,received", "all", name)
	if err != nil {
		return nil, err
	}
	raw, _ := Run("get", "-H", "-p", "-o", "property,value", "all", name)
	return ParseGet(out, raw), nil
}

// GetProps reads the named properties of the dataset (for families:
// userquota@bob, and anything zfs get all omits).
func GetProps(name string, props []string) ([]Value, error) {
	if len(props) == 0 {
		return nil, nil
	}
	list := strings.Join(props, ",")
	out, err := Run("get", "-H", "-o", "property,value,source,received", list, name)
	if err != nil {
		return nil, err
	}
	raw, _ := Run("get", "-H", "-p", "-o", "property,value", list, name)
	return ParseGet(out, raw), nil
}

// Load reads the dataset's properties, its pool's features and its children.
func Load(name string) (*Listing, error) {
	ds, err := Info(name)
	if err != nil {
		return nil, err
	}
	vals, err := Props(ds.Name)
	if err != nil {
		return nil, err
	}
	l := &Listing{Dataset: ds, Props: vals}
	l.Features, _ = PoolFeatures(ds.Pool())
	if !ds.IsSnapshot() {
		l.Children, _ = Children(ds.Name)
	}
	return l, nil
}

// PoolFeatures returns feature@NAME -> state for the pool.
func PoolFeatures(pool string) (map[string]string, error) {
	out, err := run(ZpoolCommand, "get", "-H", "-o", "property,value", "all", pool)
	if err != nil {
		return nil, err
	}
	res := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) == 2 && strings.HasPrefix(f[0], "feature@") {
			res[strings.TrimPrefix(f[0], "feature@")] = f[1]
		}
	}
	return res, nil
}

// Spot is the value of one property on one dataset (for the inheritance
// view and -where).
type Spot struct {
	Dataset string
	Value   string
	Source  string // as printed: local, default, inherited from X, received, temporary, -
}

// Tree returns the property on every ancestor (root first), on the dataset,
// and on every descendant that sets it itself (local/received).
func Tree(prop, dataset string) (chain []Spot, below []Spot, err error) {
	names := Ancestors(dataset)
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	names = append(names, dataset)
	out, err := Run(append([]string{"get", "-H", "-o", "name,value,source", prop}, names...)...)
	if err != nil {
		return nil, nil, err
	}
	chain = parseSpots(out)
	if strings.ContainsAny(dataset, "@#") {
		return chain, nil, nil
	}
	out, err = Run("get", "-H", "-r", "-t", "filesystem,volume", "-s", "local,received", "-o", "name,value,source", prop, dataset)
	if err != nil {
		return chain, nil, err
	}
	for _, s := range parseSpots(out) {
		if s.Dataset != dataset {
			below = append(below, s)
		}
	}
	return chain, below, nil
}

// Where lists every file system and volume that sets prop itself, with the value.
func Where(prop string) ([]Spot, error) {
	out, err := Run("get", "-H", "-t", "filesystem,volume", "-s", "local,received", "-o", "name,value,source", prop)
	if err != nil {
		return nil, err
	}
	return parseSpots(out), nil
}

func parseSpots(out string) []Spot {
	var res []Spot
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 3 {
			continue
		}
		res = append(res, Spot{Dataset: f[0], Value: f[1], Source: f[2]})
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Dataset < res[j].Dataset })
	return res
}

// DatasetForPath returns the dataset the path lives on ("" if it is not on
// ZFS). It statfs's the path, so the path must exist.
func DatasetForPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var st unix.Statfs_t
	if err := unix.Statfs(abs, &st); err != nil {
		return "", err
	}
	if cstr(st.Fstypename[:]) != "zfs" {
		return "", nil
	}
	return cstr(st.Mntfromname[:]), nil
}

func cstr(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// ShellQuote quotes s for a POSIX shell if needed.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./-_", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
