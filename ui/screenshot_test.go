package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/olgeni/zfs-set/props"
)

// fakeListing is a made-up tank/home for tests and screenshots.
func fakeListing() *props.Listing {
	get := "type\tfilesystem\t-\t-\ncreation\tSat Aug 23  0:30 2026\t-\t-\nused\t412G\t-\t-\navailable\t1.20T\t-\t-\nreferenced\t96K\t-\t-\ncompressratio\t1.37x\t-\t-\nmounted\tyes\t-\t-\n" +
		"quota\t500G\tlocal\t-\nreservation\tnone\tdefault\t-\nrecordsize\t128K\tdefault\t-\nmountpoint\t/home\tlocal\t-\nsharenfs\toff\tdefault\t-\nchecksum\ton\tdefault\t-\n" +
		"compression\tzstd\tlocal\tlz4\natime\toff\tinherited from tank\t-\ndevices\ton\tdefault\t-\nexec\ton\tdefault\t-\nsetuid\ton\tdefault\t-\nreadonly\toff\tdefault\t-\njailed\toff\tdefault\t-\nsnapdir\thidden\tdefault\t-\n" +
		"aclmode\tpassthrough\tlocal\t-\naclinherit\tpassthrough\tlocal\t-\ncanmount\ton\tdefault\t-\nxattr\tsa\tdefault\t-\ncopies\t1\tdefault\t-\nrefquota\tnone\tdefault\t-\nrefreservation\tnone\tdefault\t-\n" +
		"primarycache\tall\tdefault\t-\nsecondarycache\tall\tdefault\t-\nlogbias\tlatency\tdefault\t-\ndedup\toff\tdefault\t-\nsync\tstandard\tdefault\t-\ndnodesize\tauto\tinherited from tank\t-\n" +
		"acltype\tnfsv4\tdefault\t-\nrelatime\ton\tdefault\t-\nredundant_metadata\tall\tdefault\t-\noverlay\ton\tdefault\t-\nencryption\toff\tdefault\t-\nspecial_small_blocks\t0\tdefault\t-\nprefetch\tall\tdefault\t-\ndirect\tstandard\tdefault\t-\n" +
		"casesensitivity\tsensitive\t-\t-\nnormalization\tnone\t-\t-\nutf8only\toff\t-\t-\nversion\t5\t-\t-\nsnapshot_limit\tnone\tdefault\t-\nfilesystem_limit\tnone\tdefault\t-\n" +
		"defaultuserquota\t0\t-\t-\ndefaultgroupquota\t0\t-\t-\ndefaultprojectquota\t0\t-\t-\ndefaultuserobjquota\t0\t-\t-\ndefaultgroupobjquota\t0\t-\t-\ndefaultprojectobjquota\t0\t-\t-\n" +
		"com.example:backup\tdaily\tlocal\t-\ncom.example:owner\tstorage team\tinherited from tank\t-\n"
	raw := "used\t442381631488\navailable\t1319413953331\nreferenced\t98304\nquota\t536870912000\nrecordsize\t131072\n"
	return &props.Listing{
		Dataset:  props.Dataset{Name: "tank/home", Type: "filesystem", Mountpoint: "/home", Mounted: true},
		Props:    props.ParseGet(get, raw),
		Features: map[string]string{"zstd_compress": "active", "large_blocks": "enabled"},
		Children: []props.Dataset{{Name: "tank/home/alice"}, {Name: "tank/home/bob"}},
	}
}

// TestScreenshot writes the screens used in the README as ANSI text when
// ZFSSET_SCREENSHOT names a directory (render them with
// "freeze -o FILE.png < FILE.ansi"); it is a no-op otherwise. The data is
// made up so no real dataset shows.
func TestScreenshot(t *testing.T) {
	dir := os.Getenv("ZFSSET_SCREENSHOT")
	if dir == "" {
		t.Skip("ZFSSET_SCREENSHOT not set")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	lipgloss.SetHasDarkBackground(true)

	l := fakeListing()
	m := New("tank/home", "")
	m.width, m.height = 118, 30
	m.listing = l
	m.env = props.Env{Root: true}
	m.edits = props.Edits{{Prop: "quota", Action: props.ActSet, Value: "1T"}, {Prop: "recordsize", Action: props.ActSet, Value: "1M", Descend: true}}
	m.rebuildRows()
	for i, r := range m.rows {
		if r.name == "compression" {
			m.cursor = i
		}
		if r.header == props.GroupSpace {
			m.offset = i
		}
	}
	write(t, filepath.Join(dir, "main.ansi"), m.mainView())

	v, _ := l.Get("sync")
	p, _ := props.Lookup("sync")
	ed := newEditor(p, "sync", "tank/home", v, true, props.Edit{}, false, 2, 110, 22)
	ed.cursor = ed.cursor + 1 // on "always", so the radio shows a move
	write(t, filepath.Join(dir, "editor.ansi"), ed.View())
}

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
