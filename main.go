// Command zfs-set is a terminal UI and scripting front-end for ZFS dataset
// properties (zfs get / zfs set / zfs inherit).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/olgeni/zfs-set/props"
	"github.com/olgeni/zfs-set/ui"
)

const version = "1.0.0"

// multi collects a repeatable flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `usage: zfs-set [dataset]                                        (interactive)
       zfs-set -list [-local] [-json] [dataset]                  (every property, value, source, meaning)
       zfs-set -get PROP[,PROP…] [-json] [dataset]
       zfs-set -set PROP=VALUE [-set PROP=VALUE…] [-r] [-u] [-n|-y|-check] [dataset]
       zfs-set -inherit PROP [-inherit PROP…] [-S] [-r] [-n|-y|-check] [dataset]
       zfs-set -tree PROP [-json] [dataset]                      (the property up and down the tree)
       zfs-set -where PROP [-json]                               (every dataset setting PROP itself)
       zfs-set -dump [-r] [dataset] > F | zfs-set -restore F [-n|-y|-check]
       zfs-set -catalogue [-json] | zfs-set -describe PROP

-set and -inherit may be repeated. -r with -set: the children inherit the new
value too (zfs inherit -r on each child); with -inherit: zfs inherit -r; with
-dump: the descendants too. -S with -inherit reverts to the received value
(zfs inherit -S). -u with -set changes mountpoint/sharenfs without mounting or
sharing (zfs set -u). The dataset defaults to the one the current directory
is on. Exit status: 0, 1 error, 2 declined, 3 changes pending (-check).

`)
		flag.PrintDefaults()
	}
	showVersion := flag.Bool("version", false, "print version and exit")
	var o cliOptions
	flag.BoolVar(&o.list, "list", false, "non-interactive: print every property of the dataset with value, source and meaning")
	flag.BoolVar(&o.local, "local", false, "with -list: only the properties set on the dataset itself (local/received)")
	flag.StringVar(&o.get, "get", "", "non-interactive: print the named properties (comma-separated; aliases and userquota@NAME accepted)")
	flag.Var(&o.set, "set", "non-interactive: set PROP=VALUE (repeatable)")
	flag.Var(&o.inherit, "inherit", "non-interactive: clear PROP so it is inherited or defaults (repeatable)")
	flag.BoolVar(&o.revert, "S", false, "with -inherit: revert to the received value (zfs inherit -S)")
	flag.BoolVar(&o.recursive, "r", false, "with -set: the children inherit it too; with -inherit: zfs inherit -r; with -dump: include the descendants")
	flag.BoolVar(&o.noMount, "u", false, "with -set: change mountpoint/sharenfs/sharesmb without mounting or sharing (zfs set -u)")
	flag.StringVar(&o.tree, "tree", "", "non-interactive: show PROP on every ancestor and on the descendants that override it")
	flag.StringVar(&o.where, "where", "", "non-interactive: list every dataset that sets PROP itself, with the value")
	flag.BoolVar(&o.dump, "dump", false, "non-interactive: print a JSON snapshot of the properties set on the dataset (with -r: of every descendant too); -restore reads it")
	flag.StringVar(&o.restore, "restore", "", "non-interactive: bring the datasets recorded in the -dump snapshot FILE back to it (asks unless -y; -n previews; -check exits 3 if anything differs)")
	flag.BoolVar(&o.catalogue, "catalogue", false, "non-interactive: print the property catalogue")
	flag.StringVar(&o.describe, "describe", "", "non-interactive: describe PROP (values, default, meaning)")
	flag.BoolVar(&o.dryRun, "n", false, "with -set/-inherit/-restore: print the zfs commands and change nothing")
	flag.BoolVar(&o.yes, "y", false, "with -set/-inherit/-restore: apply without asking")
	flag.BoolVar(&o.check, "check", false, "with -set/-inherit/-restore: dry run that exits 3 if anything would change, 0 if nothing would")
	flag.BoolVar(&o.json, "json", false, "non-interactive: JSON output for -list, -get, -tree, -where, -catalogue and -n")
	flag.BoolVar(&o.lenient, "lenient", false, "accept property names that are not in the catalogue (newer OpenZFS)")
	flag.Parse()
	if *showVersion {
		fmt.Println("zfs-set", version)
		return
	}
	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "zfs-set: at most one dataset")
		os.Exit(1)
	}
	dataset := flag.Arg(0)
	if dataset != "" {
		if _, err := props.Info(dataset); err != nil {
			fmt.Fprintln(os.Stderr, "zfs-set:", err)
			os.Exit(1)
		}
	}
	cwdDS, _ := cwdDataset()
	if o.nonInteractive() {
		if dataset == "" && !o.catalogue && o.describe == "" && o.where == "" && o.restore == "" {
			if cwdDS == "" {
				fmt.Fprintln(os.Stderr, "zfs-set: the current directory is not on ZFS; name a dataset")
				os.Exit(1)
			}
			dataset = cwdDS
		}
		os.Exit(runCLI(dataset, o))
	}
	if err := ui.Run(dataset, cwdDS); err != nil {
		fmt.Fprintln(os.Stderr, "zfs-set:", err)
		os.Exit(1)
	}
}

func cwdDataset() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return props.DatasetForPath(wd)
}
