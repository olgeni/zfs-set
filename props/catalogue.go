// Package props knows the ZFS dataset properties (zfsprops(7)): what each
// one means, what values it takes, whether it can be set, inherited or only
// given at creation — and turns edits into zfs set / zfs inherit commands.
package props

import (
	"sort"
	"strings"
)

// Kind says how a property can be changed.
type Kind int

const (
	Settable   Kind = iota // zfs set / zfs inherit
	Readonly               // a statistic, cannot be set or inherited
	CreateOnly             // fixed when the dataset is created (zfs create -o); shown, not editable
)

func (k Kind) String() string {
	switch k {
	case Readonly:
		return "read-only"
	case CreateOnly:
		return "creation-only"
	}
	return "settable"
}

// Type is the value syntax of a property.
type Type int

const (
	TBool     Type = iota // on | off
	TEnum                 // one of Values
	TSize                 // a size with units (quota-like: also none)
	TCount                // a count or none
	TString               // free text
	TPath                 // absolute path | legacy | none (mountpoint)
	TCompress             // compression: on|off|algorithms with levels
	TDedup                // dedup: off|on|verify|algo[,verify]
	TVersion              // 1..5 | current
	TKeyloc               // prompt | file:// | https:// | http://
	TShare                // on | off | share options
	TNumber               // plain integer (volsize handled as TSize)
)

// Option is one allowed value of an enum property, with its meaning.
type Option struct {
	Value string
	Note  string
}

// Prop is one catalogue entry.
type Prop struct {
	Name    string
	Short   string // zfs column alias (avail, compress, …), "" if none
	Kind    Kind
	Type    Type
	Inherit bool     // INHERIT column of zfs get
	Values  []Option // for TBool/TEnum (and the fixed part of TCompress/TDedup)
	Default string
	Group   string
	Types   string // datasets it applies to: "fs", "vol", "fs,vol", "snap", "all"
	Note    string // one line
	Detail  string // a few sentences, for the help pane
	NewData bool   // affects only data written afterwards
	Feature string // pool feature that must be enabled to set it (or some values)
	Linux   bool   // no effect on FreeBSD (SELinux, nbmand, …)
	Family  bool   // a per-key family: userquota@USER, written@SNAP …; Name ends with @ or #
}

// IsUser reports whether name is a user property (module:property).
func IsUser(name string) bool { return strings.Contains(name, ":") }

// ValidUserName checks the user property syntax: a colon, lowercase letters,
// digits, ":" "-" "." "_", at most 256 characters, not starting with "-".
func ValidUserName(name string) bool {
	if !IsUser(name) || len(name) > 256 || strings.HasPrefix(name, "-") {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ':' || r == '-' || r == '.' || r == '_') {
			return false
		}
	}
	return true
}

// GroupOrder is the display order of the groups.
var GroupOrder = []string{
	GroupMount, GroupSpace, GroupData, GroupIO, GroupShare, GroupACL, GroupVolume, GroupCrypto, GroupNames, GroupSELinux, GroupOther, GroupStats, GroupUser,
}

const (
	GroupMount   = "Mounting & visibility"
	GroupSpace   = "Space, quotas & limits"
	GroupData    = "Data layout & integrity"
	GroupIO      = "Caching & I/O"
	GroupShare   = "Sharing"
	GroupACL     = "ACLs"
	GroupVolume  = "Volumes"
	GroupCrypto  = "Encryption"
	GroupNames   = "File names"
	GroupSELinux = "SELinux (Linux only)"
	GroupOther   = "Other"
	GroupStats   = "Statistics (read-only)"
	GroupUser    = "User properties"
)

var onoff = []Option{{"on", ""}, {"off", ""}}

func bo(on, off string) []Option { return []Option{{"on", on}, {"off", off}} }

// Catalogue is every native property of OpenZFS 2.4 as zfs get lists it on
// FreeBSD, in display order within each group.
var Catalogue = []Prop{
	// ---------------------------------------------------------- Mounting
	{Name: "mountpoint", Kind: Settable, Type: TPath, Inherit: true, Default: "/<pool>/<path>", Group: GroupMount, Types: "fs",
		Note:   "Where the file system is mounted: a path, legacy (mount(8)/fstab) or none",
		Detail: "Changing it unmounts the file system and every child that inherits the mount point and remounts them at the new path (unless the new value is legacy; previously legacy/none file systems are mounted now). Shared file systems are unshared and reshared. zfs set -u changes the property without mounting or unmounting anything."},
	{Name: "canmount", Kind: Settable, Type: TEnum, Default: "on", Group: GroupMount, Types: "fs",
		Values: []Option{{"on", "mounted by zfs mount -a and at import"}, {"off", "never mounted; the dataset only carries properties for its children"}, {"noauto", "only mounted explicitly (zfs mount DS), never by mount -a or at import"}},
		Note:   "Whether the file system is mounted automatically, explicitly only, or never",
		Detail: "off is the classic trick for a dataset that exists only to give its children a common mount point or inherited properties: it still has a mountpoint the children inherit. noauto datasets are mounted by name only. Not inherited. Setting off on a mounted file system unmounts it."},
	{Name: "readonly", Short: "rdonly", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "off", Group: GroupMount, Types: "fs,vol",
		Note:   "on: the dataset cannot be modified (mount option ro)",
		Detail: "Equivalent to the ro/rw mount options; a mounted file system is remounted read-only at once. Volumes become read-only block devices."},
	{Name: "atime", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "on", Group: GroupMount, Types: "fs",
		Note:   "Update file access times on read (off avoids write traffic when reading; see relatime)",
		Detail: "Turning it off avoids a write for every read and is a common performance setting, but confuses mailers and tools that rely on access times. on/off are the atime/noatime mount options."},
	{Name: "relatime", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "on", Group: GroupMount, Types: "fs",
		Note:   "With atime=on: update the access time only if older than mtime/ctime or than 24 hours",
		Detail: "The Linux-style compromise: access times stay roughly right without a write per read. Only matters when atime=on."},
	{Name: "exec", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "on", Group: GroupMount, Types: "fs",
		Note: "Whether programs can be executed from this file system (mount option exec/noexec)"},
	{Name: "setuid", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "on", Group: GroupMount, Types: "fs",
		Note: "Whether the setuid/setgid bits are honoured (mount option suid/nosuid)"},
	{Name: "devices", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "on", Group: GroupMount, Types: "fs",
		Note: "Whether device nodes can be opened on this file system (mount option dev/nodev)"},
	{Name: "overlay", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "on", Group: GroupMount, Types: "fs",
		Note:   "Allow mounting on a non-empty or busy directory (the normal FreeBSD/Linux behaviour)",
		Detail: "off refuses to mount over a directory that already has files, like OpenZFS on other platforms."},
	{Name: "xattr", Kind: Settable, Type: TEnum, Inherit: true, Default: "on", Group: GroupMount, Types: "fs",
		Values: []Option{{"on", "same as sa"}, {"sa", "extended attributes stored as system attributes: fast, up to 64K per file (FreeBSD/Linux only)"}, {"dir", "directory-based xattrs: slower, no size limit, portable to every ZFS"}, {"off", "no extended attributes (mount option noxattr)"}},
		Note:   "Extended attributes: sa (fast, in the dnode), dir (portable), off",
		Detail: "sa is strongly recommended for POSIX ACLs and SELinux. Data written with sa is not readable by ZFS implementations without the feature (all current OpenZFS have it)."},
	{Name: "snapdir", Kind: Settable, Type: TEnum, Inherit: true, Default: "hidden", Group: GroupMount, Types: "fs",
		Values: []Option{{"hidden", ".zfs exists but is not listed"}, {"visible", ".zfs is listed in the root of the file system"}, {"disabled", "no .zfs directory at all"}},
		Note:   "The .zfs/snapshot directory: hidden, visible or disabled"},
	{Name: "nbmand", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "off", Group: GroupMount, Types: "fs", Linux: true,
		Note:   "Non-blocking mandatory locks (SMB); not supported by FreeBSD",
		Detail: "Takes effect on remount. Only Linux before 5.15 implemented it (badly); FreeBSD ignores it."},
	{Name: "jailed", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "off", Group: GroupMount, Types: "fs",
		Note:   "on: the dataset is managed from inside a jail (zfs jail / jail.conf allow.mount.zfs)",
		Detail: "A jailed dataset is not mounted by the host; zfs jail JID DS attaches it to the jail, which then mounts and administers it. See zfs-jail(8)."},
	// ---------------------------------------------------------- Space
	{Name: "quota", Kind: Settable, Type: TSize, Default: "none", Group: GroupSpace, Types: "fs",
		Note:   "Hard limit on the space the dataset and all its descendants (snapshots included) may use",
		Detail: "A quota on a descendant adds a further limit, it never loosens an ancestor's. Cannot be below the space already used. Not on volumes: volsize is their quota."},
	{Name: "refquota", Kind: Settable, Type: TSize, Default: "none", Group: GroupSpace, Types: "fs",
		Note:   "Hard limit on the space the dataset itself references, not counting descendants and snapshots",
		Detail: "The usual per-user home directory quota: snapshots do not eat into it."},
	{Name: "reservation", Short: "reserv", Kind: Settable, Type: TSize, Default: "none", Group: GroupSpace, Types: "fs,vol",
		Note:   "Space guaranteed to the dataset and its descendants; counted as used by the parent",
		Detail: "Below the reservation the dataset is accounted as if it used that much; the reservation counts against the parent's quotas and reservations."},
	{Name: "refreservation", Short: "refreserv", Kind: Settable, Type: TSize, Default: "none", Group: GroupSpace, Types: "fs,vol",
		Note:   "Space guaranteed to the dataset itself (not descendants); auto = thick-provision a volume",
		Detail: "Snapshots are refused unless the pool has room for the referenced bytes outside the refreservation. auto (volumes only) sets it to what a fully written volume needs; none makes a volume sparse."},
	{Name: "filesystem_limit", Kind: Settable, Type: TCount, Default: "none", Group: GroupSpace, Types: "fs", Feature: "filesystem_limits",
		Note:   "Maximum number of file systems and volumes below this point",
		Detail: "Not enforced against a user allowed to change the limit (root). A limit on a descendant is an additional one."},
	{Name: "snapshot_limit", Kind: Settable, Type: TCount, Default: "none", Group: GroupSpace, Types: "fs,vol", Feature: "filesystem_limits",
		Note:   "Maximum number of snapshots of this dataset and its descendants",
		Detail: "Not enforced against a user allowed to change the limit (root): meant for delegated users and jails."},
	{Name: "defaultuserquota", Kind: Settable, Type: TSize, Default: "none", Group: GroupSpace, Types: "fs",
		Note: "Space quota for every user that has no userquota@ of their own (0/none disables)"},
	{Name: "defaultgroupquota", Kind: Settable, Type: TSize, Default: "none", Group: GroupSpace, Types: "fs",
		Note: "Space quota for every group without a groupquota@ of its own"},
	{Name: "defaultprojectquota", Kind: Settable, Type: TSize, Default: "none", Group: GroupSpace, Types: "fs",
		Note: "Space quota for every project without a projectquota@ of its own"},
	{Name: "defaultuserobjquota", Kind: Settable, Type: TCount, Default: "none", Group: GroupSpace, Types: "fs",
		Note: "Object (file) count quota for every user without a userobjquota@"},
	{Name: "defaultgroupobjquota", Kind: Settable, Type: TCount, Default: "none", Group: GroupSpace, Types: "fs",
		Note: "Object count quota for every group without a groupobjquota@"},
	{Name: "defaultprojectobjquota", Kind: Settable, Type: TCount, Default: "none", Group: GroupSpace, Types: "fs",
		Note: "Object count quota for every project without a projectobjquota@"},
	{Name: "userquota@", Kind: Settable, Type: TSize, Family: true, Default: "none", Group: GroupSpace, Types: "fs",
		Note:   "userquota@USER: space the user may own in this file system (name, uid, or SID)",
		Detail: "Enforcement lags a few seconds. Not shown by zfs get all: ask for userquota@NAME explicitly, or zfs userspace. Unprivileged users see only their own."},
	{Name: "groupquota@", Kind: Settable, Type: TSize, Family: true, Default: "none", Group: GroupSpace, Types: "fs",
		Note: "groupquota@GROUP: space files of the group may use in this file system"},
	{Name: "projectquota@", Kind: Settable, Type: TSize, Family: true, Default: "none", Group: GroupSpace, Types: "fs", Feature: "project_quota",
		Note: "projectquota@ID: space the project may use (projects: zfs project / chattr -p)"},
	{Name: "userobjquota@", Kind: Settable, Type: TCount, Family: true, Default: "none", Group: GroupSpace, Types: "fs", Feature: "userobj_accounting",
		Note: "userobjquota@USER: number of objects (files, with xattr=dir also their xattrs) the user may own"},
	{Name: "groupobjquota@", Kind: Settable, Type: TCount, Family: true, Default: "none", Group: GroupSpace, Types: "fs", Feature: "userobj_accounting",
		Note: "groupobjquota@GROUP: number of objects the group may own"},
	{Name: "projectobjquota@", Kind: Settable, Type: TCount, Family: true, Default: "none", Group: GroupSpace, Types: "fs", Feature: "project_quota",
		Note: "projectobjquota@ID: number of objects the project may own"},
	// ---------------------------------------------------------- Data
	{Name: "compression", Short: "compress", Kind: Settable, Type: TCompress, Inherit: true, Default: "on", Group: GroupData, Types: "fs,vol", NewData: true,
		Values: []Option{{"on", "the pool's default algorithm (lz4 when the lz4_compress feature is on, else lzjb)"}, {"off", "no compression"}, {"lz4", "fast, the usual choice"}, {"zstd", "zstd-3: better ratio than lz4, still fast (feature zstd_compress)"}, {"zstd-N", "zstd level 1 (fastest) … 19 (best ratio)"}, {"zstd-fast", "zstd-fast-1: faster than zstd at a lower ratio"}, {"zstd-fast-N", "N in 1–10, 20, 30 … 100, 500, 1000: ever faster, ever less compression"}, {"gzip", "gzip-6"}, {"gzip-N", "gzip level 1 (fastest) … 9 (best ratio); slow, compressible data only"}, {"lzjb", "the original ZFS algorithm"}, {"zle", "only compresses runs of zeros"}},
		Note:   "Compression algorithm for new writes: lz4, zstd[-N], gzip[-N], lzjb, zle, on (default), off",
		Detail: "Only newly written blocks are compressed with the new setting. Any setting but off also stores all-zero blocks as holes. A block is stored compressed only when it saves at least one sector and 12.5%: small recordsizes on 4K sectors compress poorly."},
	{Name: "checksum", Kind: Settable, Type: TEnum, Inherit: true, Default: "on", Group: GroupData, Types: "fs,vol", NewData: true,
		Values: []Option{{"on", "the default algorithm (fletcher4)"}, {"fletcher4", "fast, the default"}, {"sha256", "cryptographic; what dedup uses by default"}, {"sha512", "faster than sha256 on 64-bit CPUs (feature sha512)"}, {"skein", "cryptographic, fast (feature skein)"}, {"edonr", "cryptographic, fastest; dedup needs verify with it (feature edonr)"}, {"blake3", "cryptographic, very fast (feature blake3)"}, {"fletcher2", "weak, legacy"}, {"off", "no integrity checking of user data — not recommended"}},
		Note:   "Checksum of new blocks: on (fletcher4), sha256/sha512/skein/edonr/blake3, off (not recommended)",
		Detail: "sha512, skein, edonr and blake3 need their pool feature enabled. Changing it affects only new data. off disables integrity checking of user data; metadata is always checksummed."},
	{Name: "copies", Kind: Settable, Type: TEnum, Inherit: true, Default: "1", Group: GroupData, Types: "fs,vol", NewData: true,
		Values: []Option{{"1", ""}, {"2", "every block stored twice, on different disks when possible"}, {"3", "three copies (not allowed on encrypted datasets)"}},
		Note:   "Extra copies of every block of new data, on top of the pool's redundancy; charged to the dataset",
		Detail: "Only new writes get the extra copies. Not a substitute for a redundant pool: a pool missing a top-level vdev does not import, copies or not."},
	{Name: "dedup", Kind: Settable, Type: TDedup, Inherit: true, Default: "off", Group: GroupData, Types: "fs,vol", NewData: true,
		Values: []Option{{"off", ""}, {"on", "deduplicate new blocks with sha256"}, {"verify", "sha256,verify: byte-compare blocks with equal checksums"}, {"sha256", ""}, {"sha256,verify", ""}, {"sha512", ""}, {"sha512,verify", ""}, {"skein", ""}, {"skein,verify", ""}, {"edonr,verify", "edonr requires verify"}, {"blake3", ""}, {"blake3,verify", ""}},
		Note:   "Deduplication of new writes (off, on, verify, ALGO[,verify]); needs lots of RAM — rarely worth it",
		Detail: "The dedup checksum overrides the checksum property. The dedup table lives in RAM/L2ARC: a few GB per TB of unique data, or the pool crawls. Unless necessary, do not enable it (zfsconcepts(7))."},
	{Name: "recordsize", Short: "recsize", Kind: Settable, Type: TSize, Inherit: true, Default: "128K", Group: GroupData, Types: "fs", NewData: true,
		Note:   "Maximum block size of new files: 512 … 16M, power of two (default 128K; >128K needs feature large_blocks)",
		Detail: "Meant for databases and other fixed-record workloads (match their page size, e.g. 16K for InnoDB, 8K for PostgreSQL); for general use leave the default. Existing files keep their block size. 1M is common for media/backup data."},
	{Name: "special_small_blocks", Kind: Settable, Type: TSize, Inherit: true, Default: "0", Group: GroupData, Types: "fs,vol", NewData: true,
		Note:   "Blocks up to this size (0 … 16M, power of two) go to the pool's special vdev; 0 = metadata only",
		Detail: "Only meaningful with a special allocation class vdev in the pool (zpoolconcepts(7)). Set it to recordsize to put a whole dataset on the special vdev. Compared after compression."},
	{Name: "redundant_metadata", Kind: Settable, Type: TEnum, Inherit: true, Default: "all", Group: GroupData, Types: "fs,vol", NewData: true,
		Values: []Option{{"all", "an extra copy of all metadata: a corrupt block loses at most one data block"}, {"most", "extra copy of most metadata: faster random writes, up to ~1000 blocks at risk per corrupt block"}, {"some", "extra copy of critical metadata only: faster file creation, files/directories at risk"}, {"none", "no extra copies: a corrupt block can lose the whole dataset"}},
		Note:   "How much metadata gets an extra copy on top of pool redundancy and copies"},
	{Name: "dnodesize", Short: "dnsize", Kind: Settable, Type: TEnum, Inherit: true, Default: "legacy", Group: GroupData, Types: "fs", Feature: "large_dnode", NewData: true,
		Values: []Option{{"legacy", "512-byte dnodes, compatible with every ZFS"}, {"auto", "ZFS picks the size: good with xattr=sa and many xattrs (Samba, SELinux)"}, {"1k", ""}, {"2k", ""}, {"4k", ""}, {"8k", ""}, {"16k", ""}},
		Note:   "Size of new dnodes: legacy (512), auto, or 1k…16k (feature large_dnode)",
		Detail: "auto helps workloads with many extended attributes. Leave legacy if the dataset must be sent to, or the pool imported on, a system without large_dnode."},
	// ---------------------------------------------------------- Caching & I/O
	{Name: "primarycache", Kind: Settable, Type: TEnum, Inherit: true, Default: "all", Group: GroupIO, Types: "fs,vol",
		Values: []Option{{"all", "cache data and metadata in the ARC"}, {"metadata", "only metadata (databases/VMs with their own cache)"}, {"none", "nothing"}},
		Note:   "What the ARC (RAM cache) keeps for this dataset: all, metadata, none"},
	{Name: "secondarycache", Kind: Settable, Type: TEnum, Inherit: true, Default: "all", Group: GroupIO, Types: "fs,vol",
		Values: []Option{{"all", "data and metadata may go to the L2ARC"}, {"metadata", "only metadata"}, {"none", "nothing"}},
		Note:   "What the L2ARC (cache device) keeps for this dataset: all, metadata, none"},
	{Name: "prefetch", Kind: Settable, Type: TEnum, Inherit: true, Default: "all", Group: GroupIO, Types: "fs,vol",
		Values: []Option{{"all", "prefetch data and metadata"}, {"metadata", "prefetch only metadata"}, {"none", "no speculative prefetch"}},
		Note:   "Speculative prefetch: all, metadata, none (the sysctl zfs_prefetch_disable overrides it)"},
	{Name: "logbias", Kind: Settable, Type: TEnum, Inherit: true, Default: "latency", Group: GroupIO, Types: "fs,vol",
		Values: []Option{{"latency", "synchronous writes go to the log device (SLOG) for low latency"}, {"throughput", "bypass the log device: optimise for pool throughput (large streaming sync writes)"}},
		Note:   "Synchronous writes: latency (use the SLOG) or throughput (skip it)"},
	{Name: "sync", Kind: Settable, Type: TEnum, Inherit: true, Default: "standard", Group: GroupIO, Types: "fs,vol",
		Values: []Option{{"standard", "POSIX: fsync/O_SYNC wait for stable storage"}, {"always", "every transaction is synchronous — large performance penalty"}, {"disabled", "synchronous requests are ignored: fast, and dangerous for databases, NFS and anything that relies on fsync"}},
		Note:   "Synchronous writes: standard, always, or disabled (fast, unsafe on power loss)",
		Detail: "disabled means data an application was told is on stable storage may be lost on a crash or power failure (up to the txg interval, ~5 s). Use it only for throw-away data."},
	{Name: "direct", Kind: Settable, Type: TEnum, Inherit: true, Default: "standard", Group: GroupIO, Types: "fs",
		Values: []Option{{"standard", "O_DIRECT bypasses the ARC when the request is aligned"}, {"always", "every aligned read/write is treated as direct"}, {"disabled", "O_DIRECT is ignored, everything goes through the ARC (the pre-2.3 behaviour)"}},
		Note:   "Direct I/O (O_DIRECT): standard, always, disabled",
		Detail: "Direct writes must be recordsize-aligned and page-aligned; mmap'ed files fall back to buffered. Not for zvols; incompatible with dedup."},
	// ---------------------------------------------------------- Sharing
	{Name: "sharenfs", Kind: Settable, Type: TShare, Inherit: true, Default: "off", Group: GroupShare, Types: "fs",
		Values: []Option{{"off", "not exported by ZFS (use /etc/exports yourself)"}, {"on", "exported to everyone read-write with the default options"}, {"OPTIONS", "exports(5) options, comma-separated; FreeBSD: several host sets separated by ;"}},
		Note:   "NFS export managed by ZFS: off, on, or exports(5) options (FreeBSD: comma-separated, ; between host sets)",
		Detail: "ZFS writes the export lines to /etc/zfs/exports and tells mountd; nfsd and mountd must be enabled (nfs_server_enable, mountd_enable). Changing it reshares children that inherit it. zfs set -u changes the property without sharing or unsharing."},
	{Name: "sharesmb", Kind: Settable, Type: TShare, Inherit: true, Default: "off", Group: GroupShare, Types: "fs", Linux: true,
		Values: []Option{{"off", ""}, {"on", "Samba USERSHARE named after the dataset (Linux only)"}},
		Note:   "SMB share through Samba usershares (Linux); FreeBSD has no usershare support — configure smb4.conf instead"},
	// ---------------------------------------------------------- ACLs
	{Name: "acltype", Kind: Settable, Type: TEnum, Inherit: true, Default: "nfsv4", Group: GroupACL, Types: "fs",
		Values: []Option{{"nfsv4", "NFSv4 ACLs (getfacl/setfacl, facl) — the FreeBSD default"}, {"posix", "POSIX draft ACLs — Linux only, behaves as off on FreeBSD"}, {"off", "no ACLs beyond the mode bits"}},
		Note:   "ACL flavour: nfsv4 (FreeBSD default), posix (Linux only), off"},
	{Name: "aclmode", Kind: Settable, Type: TEnum, Inherit: true, Default: "discard", Group: GroupACL, Types: "fs",
		Values: []Option{{"discard", "chmod drops every ACE that is not the mode (the default)"}, {"groupmask", "chmod reduces ALLOW entries to the group bits"}, {"passthrough", "chmod only updates the owner@/group@/everyone@ entries, other ACEs survive"}, {"restricted", "chmod fails on a file with a non-trivial ACL"}},
		Note:   "What chmod(2) does to a non-trivial NFSv4 ACL: discard, groupmask, passthrough, restricted",
		Detail: "passthrough (or restricted) is what you want when ACLs are managed deliberately: with discard any chmod — including by an editor or an installer — wipes them."},
	{Name: "aclinherit", Kind: Settable, Type: TEnum, Inherit: true, Default: "restricted", Group: GroupACL, Types: "fs",
		Values: []Option{{"discard", "new files inherit no ACEs"}, {"noallow", "only DENY entries are inherited"}, {"restricted", "inherited ACEs lose write_acl and write_owner (the default)"}, {"passthrough", "inherited ACEs are copied unchanged; the mode comes from them"}, {"passthrough-x", "like passthrough, but owner@/group@/everyone@ get execute only if the creating mode asks for it"}},
		Note:   "How inheritable ACEs are applied to new files: discard, noallow, restricted, passthrough[-x]",
		Detail: "passthrough is the usual choice for shared directories managed with ACLs; restricted silently strips write_acl/write_owner from what children inherit. Does not apply to POSIX ACLs."},
	// ---------------------------------------------------------- Volumes
	{Name: "volsize", Kind: Settable, Type: TSize, Default: "", Group: GroupVolume, Types: "vol",
		Note:   "Logical size of the volume (a multiple of volblocksize); growing it grows the refreservation too",
		Detail: "Shrinking a volume in use corrupts whatever is on it — extreme care. A sparse volume (refreservation below volsize) can run out of pool space and fail writes with ENOSPC."},
	{Name: "volblocksize", Short: "volblock", Kind: CreateOnly, Type: TSize, Inherit: true, Default: "16K", Group: GroupVolume, Types: "vol",
		Note:   "Block size of the volume: 512 … 16M, power of two (default 16K); fixed once written — set at zfs create -V",
		Detail: "Match the consumer (e.g. the guest file system block size, 4K–64K). Cannot be changed once the volume has data."},
	{Name: "volmode", Kind: Settable, Type: TEnum, Inherit: true, Default: "default", Group: GroupVolume, Types: "vol",
		Values: []Option{{"default", "the system-wide vfs.zfs.vol.mode decides (full unless tuned)"}, {"full", "a full block device under /dev/zvol, partitions exposed by GEOM"}, {"geom", "alias of full"}, {"dev", "a device node without partition probing"}, {"none", "not exposed at all: snapshot/send/receive only"}},
		Note:   "How the volume is exposed to the OS: default, full (geom), dev, none"},
	{Name: "snapdev", Kind: Settable, Type: TEnum, Inherit: true, Default: "hidden", Group: GroupVolume, Types: "vol",
		Values: []Option{{"hidden", "no device nodes for volume snapshots"}, {"visible", "/dev/zvol/POOL/VOL@SNAP device nodes exist (read-only)"}},
		Note:   "Whether volume snapshots get device nodes under /dev/zvol"},
	{Name: "volthreading", Kind: Settable, Type: TBool, Values: onoff, Default: "on", Group: GroupVolume, Types: "vol", Linux: true,
		Note: "Internal zvol threading (Linux only; overridden by zvol_request_sync)"},
	// ---------------------------------------------------------- Encryption
	{Name: "encryption", Kind: CreateOnly, Type: TEnum, Inherit: true, Default: "off", Group: GroupCrypto, Types: "fs,vol", Feature: "encryption",
		Values: []Option{{"off", ""}, {"on", "aes-256-gcm"}, {"aes-128-ccm", ""}, {"aes-192-ccm", ""}, {"aes-256-ccm", ""}, {"aes-128-gcm", ""}, {"aes-192-gcm", ""}, {"aes-256-gcm", "the default suite"}},
		Note:   "Encryption cipher suite; chosen at creation (zfs create -o encryption=on -o keyformat=…) and never changed"},
	{Name: "keyformat", Kind: CreateOnly, Type: TEnum, Default: "none", Group: GroupCrypto, Types: "fs,vol",
		Values: []Option{{"none", ""}, {"raw", "32 random bytes"}, {"hex", "64 hex digits"}, {"passphrase", "8–512 bytes, run through PBKDF2"}},
		Note:   "Format of the wrapping key: raw, hex, passphrase; change it with zfs change-key, not zfs set"},
	{Name: "keylocation", Kind: Settable, Type: TKeyloc, Default: "none", Group: GroupCrypto, Types: "fs,vol",
		Values: []Option{{"prompt", "ask on the terminal (or read stdin) when the key is needed"}, {"file:///PATH", "read the key from an absolute path"}, {"https://HOST/…", "fetch it (SSL_CA_CERT_FILE etc. for the trust store)"}, {"http://HOST/…", ""}},
		Note:   "Where zfs load-key / zfs mount -l find the key (encryption roots only): prompt, file://, https://, http://",
		Detail: "Settable only on an encryption root (encryptionroot = the dataset itself); zfs change-key can set it too."},
	{Name: "pbkdf2iters", Kind: CreateOnly, Type: TNumber, Default: "0", Group: GroupCrypto, Types: "fs,vol",
		Note: "PBKDF2 iterations for a passphrase key (min 100000, default 350000); change with zfs change-key -o pbkdf2iters=N"},
	// ---------------------------------------------------------- Names
	{Name: "casesensitivity", Kind: CreateOnly, Type: TEnum, Inherit: true, Default: "sensitive", Group: GroupNames, Types: "fs",
		Values: []Option{{"sensitive", "POSIX: Foo and foo differ"}, {"insensitive", "Foo and foo are the same file"}, {"mixed", "both styles of lookup, for SMB servers"}},
		Note:   "File name matching: sensitive, insensitive, mixed — fixed at creation (zfs create -o casesensitivity=…)"},
	{Name: "normalization", Kind: CreateOnly, Type: TEnum, Inherit: true, Default: "none", Group: GroupNames, Types: "fs",
		Values: []Option{{"none", ""}, {"formC", ""}, {"formD", "what macOS clients expect"}, {"formKC", ""}, {"formKD", ""}},
		Note:   "Unicode normalization used when comparing file names — fixed at creation; implies utf8only=on"},
	{Name: "utf8only", Kind: CreateOnly, Type: TBool, Inherit: true, Values: onoff, Default: "off", Group: GroupNames, Types: "fs",
		Note: "Reject file names that are not valid UTF-8 — fixed at creation"},
	{Name: "longname", Kind: Settable, Type: TBool, Inherit: true, Values: bo("file names up to 1023 bytes", "255 bytes, portable"), Default: "off", Group: GroupNames, Types: "fs", Feature: "longname",
		Note: "Allow file names longer than 255 bytes (feature longname; such a dataset needs OpenZFS 2.3+ everywhere)"},
	// ---------------------------------------------------------- SELinux
	{Name: "context", Kind: Settable, Type: TString, Default: "none", Group: GroupSELinux, Types: "fs", Linux: true,
		Note: "SELinux context of every file (Linux mount option context=)"},
	{Name: "fscontext", Kind: Settable, Type: TString, Default: "none", Group: GroupSELinux, Types: "fs", Linux: true,
		Note: "SELinux context of the file system itself (fscontext=)"},
	{Name: "defcontext", Kind: Settable, Type: TString, Default: "none", Group: GroupSELinux, Types: "fs", Linux: true,
		Note: "SELinux default context for unlabelled files (defcontext=)"},
	{Name: "rootcontext", Kind: Settable, Type: TString, Default: "none", Group: GroupSELinux, Types: "fs", Linux: true,
		Note: "SELinux context of the root inode (rootcontext=)"},
	// ---------------------------------------------------------- Other
	{Name: "version", Kind: Settable, Type: TVersion, Default: "5", Group: GroupOther, Types: "fs",
		Values: []Option{{"current", "the newest version this ZFS supports"}, {"5", "the current one (userspace accounting, system attributes)"}, {"4", ""}, {"3", ""}, {"2", ""}, {"1", ""}},
		Note:   "On-disk file system version; can only go up (zfs upgrade)"},
	{Name: "vscan", Kind: Settable, Type: TBool, Inherit: true, Values: onoff, Default: "off", Group: GroupOther, Types: "fs", Linux: true,
		Note: "Virus scanning on open/close (Solaris); not used by OpenZFS"},
	{Name: "mlslabel", Kind: Settable, Type: TString, Inherit: true, Default: "none", Group: GroupOther, Types: "fs", Linux: true,
		Note: "Solaris Trusted Extensions sensitivity label; not used by OpenZFS (FreeBSD refuses it)"},
	// ---------------------------------------------------------- Statistics
	{Name: "type", Kind: Readonly, Group: GroupStats, Types: "all", Note: "filesystem, volume, snapshot or bookmark"},
	{Name: "creation", Kind: Readonly, Group: GroupStats, Types: "all", Note: "When the dataset was created"},
	{Name: "used", Kind: Readonly, Group: GroupStats, Types: "all", Note: "Space consumed by the dataset and all its descendants (what quota and reservation are checked against)",
		Detail: "used = usedbydataset + usedbychildren + usedbysnapshots + usedbyrefreservation. For a snapshot: the space only it references, freed when it is destroyed."},
	{Name: "available", Short: "avail", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "Space available to the dataset and its children (pool free space, quotas and reservations considered)"},
	{Name: "referenced", Short: "refer", Kind: Readonly, Group: GroupStats, Types: "all", Note: "Data accessible through this dataset, shared with snapshots/clones or not"},
	{Name: "logicalused", Short: "lused", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "used before compression and copies: roughly what applications see"},
	{Name: "logicalreferenced", Short: "lrefer", Kind: Readonly, Group: GroupStats, Types: "all", Note: "referenced before compression and copies"},
	{Name: "compressratio", Kind: Readonly, Group: GroupStats, Types: "all", Note: "Compression ratio achieved on used (descendants included)"},
	{Name: "refcompressratio", Kind: Readonly, Group: GroupStats, Types: "all", Note: "Compression ratio achieved on referenced"},
	{Name: "usedbydataset", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "Space freed if the dataset itself were destroyed (after its refreservation, snapshots, descendants)"},
	{Name: "usedbychildren", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "Space freed if every child were destroyed"},
	{Name: "usedbysnapshots", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "Space freed if every snapshot of the dataset were destroyed (not the sum of their used: they share)"},
	{Name: "usedbyrefreservation", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "Space held by the refreservation, freed if it were removed"},
	{Name: "written", Kind: Readonly, Group: GroupStats, Types: "all", Note: "Referenced space written since the previous snapshot"},
	{Name: "mounted", Kind: Readonly, Group: GroupStats, Types: "fs", Note: "yes if the file system is mounted"},
	{Name: "origin", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "For a clone: the snapshot it was cloned from (zfs promote swaps the roles)"},
	{Name: "clones", Kind: Readonly, Group: GroupStats, Types: "snap", Note: "For a snapshot: its clones (a snapshot with clones cannot be destroyed)"},
	{Name: "defer_destroy", Kind: Readonly, Group: GroupStats, Types: "snap", Note: "yes if the snapshot was marked for deferred destruction (zfs destroy -d)"},
	{Name: "userrefs", Kind: Readonly, Group: GroupStats, Types: "snap", Note: "Number of user holds on the snapshot (zfs hold)"},
	{Name: "encryptionroot", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "The dataset whose key this one uses (zfs load-key there loads it for all)"},
	{Name: "keystatus", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "none, available (key loaded) or unavailable"},
	{Name: "filesystem_count", Kind: Readonly, Group: GroupStats, Types: "fs", Note: "File systems and volumes below this point (only when a filesystem_limit is set above)"},
	{Name: "snapshot_count", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "Snapshots below this point (only when a snapshot_limit is set above)"},
	{Name: "snapshots_changed", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "When a snapshot was last created or destroyed"},
	{Name: "receive_resume_token", Kind: Readonly, Group: GroupStats, Types: "fs,vol", Note: "Token of an interrupted zfs receive -s, for zfs send -t"},
	{Name: "redact_snaps", Kind: Readonly, Group: GroupStats, Types: "snap", Note: "For redacted snapshots/bookmarks: the snapshot GUIDs of the redaction list"},
	{Name: "guid", Kind: Readonly, Group: GroupStats, Types: "all", Note: "64-bit id that survives send/receive (identifies a snapshot across pools)"},
	{Name: "objsetid", Kind: Readonly, Group: GroupStats, Types: "all", Note: "Id of the dataset within the pool (not preserved by send/receive, may be reused)"},
	{Name: "createtxg", Kind: Readonly, Group: GroupStats, Types: "all", Note: "Transaction group the dataset was created in (orders snapshots for incremental sends)"},
	{Name: "userused@", Kind: Readonly, Family: true, Group: GroupStats, Types: "fs", Note: "userused@USER: space owned by the user here (zfs userspace)"},
	{Name: "groupused@", Kind: Readonly, Family: true, Group: GroupStats, Types: "fs", Note: "groupused@GROUP: space owned by the group here (zfs groupspace)"},
	{Name: "projectused@", Kind: Readonly, Family: true, Group: GroupStats, Types: "fs", Note: "projectused@ID: space of the project here (zfs projectspace)"},
	{Name: "userobjused@", Kind: Readonly, Family: true, Group: GroupStats, Types: "fs", Note: "userobjused@USER: objects owned by the user"},
	{Name: "groupobjused@", Kind: Readonly, Family: true, Group: GroupStats, Types: "fs", Note: "groupobjused@GROUP: objects owned by the group"},
	{Name: "projectobjused@", Kind: Readonly, Family: true, Group: GroupStats, Types: "fs", Note: "projectobjused@ID: objects of the project"},
	{Name: "written@", Kind: Readonly, Family: true, Group: GroupStats, Types: "fs,vol", Note: "written@SNAP: referenced space written since that snapshot"},
	{Name: "written#", Kind: Readonly, Family: true, Group: GroupStats, Types: "fs,vol", Note: "written#BOOKMARK: referenced space written since that bookmark"},
}

var byName = map[string]*Prop{}
var byShort = map[string]*Prop{}

func init() {
	for i := range Catalogue {
		p := &Catalogue[i]
		byName[p.Name] = p
		if p.Short != "" {
			byShort[p.Short] = p
		}
	}
}

// Lookup finds a native property by name or alias; for a per-key property
// (userquota@bob) it returns the family entry. User properties are not in
// the catalogue: Lookup returns a synthetic entry for them.
func Lookup(name string) (*Prop, bool) {
	if p, ok := byName[name]; ok {
		return p, true
	}
	if p, ok := byShort[name]; ok {
		return p, true
	}
	for _, sep := range []string{"@", "#"} {
		if i := strings.Index(name, sep); i > 0 {
			if p, ok := byName[name[:i+1]]; ok && p.Family {
				return p, true
			}
		}
	}
	if IsUser(name) {
		return &Prop{Name: name, Kind: Settable, Type: TString, Inherit: true, Group: GroupUser, Types: "all",
			Note: "User property (module:name): an arbitrary string for your own tools, inherited, never interpreted by ZFS"}, true
	}
	return nil, false
}

// Known reports whether name is a native property, a family member or a
// syntactically valid user property.
func Known(name string) bool {
	if _, ok := Lookup(name); ok {
		return !IsUser(name) || ValidUserName(name)
	}
	return false
}

// Canonical maps an alias (avail, compress, …) to the property name.
func Canonical(name string) string {
	if p, ok := byShort[name]; ok {
		return p.Name
	}
	return name
}

// Family returns the family prefix of a per-key property ("userquota@" for
// userquota@bob), or "" when name is not one.
func Family(name string) string {
	for _, sep := range []string{"@", "#"} {
		if i := strings.Index(name, sep); i > 0 {
			if p, ok := byName[name[:i+1]]; ok && p.Family {
				return p.Name
			}
		}
	}
	return ""
}

// AppliesTo reports whether the property applies to a dataset of this type
// (filesystem, volume, snapshot, bookmark).
func (p *Prop) AppliesTo(dsType string) bool {
	if p.Types == "all" || p.Types == "" {
		return true
	}
	short := map[string]string{"filesystem": "fs", "volume": "vol", "snapshot": "snap", "bookmark": "bm"}[dsType]
	for _, t := range strings.Split(p.Types, ",") {
		if t == short {
			return true
		}
	}
	return false
}

// Editable reports whether zfs set accepts the property.
func (p *Prop) Editable() bool { return p.Kind == Settable }

// TypesLabel is the human form of Types.
func (p *Prop) TypesLabel() string {
	switch p.Types {
	case "fs":
		return "file systems"
	case "vol":
		return "volumes"
	case "fs,vol":
		return "file systems and volumes"
	case "snap":
		return "snapshots"
	}
	return "every dataset"
}

// ByGroup returns the catalogue grouped in GroupOrder.
func ByGroup() map[string][]*Prop {
	m := map[string][]*Prop{}
	for i := range Catalogue {
		p := &Catalogue[i]
		m[p.Group] = append(m[p.Group], p)
	}
	return m
}

// Names lists every catalogue name, sorted (families with their @/# suffix).
func Names() []string {
	var res []string
	for _, p := range Catalogue {
		res = append(res, p.Name)
	}
	sort.Strings(res)
	return res
}

// SettableNames lists the names zfs set accepts, sorted.
func SettableNames() []string {
	var res []string
	for _, p := range Catalogue {
		if p.Kind == Settable {
			res = append(res, p.Name)
		}
	}
	sort.Strings(res)
	return res
}
