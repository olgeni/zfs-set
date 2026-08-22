package ui

import (
	"fmt"
	"strings"
)

const helpText = `zfs-set — ZFS dataset property editor (zfs get / zfs set / zfs inherit front-end)

What it shows
  Every property of one dataset, grouped by subject, with its value and
  where the value comes from:
    local            set on this dataset (zfs set)
    received         set by zfs receive; zfs set on top keeps the received
                     value around (zfs inherit -S goes back to it)
    inherited from X an ancestor sets it
    default          nobody sets it
    temporary        a mount option (-o ro, noatime…) overrides it until remount
    -                a statistic, or not applicable
  Read-only statistics and the properties fixed at creation (casesensitivity,
  normalization, utf8only, encryption, keyformat, pbkdf2iters, volblocksize)
  are listed but not editable. Properties that do nothing on FreeBSD
  (SELinux contexts, nbmand, sharesmb, vscan, mlslabel, volthreading) are
  hidden until x is pressed.

Editing
  enter/e opens the property: choose the action (set a value, inherit from
  the parent / reset to the default, revert to the received value) and the
  value — a list of every allowed value with its meaning, or a typed value
  validated on the spot (sizes like 10G, paths, options). Options: make the
  children inherit the new value too (zfs inherit -r on each child), change
  mountpoint/sharenfs without mounting or sharing now (zfs set -u).
  i is a shortcut for inherit, S for revert to received, a adds a user
  property (module:name) or a per-key one (userquota@NAME…). Edits are
  pending (→ in the list) until you apply (A): the plan screen shows the
  exact zfs set / zfs inherit commands and the pre-flight notes:
    - values zfs will refuse: a quota below the used space, a volsize that
      is not a multiple of volblocksize, a pool feature not enabled, a
      property the dataset type does not have
    - consequences: mountpoint changes unmount and remount (busy file
      systems fail), canmount=off unmounts now, readonly=on is immediate,
      compression/checksum/recordsize/copies only affect new data,
      sync=disabled and checksum=off and dedup are dangerous or expensive,
      sharenfs needs mountd, Linux-only properties do nothing here
  t shows the property up and down the tree: the value on every ancestor
  and every descendant that overrides it — useful before "children inherit".

Facts worth knowing
  - Inheritance is by dataset tree, not by mount point. zfs inherit refuses
    the properties that are not inheritable (quota, reservation, canmount,
    the limits…): zfs-set resets those with zfs inherit -S, which goes back
    to the received value if there is one, else to the default.
  - zfs set on a mounted file system applies mount-related properties at
    once (readonly, atime, exec, setuid, devices, xattr); nbmand needs a
    remount.
  - User properties (module:name) are free strings, always inherited, and
    removed by zfs inherit when no ancestor sets them.
  - Per-user quotas (userquota@NAME, groupquota@, projectquota@, the obj
    variants) are not in zfs get all: add them with a, or zfs userspace.
  - zfs set is not recursive; the "children inherit it" option is zfs
    inherit -r on each child, which clears what they had.
`

// keymap is the compact key reference (h).
var keymap = [][2]string{
	{"Main screen", ""},
	{"↑/↓ j/k, pgup/pgdn, g/G", "move"},
	{"enter, e", "edit the property (set / inherit / revert to received)"},
	{"i", "inherit: clear the local value (pending)"},
	{"S", "revert to the received value (pending, zfs inherit -S)"},
	{"a", "add a user property (module:name) or userquota@NAME…"},
	{"d", "drop the pending edit of this property"},
	{"A", "apply: preview the zfs commands, then run them"},
	{"u", "undo the last edit"},
	{"r", "reload from the kernel (discarding edits)"},
	{"t", "the property up and down the tree (ancestors, overriding descendants)"},
	{"/", "filter by name or description (esc clears)"},
	{"l", "only the properties set here (local/received)"},
	{"x", "show the properties that do nothing on FreeBSD"},
	{"D", "switch dataset"},
	{"q, esc", "back to the dataset list when started there, else quit"},
	{"?", "help   h  this key list"},
	{"Editor", ""},
	{"↑/↓", "move between action, values, options and buttons"},
	{"space", "select the action / the value, toggle an option"},
	{"enter", "on a value: select it and OK; on a text field: OK; on the action: select"},
	{"tab", "jump to the buttons"},
	{"ctrl+s, F10", "OK"},
	{"esc, q", "cancel"},
	{"Plan screen", ""},
	{"y, enter", "run the commands"},
	{"esc, n", "back to editing"},
}

func keymapView(width int) string {
	var b strings.Builder
	for _, k := range keymap {
		if k[1] == "" {
			b.WriteString("\n" + styleHeader.Render(k[0]) + "\n")
			continue
		}
		fmt.Fprintf(&b, "  %s %s\n", styleHelpKey.Render(fit(k[0], 26)), k[1])
	}
	return b.String()
}
