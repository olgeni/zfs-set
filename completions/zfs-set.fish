# fish completion for zfs-set(1). Install as
# /usr/local/share/fish/vendor_completions.d/zfs-set.fish or ~/.config/fish/completions/zfs-set.fish.

function __zfs_set_datasets
    zfs list -H -o name -t filesystem,volume 2>/dev/null
end

function __zfs_set_props
    zfs-set -catalogue 2>/dev/null | awk '/^  [a-z]/ { print $1 }' | grep -v '[@#]$'
end

function __zfs_set_settable
    zfs-set -catalogue 2>/dev/null | awk '/settable/ { print $1 }' | grep -v '[@#]$'
end

function __zfs_set_assign
    set -l tok (commandline -ct)
    if string match -q '*=*' -- $tok
        set -l p (string split -m1 = -- $tok)[1]
        zfs-set -describe $p 2>/dev/null | awk -v p=$p '/^    [a-z0-9]/ { print p"="$1 }'
    else
        __zfs_set_settable | sed 's/$/=/'
    end
end

complete -c zfs-set -o version -d 'Print version and exit'
complete -c zfs-set -o list -d 'Print every property with value, source and meaning'
complete -c zfs-set -o local -d 'With -list: only the properties set here'
complete -c zfs-set -o get -x -a '(__fish_complete_list , __zfs_set_props)' -d 'Print the named properties'
complete -c zfs-set -o set -x -a '(__zfs_set_assign)' -d 'Set PROP=VALUE (repeatable)'
complete -c zfs-set -o inherit -x -a '(__zfs_set_props)' -d 'Clear PROP so it is inherited or defaults (repeatable)'
complete -c zfs-set -o S -d 'With -inherit: revert to the received value'
complete -c zfs-set -o r -d 'With -set: children inherit it; with -inherit: zfs inherit -r; with -dump: descendants'
complete -c zfs-set -o u -d 'With -set: change mountpoint/sharenfs without mounting/sharing (zfs set -u)'
complete -c zfs-set -o tree -x -a '(__zfs_set_props)' -d 'The property on every ancestor and overriding descendant'
complete -c zfs-set -o where -x -a '(__zfs_set_props)' -d 'Every dataset that sets PROP itself'
complete -c zfs-set -o dump -d 'JSON snapshot of the properties set here'
complete -c zfs-set -o restore -r -d 'Restore a -dump snapshot'
complete -c zfs-set -o catalogue -d 'Print the property catalogue'
complete -c zfs-set -o describe -x -a '(__zfs_set_props)' -d 'Describe one property'
complete -c zfs-set -o n -d 'Dry run: print the zfs commands'
complete -c zfs-set -o y -d 'Apply without asking'
complete -c zfs-set -o check -d 'Dry run, exit 3 if anything would change'
complete -c zfs-set -o json -d 'JSON output'
complete -c zfs-set -o lenient -d 'Accept unknown property names'
complete -c zfs-set -f -a '(__zfs_set_datasets)' -d 'Dataset'
