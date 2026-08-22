package props

import (
	"os"
	"strings"
	"testing"
)

// TestCatalogueMatchesZFSGet pins the catalogue to the table "zfs get"
// prints (OpenZFS 2.4.2 on FreeBSD 15): every name, the EDIT and INHERIT
// columns.
func TestCatalogueMatchesZFSGet(t *testing.T) {
	const table = `
available NO NO
clones NO NO
compressratio NO NO
createtxg NO NO
creation NO NO
defer_destroy NO NO
encryptionroot NO NO
filesystem_count NO NO
guid NO NO
keystatus NO NO
logicalreferenced NO NO
logicalused NO NO
mounted NO NO
objsetid NO NO
origin NO NO
receive_resume_token NO NO
redact_snaps NO NO
refcompressratio NO NO
referenced NO NO
snapshot_count NO NO
snapshots_changed NO NO
type NO NO
used NO NO
usedbychildren NO NO
usedbydataset NO NO
usedbyrefreservation NO NO
usedbysnapshots NO NO
userrefs NO NO
written NO NO
aclinherit YES YES
aclmode YES YES
acltype YES YES
atime YES YES
canmount YES NO
casesensitivity NO YES
checksum YES YES
compression YES YES
context YES NO
copies YES YES
dedup YES YES
defaultgroupobjquota YES NO
defaultgroupquota YES NO
defaultprojectobjquota YES NO
defaultprojectquota YES NO
defaultuserobjquota YES NO
defaultuserquota YES NO
defcontext YES NO
devices YES YES
direct YES YES
dnodesize YES YES
encryption NO YES
exec YES YES
filesystem_limit YES NO
fscontext YES NO
jailed YES YES
keyformat NO NO
keylocation YES NO
logbias YES YES
longname YES YES
mlslabel YES YES
mountpoint YES YES
nbmand YES YES
normalization NO YES
overlay YES YES
pbkdf2iters NO NO
prefetch YES YES
primarycache YES YES
quota YES NO
readonly YES YES
recordsize YES YES
redundant_metadata YES YES
refquota YES NO
refreservation YES NO
relatime YES YES
reservation YES NO
rootcontext YES NO
secondarycache YES YES
setuid YES YES
sharenfs YES YES
sharesmb YES YES
snapdev YES YES
snapdir YES YES
snapshot_limit YES NO
special_small_blocks YES YES
sync YES YES
utf8only NO YES
version YES NO
volblocksize NO YES
volmode YES YES
volsize YES NO
volthreading YES NO
vscan YES YES
xattr YES YES
userused@ NO NO
groupused@ NO NO
projectused@ NO NO
userobjused@ NO NO
groupobjused@ NO NO
projectobjused@ NO NO
userquota@ YES NO
groupquota@ YES NO
projectquota@ YES NO
userobjquota@ YES NO
groupobjquota@ YES NO
projectobjquota@ YES NO
written@ NO NO
written# NO NO`
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(table), "\n") {
		f := strings.Fields(line)
		name, edit, inh := f[0], f[1] == "YES", f[2] == "YES"
		seen[name] = true
		p, ok := byName[name]
		if !ok {
			t.Errorf("%s: missing from the catalogue", name)
			continue
		}
		if (p.Kind == Settable) != edit {
			t.Errorf("%s: zfs says EDIT=%v, catalogue kind %s", name, edit, p.Kind)
		}
		if p.Inherit != inh {
			t.Errorf("%s: zfs says INHERIT=%v, catalogue %v", name, inh, p.Inherit)
		}
		if p.Note == "" || p.Group == "" {
			t.Errorf("%s: no note or group", name)
		}
		if p.Kind == Settable && (p.Type == TBool || p.Type == TEnum) && len(p.Values) == 0 {
			t.Errorf("%s: no values", name)
		}
	}
	for _, p := range Catalogue {
		if !seen[p.Name] {
			t.Errorf("%s: in the catalogue but zfs get does not list it", p.Name)
		}
	}
	for _, g := range GroupOrder {
		if g == GroupUser {
			continue
		}
		if len(ByGroup()[g]) == 0 {
			t.Errorf("group %q is empty", g)
		}
	}
}

func TestLookup(t *testing.T) {
	for _, c := range []struct {
		name, want string
		ok         bool
	}{
		{"compression", "compression", true}, {"compress", "compression", true}, {"avail", "available", true},
		{"userquota@bob", "userquota@", true}, {"written@snap1", "written@", true}, {"written#bm", "written#", true},
		{"com.example:foo", "com.example:foo", true}, {"nosuchprop", "", false}, {"userquota@", "userquota@", true},
	} {
		p, ok := Lookup(c.name)
		if ok != c.ok || ok && p.Name != c.want {
			t.Errorf("Lookup(%q) = %v,%v want %q,%v", c.name, p, ok, c.want, c.ok)
		}
	}
	if Known("Bad:Name") || !Known("org.freebsd:jail") || Known("foo") {
		t.Error("Known")
	}
	if Family("userquota@bob") != "userquota@" || Family("quota") != "" {
		t.Error("Family")
	}
}

func TestParseSize(t *testing.T) {
	for _, c := range []struct {
		in   string
		want uint64
		bad  bool
	}{
		{"10G", 10 << 30, false}, {"10g", 10 << 30, false}, {"10GB", 10 << 30, false}, {"1.5g", 1536 << 20, false},
		{"512", 512, false}, {"16K", 16384, false}, {"1.50GB", 1536 << 20, false}, {"100b", 100, false},
		{"", 0, true}, {"abc", 0, true}, {"10X", 0, true}, {"1..5G", 0, true},
	} {
		n, err := ParseSize(c.in)
		if (err != nil) != c.bad || n != c.want {
			t.Errorf("ParseSize(%q) = %d,%v want %d,bad=%v", c.in, n, err, c.want, c.bad)
		}
	}
	for _, c := range []struct {
		in   uint64
		want string
	}{{0, "0"}, {512, "512"}, {131072, "128K"}, {1536 << 20, "1.50G"}, {10 << 30, "10G"}, {123 << 40, "123T"}} {
		if got := FormatSize(c.in); got != c.want {
			t.Errorf("FormatSize(%d) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := func(prop, v string) {
		t.Helper()
		p, _ := Lookup(prop)
		if _, err := p.Validate(v); err != nil {
			t.Errorf("%s=%s: %v", prop, v, err)
		}
	}
	bad := func(prop, v string) {
		t.Helper()
		p, _ := Lookup(prop)
		if _, err := p.Validate(v); err == nil {
			t.Errorf("%s=%s accepted", prop, v)
		}
	}
	ok("atime", "off")
	bad("atime", "yes")
	ok("compression", "zstd-19")
	ok("compression", "zstd-fast-1000")
	ok("compression", "gzip-9")
	bad("compression", "zstd-20")
	bad("compression", "gzip-0")
	ok("dedup", "edonr,verify")
	bad("dedup", "edonr")
	ok("recordsize", "1M")
	bad("recordsize", "100K")
	bad("recordsize", "32M")
	ok("special_small_blocks", "0")
	ok("quota", "none")
	ok("quota", "10G")
	bad("quota", "lots")
	ok("refreservation", "auto")
	bad("reservation", "auto")
	ok("mountpoint", "/srv/x")
	bad("mountpoint", "srv/x")
	ok("mountpoint", "legacy")
	ok("version", "current")
	bad("version", "6")
	ok("keylocation", "file:///etc/key")
	bad("keylocation", "/etc/key")
	ok("sharenfs", "-maproot=root,-network=10.0.0.0/24")
	ok("filesystem_limit", "10")
	bad("filesystem_limit", "ten")
	ok("com.example:x", "anything at all")
	bad("used", "1")
	bad("casesensitivity", "mixed")
	if n := len((&Catalogue[0]).Choices()); n != 0 { // mountpoint
		t.Errorf("mountpoint choices %d", n)
	}
	p, _ := Lookup("compression")
	if n := len(p.Choices()); n != 8+19+21+9 {
		t.Errorf("compression choices %d", n)
	}
}

func loadGolden(t *testing.T) *Listing {
	t.Helper()
	out, err := os.ReadFile("testdata/get.txt")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile("testdata/get-p.txt")
	vals := ParseGet(string(out), string(raw))
	l := &Listing{Dataset: Dataset{Name: "tank/test", Type: "filesystem", Mountpoint: "none"}, Props: vals,
		Features: map[string]string{"zstd_compress": "active", "large_blocks": "enabled", "sha512": "enabled", "skein": "enabled", "edonr": "enabled", "blake3": "disabled", "large_dnode": "enabled", "filesystem_limits": "active", "allocation_classes": "enabled", "longname": "disabled"},
		Children: []Dataset{{Name: "tank/test/a", Type: "filesystem"}}}
	return l
}

func TestParseGet(t *testing.T) {
	l := loadGolden(t)
	if len(l.Props) != 82 {
		t.Fatalf("%d props", len(l.Props))
	}
	mp, _ := l.Get("mountpoint")
	if mp.Value != "none" || mp.Source != "inherited" || mp.From != "tank" || mp.SourceLabel() != "inherited from tank" {
		t.Errorf("mountpoint %+v", mp)
	}
	rs, _ := l.Get("recsize")
	if rs.Name != "recordsize" || rs.Value != "128K" || rs.Raw != "131072" || rs.Source != "default" || rs.Local() {
		t.Errorf("recordsize %+v", rs)
	}
	ty, _ := l.Get("type")
	if ty.Source != "none" || ty.SourceLabel() != "-" {
		t.Errorf("type %+v", ty)
	}
	up, ok := l.Get("org.freebsd.ioc:active")
	if !ok || up.Value != "yes" || up.From != "tank" {
		t.Errorf("user prop %+v %v", up, ok)
	}
	// received value
	vals := ParseGet("compression\tzstd\tlocal\tlz4\n", "")
	if vals[0].Received != "lz4" || vals[0].Source != "local" {
		t.Errorf("received %+v", vals[0])
	}
}

func TestBuildAndApply(t *testing.T) {
	l := loadGolden(t)
	edits := Edits{}
	edits = edits.Put(Edit{Prop: "compression", Action: ActSet, Value: "zstd", Descend: true})
	edits = edits.Put(Edit{Prop: "atime", Action: ActInherit})
	edits = edits.Put(Edit{Prop: "mountpoint", Action: ActSet, Value: "/srv/test", NoMount: true})
	edits = edits.Put(Edit{Prop: "recordsize", Action: ActSet, Value: "128K"}) // default, not local: still a set
	edits = edits.Put(Edit{Prop: "quota", Action: ActInherit})                 // not local: skipped
	edits = edits.Put(Edit{Prop: "org.freebsd.ioc:active", Action: ActRevert, Descend: true})
	edits = edits.Put(Edit{Prop: "atime", Action: ActInherit, Descend: true}) // replaces
	if len(edits) != 6 {
		t.Fatalf("%d edits", len(edits))
	}
	p := Build(l, edits)
	want := []string{
		"zfs set compression=zstd tank/test",
		"zfs inherit -r compression tank/test/a",
		"zfs inherit -r atime tank/test",
		"zfs set -u mountpoint=/srv/test tank/test",
		"zfs set recordsize=128K tank/test",
		"zfs inherit -S -r org.freebsd.ioc:active tank/test",
	}
	// a non-inheritable property is reset with -S (zfs inherit refuses it otherwise)
	if got := Build(l, Edits{{Prop: "quota", Action: ActInherit}}).Commands(); len(got) != 0 {
		t.Errorf("quota not local: %v", got)
	}
	lq := l.Clone()
	lq.Set(Value{Name: "quota", Value: "10G", Raw: "10737418240", Source: "local"})
	if got := Build(lq, Edits{{Prop: "quota", Action: ActInherit}, {Prop: "canmount", Action: ActInherit, Descend: true}}).Commands(); strings.Join(got, ";") != "zfs inherit -S quota tank/test;zfs inherit -S -r canmount tank/test" {
		t.Errorf("reset: %v", got)
	}
	got := p.Commands()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("plan:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if p.Steps[0].Desc != "set compression to zstd (was on, default)" {
		t.Errorf("desc %q", p.Steps[0].Desc)
	}
	parent := &Listing{Dataset: Dataset{Name: "tank"}, Props: []Value{{Name: "atime", Value: "off", Source: "local"}}}
	after := Apply(l, edits, parent)
	if v, _ := after.Get("compression"); v.Value != "zstd" || v.Source != "local" {
		t.Errorf("after compression %+v", v)
	}
	if v, _ := after.Get("atime"); v.Value != "off" || v.Source != "inherited" || v.From != "tank" {
		t.Errorf("after atime %+v", v)
	}
	if v, _ := after.Get("quota"); v.Value != "none" || v.Source != "default" {
		t.Errorf("after quota %+v", v)
	}
	if edits.Drop("atime").Drop("quota").Drop("recordsize").Drop("mountpoint").Drop("compression").Drop("org.freebsd.ioc:active") != nil {
		t.Error("drop")
	}
}

func TestPreflight(t *testing.T) {
	l := loadGolden(t)
	env := Env{Root: true, Mountd: false}
	probs := Preflight(l, Edits{
		{Prop: "quota", Action: ActSet, Value: "1K"},              // below used (98304)
		{Prop: "checksum", Action: ActSet, Value: "blake3"},       // feature disabled
		{Prop: "compression", Action: ActSet, Value: "zstd"},      // new data note
		{Prop: "volsize", Action: ActSet, Value: "1G"},            // not a volume
		{Prop: "sync", Action: ActSet, Value: "disabled"},         // warning
		{Prop: "casesensitivity", Action: ActSet, Value: "mixed"}, // create-only
		{Prop: "used", Action: ActSet, Value: "1"},                // read-only
		{Prop: "nbmand", Action: ActSet, Value: "on"},             // linux
		{Prop: "keylocation", Action: ActSet, Value: "prompt"},    // not encrypted
		{Prop: "version", Action: ActSet, Value: "4"},             // downgrade
		{Prop: "sharenfs", Action: ActSet, Value: "on"},           // mountd
		{Prop: "atime", Action: ActRevert},                        // no received
		{Prop: "nosuch", Action: ActSet, Value: "x"},              // unknown
		{Prop: "recordsize", Action: ActSet, Value: "1M"},         // large_blocks enabled: fine
		{Prop: "org.freebsd:x", Action: ActSet, Value: "y"},       // fine
		{Prop: "canmount", Action: ActInherit, Descend: true},     // not inheritable
		{Prop: "quota", Action: ActSet, Value: "bogus"},           // invalid (Put not used: duplicate on purpose)
	}, env)
	var fatal, warn []string
	for _, p := range probs {
		if p.Fatal {
			fatal = append(fatal, p.Msg)
		} else {
			warn = append(warn, p.Msg)
		}
	}
	has := func(list []string, sub string) bool {
		for _, s := range list {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	for _, s := range []string{"quota 1K is below", "blake3, which is disabled", "volsize applies to volumes", "casesensitivity can only be set when", "used is read-only", "not encrypted", "version can only go up", "nosuch is not a known", "is a size (10G, 512M, …) or none"} {
		if !has(fatal, s) {
			t.Errorf("missing fatal %q in %v", s, fatal)
		}
	}
	for _, s := range []string{"compression affects only data written", "sync=disabled", "nbmand has no effect on FreeBSD", "mountd is not running", "atime has no received value", "canmount is not inheritable"} {
		if !has(warn, s) {
			t.Errorf("missing warning %q in %v", s, warn)
		}
	}
	if has(fatal, "recordsize") || has(fatal, "org.freebsd:x") {
		t.Errorf("false fatal: %v", fatal)
	}
	if len(probs) > 0 && !probs[0].Fatal {
		t.Error("fatal first")
	}
	// unprivileged note
	probs = Preflight(l, Edits{{Prop: "mountpoint", Action: ActSet, Value: "/x"}}, Env{User: "bob"})
	if !has(msgs(probs), "bob is not root") || !has(msgs(probs), "mountpoint also need") {
		t.Errorf("unprivileged: %v", msgs(probs))
	}
}

func msgs(ps []Problem) []string {
	var r []string
	for _, p := range ps {
		r = append(r, p.Msg)
	}
	return r
}
