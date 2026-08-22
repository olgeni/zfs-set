package props

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Problem is a pre-flight finding: Fatal when zfs will refuse the command,
// otherwise a consequence worth knowing before running it.
type Problem struct {
	Fatal bool
	Msg   string
}

// Env is what the pre-flight knows about the operator and the system.
type Env struct {
	Root    bool   // running as root
	User    string // operator name (for the unprivileged note)
	Cwd     string // current directory (to warn when it is inside a mount point that moves)
	Lenient bool   // accept property names outside the catalogue
	Mountd  bool   // mountd is running (for sharenfs)
}

// MountdRunning reports whether mountd has a pid file (FreeBSD).
func MountdRunning() bool {
	_, err := os.Stat("/var/run/mountd.pid")
	return err == nil
}

// featureFor returns the pool feature a value needs, "" if none.
func featureFor(prop, value string) string {
	switch prop {
	case "checksum":
		switch value {
		case "sha512", "skein", "edonr", "blake3":
			return value
		}
	case "compression":
		switch {
		case strings.HasPrefix(value, "zstd"):
			return "zstd_compress"
		case value == "lz4":
			return "lz4_compress"
		}
	case "dedup":
		for _, f := range []string{"sha512", "skein", "edonr", "blake3"} {
			if strings.HasPrefix(value, f) {
				return f
			}
		}
	case "recordsize":
		if n, err := ParseSize(value); err == nil && n > 128<<10 {
			return "large_blocks"
		}
	case "dnodesize":
		if value != "legacy" {
			return "large_dnode"
		}
	case "special_small_blocks":
		if n, err := ParseSize(value); err == nil && n > 0 {
			return "allocation_classes"
		}
	case "longname":
		if value == "on" {
			return "longname"
		}
	}
	if p, ok := Lookup(prop); ok && p.Feature != "" && prop != "dnodesize" && prop != "longname" {
		return p.Feature
	}
	return ""
}

// Preflight checks the edits against the dataset before the plan runs.
// Fatal problems come first.
func Preflight(l *Listing, edits Edits, env Env) []Problem {
	var probs []Problem
	fatal := func(f string, a ...any) { probs = append(probs, Problem{true, fmt.Sprintf(f, a...)}) }
	warn := func(f string, a ...any) { probs = append(probs, Problem{false, fmt.Sprintf(f, a...)}) }
	ds := l.Dataset
	raw := func(name string) (uint64, bool) {
		v, ok := l.Get(name)
		if !ok {
			return 0, false
		}
		n, err := strconv.ParseUint(v.Raw, 10, 64)
		return n, err == nil
	}
	needMount := map[string]bool{}
	for _, e := range edits {
		p, known := Lookup(e.Prop)
		if !known {
			if env.Lenient {
				warn("%s is not in the catalogue; passed to zfs as is", e.Prop)
				continue
			}
			fatal("%s is not a known property (or a valid user property: module:name); -lenient passes it through", e.Prop)
			continue
		}
		if IsUser(e.Prop) && !ValidUserName(e.Prop) {
			fatal("%s: user property names are lowercase letters, digits, : - . _ with a colon, at most 256 characters", e.Prop)
			continue
		}
		if p.Kind == Readonly {
			fatal("%s is read-only", e.Prop)
			continue
		}
		if p.Kind == CreateOnly {
			switch e.Prop {
			case "keyformat", "pbkdf2iters":
				fatal("%s cannot be set with zfs set: use zfs change-key -o %s=…", e.Prop, e.Prop)
			default:
				fatal("%s can only be set when the dataset is created (zfs create -o %s=…)", e.Prop, e.Prop)
			}
			continue
		}
		if !p.AppliesTo(ds.Type) && !IsUser(e.Prop) {
			fatal("%s applies to %s, not to a %s", e.Prop, p.TypesLabel(), ds.Type)
			continue
		}
		if ds.IsSnapshot() && !IsUser(e.Prop) {
			fatal("only user properties can be set on a snapshot")
			continue
		}
		if e.NoMount && e.Prop != "mountpoint" && e.Prop != "sharenfs" && e.Prop != "sharesmb" {
			fatal("-u (update without mounting/sharing) only applies to mountpoint, sharenfs and sharesmb")
		}
		cur, _ := l.Get(e.Prop)
		if e.Action != ActSet {
			if have := l.has(e.Prop); have && !cur.Local() && e.Action == ActInherit && !e.Descend {
				warn("%s is not set on %s (%s): zfs inherit changes nothing", e.Prop, ds.Name, cur.SourceLabel())
			}
			if e.Action == ActRevert && cur.Received == "" {
				warn("%s has no received value: zfs inherit -S behaves like a plain zfs inherit", e.Prop)
			}
			if !p.Inherit && !IsUser(e.Prop) && e.Descend {
				warn("%s is not inheritable: zfs inherit -r resets it to the default on every dataset below", e.Prop)
			}
			if e.Prop == "mountpoint" || e.Prop == "canmount" {
				needMount[e.Prop] = true
			}
			continue
		}
		if _, err := p.Validate(e.Value); err != nil {
			fatal("%v", err)
			continue
		}
		if p.Linux {
			warn("%s has no effect on FreeBSD (%s)", e.Prop, strings.TrimSuffix(strings.SplitN(p.Note, ";", 2)[0], "."))
		}
		if f := featureFor(e.Prop, e.Value); f != "" && l.Features != nil {
			if st, ok := l.Features[f]; ok && st == "disabled" {
				fatal("%s=%s needs the pool feature %s, which is disabled on %s: zpool set feature@%s=enabled %s", e.Prop, e.Value, f, ds.Pool(), f, ds.Pool())
			} else if !ok && f != "lz4_compress" {
				warn("%s=%s needs the pool feature %s, which %s does not have: zfs will refuse it", e.Prop, e.Value, f, ds.Pool())
			}
		}
		if p.NewData && cur.Value != e.Value {
			warn("%s affects only data written from now on; existing blocks keep the old %s", e.Prop, cur.Value)
		}
		switch e.Prop {
		case "quota":
			if n, err := ParseSize(e.Value); err == nil && e.Value != "none" {
				if used, ok := raw("used"); ok && n < used {
					fatal("quota %s is below the %s already used (zfs refuses it)", e.Value, FormatSize(used))
				}
			}
		case "refquota":
			if n, err := ParseSize(e.Value); err == nil && e.Value != "none" {
				if ref, ok := raw("referenced"); ok && n < ref {
					fatal("refquota %s is below the %s referenced (zfs refuses it)", e.Value, FormatSize(ref))
				}
			}
		case "reservation", "refreservation":
			if e.Value == "auto" && !ds.IsVolume() {
				fatal("refreservation=auto is only for volumes")
			} else if n, err := ParseSize(e.Value); err == nil && e.Value != "none" && e.Value != "auto" {
				avail, ok1 := raw("available")
				curr, _ := raw(e.Prop)
				if ok1 && n > avail+curr {
					fatal("%s %s exceeds the %s available (zfs refuses it)", e.Prop, e.Value, FormatSize(avail+curr))
				}
			}
		case "volsize":
			n, err := ParseSize(e.Value)
			if err == nil {
				if bs, ok := raw("volblocksize"); ok && bs > 0 && n%bs != 0 {
					fatal("volsize must be a multiple of volblocksize (%s); %s rounds to %s", FormatSize(bs), e.Value, FormatSize((n/bs+1)*bs))
				}
				if old, ok := raw("volsize"); ok && n < old {
					warn("shrinking the volume from %s to %s destroys whatever lies beyond the new end — extreme care", FormatSize(old), FormatSize(n))
				}
				if avail, ok := raw("available"); ok {
					if rr, ok2 := raw("refreservation"); ok2 && rr > 0 {
						if old, _ := raw("volsize"); n > old && n-old > avail {
							fatal("growing the volume by %s also grows its refreservation, but only %s is available", FormatSize(n-old), FormatSize(avail))
						}
					}
				}
			}
		case "mountpoint":
			needMount[e.Prop] = true
			if !e.NoMount {
				if ds.Mounted && e.Value != cur.Value {
					warn("%s is unmounted and remounted at %s, and so is every child inheriting the mount point; a busy file system makes zfs set fail (the -u option changes the property only)", ds.Name, e.Value)
					if env.Cwd != "" && (env.Cwd == ds.Mountpoint || strings.HasPrefix(env.Cwd, ds.Mountpoint+"/")) {
						warn("the current directory %s is inside it: the unmount will fail or leave you in a stale directory", env.Cwd)
					}
				}
				if e.Value == "legacy" || e.Value == "none" {
					warn("mountpoint=%s: the file system stays unmounted%s", e.Value, map[bool]string{true: " (mount it with mount -t zfs / fstab)", false: ""}[e.Value == "legacy"])
				}
			}
		case "canmount":
			needMount[e.Prop] = true
			if e.Value == "off" && ds.Mounted {
				warn("canmount=off unmounts %s now", ds.Name)
			}
			if e.Value == "noauto" {
				warn("canmount=noauto: after the next boot or import the file system is not mounted until zfs mount %s", ds.Name)
			}
		case "readonly":
			if e.Value == "on" && ds.Mounted && cur.Value != "on" {
				warn("%s becomes read-only immediately (it is mounted)", ds.Name)
			}
		case "sync":
			if e.Value == "disabled" {
				warn("sync=disabled: applications are told their fsync'ed data is safe when it is not; a crash loses up to ~5 s of it — never for databases, NFS or VM images that matter")
			}
		case "dedup":
			if e.Value != "off" {
				warn("dedup needs several GB of RAM per TB of unique data (the table must stay in the ARC) and slows writes; it only pays for highly duplicate data")
			}
		case "checksum":
			if e.Value == "off" {
				warn("checksum=off: silent corruption of user data is no longer detected or healed")
			}
		case "copies":
			if e.Value == "3" {
				if enc, ok := l.Get("encryption"); ok && enc.Value != "off" {
					fatal("copies=3 is not allowed on an encrypted dataset")
				}
			}
		case "sharenfs":
			needMount[e.Prop] = true
			if e.Value != "off" && !env.Mountd && !e.NoMount {
				warn("mountd is not running: the export is written to /etc/zfs/exports but nothing serves it (nfs_server_enable=YES, mountd_enable=YES)")
			}
		case "acltype":
			if e.Value == "posix" {
				warn("acltype=posix is Linux-only: on FreeBSD it behaves as off")
			}
		case "keylocation":
			if enc, ok := l.Get("encryption"); ok && enc.Value == "off" {
				fatal("%s is not encrypted: keylocation does not apply", ds.Name)
			} else if root, ok := l.Get("encryptionroot"); ok && root.Value != ds.Name && root.Value != "-" && root.Value != "" {
				fatal("keylocation can only be set on the encryption root (%s), not on a dataset inheriting its key", root.Value)
			}
		case "version":
			if e.Value != "current" {
				if n, err := strconv.Atoi(e.Value); err == nil {
					if c, err2 := strconv.Atoi(cur.Value); err2 == nil && n < c {
						fatal("version can only go up (it is %s)", cur.Value)
					}
				}
			}
		case "jailed":
			if e.Value == "on" {
				warn("jailed=on: the host unmounts %s and no longer manages it; attach it with zfs jail JID %s", ds.Name, ds.Name)
			}
		case "special_small_blocks":
			if n, err := ParseSize(e.Value); err == nil && n > 0 {
				warn("special_small_blocks only does something with a special allocation class vdev in %s", ds.Pool())
			}
		case "recordsize":
			if n, err := ParseSize(e.Value); err == nil && n < 16<<10 {
				warn("recordsize %s: such small blocks compress poorly and inflate metadata; only for fixed-record databases", e.Value)
			}
		case "atime":
			if e.Value == "on" {
				if rel, ok := l.Get("relatime"); ok && rel.Value == "off" {
					warn("atime=on with relatime=off writes on every read; consider relatime=on")
				}
			}
		}
		if e.Descend && len(l.Children) == 0 {
			warn("%s has no children: nothing below to make inherit %s", ds.Name, e.Prop)
		}
	}
	if !env.Root {
		var names []string
		for n := range needMount {
			names = append(names, n)
		}
		sort.Strings(names)
		who := env.User
		if who == "" {
			who = "an unprivileged user"
		}
		msg := fmt.Sprintf("%s is not root: each property needs its delegated permission (zfs allow … PROP), and zfs inherit the same", who)
		if len(names) > 0 {
			msg += fmt.Sprintf("; %s also need(s) the mount permission and vfs.usermount=1", strings.Join(names, ", "))
		}
		warn("%s", msg)
	}
	sort.SliceStable(probs, func(i, j int) bool { return probs[i].Fatal && !probs[j].Fatal })
	return probs
}
