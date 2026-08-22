package props

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ParseSize parses a ZFS size: digits with an optional decimal part and a
// unit suffix (b, k, m, g, t, p, e, z, optionally followed by "b"; "1.5g",
// "16K", "10737418240"). It returns the byte count.
func ParseSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.') {
		i++
	}
	num, unit := s[:i], strings.ToLower(strings.TrimSpace(s[i:]))
	if num == "" {
		return 0, fmt.Errorf("%q is not a size (e.g. 10G, 512K, 1.5T)", s)
	}
	unit = strings.TrimSuffix(unit, "b") // "10GB" == "10G", "100b" == 100
	shift := map[string]uint{"": 0, "k": 10, "m": 20, "g": 30, "t": 40, "p": 50, "e": 60, "z": 70}
	sh, ok := shift[unit]
	if !ok {
		return 0, fmt.Errorf("%q: unknown unit %q (use K, M, G, T, P, E)", s, unit)
	}
	if strings.Contains(num, ".") {
		f, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not a size", s)
		}
		v := f * math.Pow(2, float64(sh))
		if v > math.MaxUint64/2 || v < 0 {
			return 0, fmt.Errorf("%q is too large", s)
		}
		return uint64(v), nil
	}
	n, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size", s)
	}
	if sh > 0 && n > math.MaxUint64>>sh {
		return 0, fmt.Errorf("%q is too large", s)
	}
	return n << sh, nil
}

// FormatSize renders bytes the way zfs does (zfs_nicenum: 1.50G, 10.5G,
// 128K, 10T): an integer when the value is an exact multiple of the unit,
// otherwise two, one or no decimals depending on the magnitude.
func FormatSize(n uint64) string {
	units := []string{"", "K", "M", "G", "T", "P", "E"}
	i := 0
	for n>>(10*uint(i)) >= 1024 && i < len(units)-1 {
		i++
	}
	if i == 0 {
		return strconv.FormatUint(n, 10)
	}
	if n%(1<<(10*uint(i))) == 0 {
		return strconv.FormatUint(n>>(10*uint(i)), 10) + units[i]
	}
	f := float64(n) / float64(uint64(1)<<(10*uint(i)))
	switch {
	case f >= 100:
		return fmt.Sprintf("%.0f%s", f, units[i])
	case f >= 10:
		return fmt.Sprintf("%.1f%s", f, units[i])
	}
	return fmt.Sprintf("%.2f%s", f, units[i])
}

func isPow2(n uint64) bool { return n != 0 && n&(n-1) == 0 }

var (
	reGzip     = regexp.MustCompile(`^gzip-[1-9]$`)
	reZstd     = regexp.MustCompile(`^zstd-([1-9]|1[0-9])$`)
	reZstdFast = regexp.MustCompile(`^zstd-fast-([1-9]|10|20|30|40|50|60|70|80|90|100|500|1000)$`)
)

// Validate checks value for the property and returns a normalised value
// (trimmed; sizes kept as typed) or an error that explains what is accepted.
func (p *Prop) Validate(value string) (string, error) {
	v := strings.TrimSpace(value)
	if p.Kind == Readonly {
		return "", fmt.Errorf("%s is read-only", p.Name)
	}
	if p.Kind == CreateOnly {
		return "", fmt.Errorf("%s can only be set when the dataset is created", p.Name)
	}
	if IsUser(p.Name) {
		if len(value) > 8192 {
			return "", fmt.Errorf("user property values are at most 8192 bytes")
		}
		return value, nil
	}
	switch p.Type {
	case TBool:
		if v == "on" || v == "off" {
			return v, nil
		}
		return "", fmt.Errorf("%s is on or off", p.Name)
	case TEnum:
		for _, o := range p.Values {
			if o.Value == v {
				return v, nil
			}
		}
		return "", fmt.Errorf("%s is one of %s", p.Name, joinValues(p.Values))
	case TVersion:
		if v == "current" {
			return v, nil
		}
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 5 {
			return v, nil
		}
		return "", fmt.Errorf("version is 1 … 5 or current")
	case TCompress:
		switch {
		case v == "on", v == "off", v == "lz4", v == "lzjb", v == "zle", v == "gzip", v == "zstd", v == "zstd-fast":
			return v, nil
		case reGzip.MatchString(v), reZstd.MatchString(v), reZstdFast.MatchString(v):
			return v, nil
		}
		return "", fmt.Errorf("compression is on, off, lz4, zstd, zstd-1…19, zstd-fast, zstd-fast-N, gzip, gzip-1…9, lzjb or zle")
	case TDedup:
		for _, o := range p.Values {
			if o.Value == v {
				return v, nil
			}
		}
		return "", fmt.Errorf("dedup is off, on, verify, or sha256/sha512/skein/blake3 optionally with ,verify (edonr,verify)")
	case TSize:
		return p.validateSize(v)
	case TCount:
		if v == "none" {
			return v, nil
		}
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return strconv.FormatUint(n, 10), nil
		}
		return "", fmt.Errorf("%s is a count or none", p.Name)
	case TNumber:
		if _, err := strconv.ParseUint(v, 10, 64); err == nil {
			return v, nil
		}
		return "", fmt.Errorf("%s is a number", p.Name)
	case TPath:
		if v == "legacy" || v == "none" {
			return v, nil
		}
		if strings.HasPrefix(v, "/") {
			return strings.TrimRight(v, "/") + map[bool]string{true: "/", false: ""}[v == "/"], nil
		}
		return "", fmt.Errorf("mountpoint is an absolute path, legacy or none")
	case TKeyloc:
		if v == "prompt" || v == "none" || strings.HasPrefix(v, "file:///") || strings.HasPrefix(v, "https://") || strings.HasPrefix(v, "http://") {
			return v, nil
		}
		return "", fmt.Errorf("keylocation is prompt, file:///ABSOLUTE/PATH, https://… or http://…")
	case TShare:
		if v == "" {
			return "", fmt.Errorf("%s is on, off or share options", p.Name)
		}
		return v, nil
	}
	return v, nil
}

func (p *Prop) validateSize(v string) (string, error) {
	switch p.Name {
	case "recordsize", "volblocksize":
		n, err := ParseSize(v)
		if err != nil {
			return "", err
		}
		if n < 512 || n > 16<<20 || !isPow2(n) {
			return "", fmt.Errorf("%s is a power of two from 512 to 16M", p.Name)
		}
		return v, nil
	case "special_small_blocks":
		n, err := ParseSize(v)
		if err != nil {
			return "", err
		}
		if n != 0 && (n < 512 || n > 16<<20 || !isPow2(n)) {
			return "", fmt.Errorf("special_small_blocks is 0 or a power of two from 512 to 16M")
		}
		return v, nil
	case "volsize":
		n, err := ParseSize(v)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", fmt.Errorf("volsize cannot be zero")
		}
		return v, nil
	case "refreservation":
		if v == "none" || v == "auto" {
			return v, nil
		}
	default:
		if v == "none" {
			return v, nil
		}
	}
	if _, err := ParseSize(v); err != nil {
		return "", fmt.Errorf("%s is a size (10G, 512M, …) or none", p.Name)
	}
	return v, nil
}

func joinValues(opts []Option) string {
	var vs []string
	for _, o := range opts {
		vs = append(vs, o.Value)
	}
	return strings.Join(vs, ", ")
}

// Choices returns the selectable values of an enum-like property, expanding
// the compression levels; nil for free-form properties.
func (p *Prop) Choices() []Option {
	switch p.Type {
	case TBool, TEnum, TDedup, TVersion:
		return p.Values
	case TCompress:
		var res []Option
		for _, o := range p.Values {
			switch o.Value {
			case "zstd-N":
				for i := 1; i <= 19; i++ {
					note := ""
					switch i {
					case 1:
						note = "fastest zstd"
					case 3:
						note = "= zstd"
					case 19:
						note = "best ratio, slow"
					}
					res = append(res, Option{fmt.Sprintf("zstd-%d", i), note})
				}
			case "zstd-fast-N":
				for _, i := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 500, 1000} {
					note := ""
					if i == 1000 {
						note = "fastest, least compression"
					}
					res = append(res, Option{fmt.Sprintf("zstd-fast-%d", i), note})
				}
			case "gzip-N":
				for i := 1; i <= 9; i++ {
					note := ""
					if i == 6 {
						note = "= gzip"
					}
					res = append(res, Option{fmt.Sprintf("gzip-%d", i), note})
				}
			default:
				res = append(res, o)
			}
		}
		return res
	}
	return nil
}

// FreeForm reports whether the property takes typed text (sizes, paths,
// strings) rather than a choice.
func (p *Prop) FreeForm() bool { return p.Choices() == nil || p.Type == TShare || p.Type == TKeyloc }

// Hint is a one-line reminder of the accepted syntax for free-form properties.
func (p *Prop) Hint() string {
	switch p.Type {
	case TSize:
		switch p.Name {
		case "recordsize", "volblocksize":
			return "512 … 16M, power of two (e.g. 16K, 1M)"
		case "special_small_blocks":
			return "0, or 512 … 16M power of two"
		case "volsize":
			return "a size (e.g. 20G), multiple of volblocksize"
		case "refreservation":
			return "a size (e.g. 10G), none, or auto (volumes)"
		}
		return "a size (e.g. 10G, 512M) or none"
	case TCount:
		return "a number or none"
	case TNumber:
		return "a number"
	case TPath:
		return "/absolute/path, legacy or none"
	case TKeyloc:
		return "prompt, file:///path, https://…"
	case TShare:
		return "on, off, or exports(5) options (rw,maproot=root,-network=10.0.0.0/24)"
	}
	if IsUser(p.Name) {
		return "any text (up to 8192 bytes)"
	}
	return ""
}
