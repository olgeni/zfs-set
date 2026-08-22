# bash completion for zfs-set(1). Source it from ~/.bashrc or install it as
# /usr/local/share/bash-completion/completions/zfs-set.

_zfs_set() {
  local cur prev
  COMPREPLY=()
  if declare -F _get_comp_words_by_ref >/dev/null 2>&1; then
    _get_comp_words_by_ref -n := cur prev
  else
    cur=${COMP_WORDS[COMP_CWORD]}
    prev=${COMP_WORDS[COMP_CWORD-1]}
  fi
  local props
  case $prev in
    -set)
      if [[ $cur == *=* ]]; then
        local p=${cur%%=*} v=${cur#*=}
        COMPREPLY=($(compgen -P "$p=" -W "$(zfs-set -describe "$p" 2>/dev/null | awk '/^    [a-z0-9]/ { print $1 }')" -- "$v"))
      else
        props=$(zfs-set -catalogue 2>/dev/null | awk '/settable/ { print $1 }' | grep -v '[@#]$')
        COMPREPLY=($(compgen -S = -W "$props" -- "$cur"))
        compopt -o nospace 2>/dev/null
      fi
      declare -F __ltrim_colon_completions >/dev/null 2>&1 && __ltrim_colon_completions "$cur"
      return ;;
    -get|-inherit|-tree|-where|-describe)
      props=$(zfs-set -catalogue 2>/dev/null | awk '/^  [a-z]/ { print $1 }' | grep -v '[@#]$')
      local last=${cur##*,} head=
      [[ $cur == *,* ]] && head=${cur%,*},
      COMPREPLY=($(compgen -P "$head" -W "$props" -- "$last"))
      return ;;
    -restore)
      COMPREPLY=($(compgen -f -- "$cur"))
      return ;;
  esac
  if [[ $cur == -* ]]; then
    COMPREPLY=($(compgen -W '-version -list -local -get -set -inherit -S -r -u -tree -where -dump -restore -catalogue -describe -n -y -check -json -lenient' -- "$cur"))
    return
  fi
  COMPREPLY=($(compgen -W "$(zfs list -H -o name -t filesystem,volume 2>/dev/null)" -- "$cur"))
}
complete -F _zfs_set zfs-set
