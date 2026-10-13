package sandbox

// The supervisor stays the session/group leader until cancellation cleanup
// finishes. Recording Linux start time prevents a recycled PID from selecting
// an unrelated process. Markers are private, random, and outside user files.
const dockerCommandStart = `pid_file=$1
shell=$2
command=$3
start_time=$(
  IFS= read -r stat < /proc/$$/stat || exit 1
  set -f
  set -- ${stat##*) }
  [ "$3" = "$$" ] && [ "$4" = "$$" ] || exit 1
  shift 19
  printf '%s' "$1"
) || exit 1
(umask 077; set -C; printf '%s %s\n' "$$" "$start_time" > "$pid_file") || exit 1
stopping=0
trap 'stopping=1' TERM HUP INT
"$shell" -lc "$command" &
child=$!
wait "$child"
exit_code=$?
if [ "$stopping" = 1 ]; then
  # Keep the identity anchor alive while the caller escalates to group KILL.
  while :; do sleep 1; done
fi
(umask 077; set -C; printf '%s\n' "$exit_code" > "$pid_file.exit") || exit 1
exit "$exit_code"`

const dockerCommandStop = `pid_file=$1
attempts=0
until IFS=' ' read -r pid start_time 2>/dev/null < "$pid_file"; do
  attempts=$((attempts + 1))
  [ "$attempts" -lt 30 ] || exit 1
  sleep 0.05
done
case "$pid:$start_time" in *[!0-9:]*|:*|*:) exit 1;; esac
[ "$pid" -gt 1 ] || exit 1
same_process() {
  IFS= read -r stat 2>/dev/null < "/proc/$pid/stat" || return 1
  set -f
  set -- ${stat##*) }
  [ "$3" = "$pid" ] && [ "$4" = "$pid" ] || return 1
  shift 19
  [ "$1" = "$start_time" ]
}
records=
collect_descendants() {
  seen=$pid
  rounds=0
  while :; do
    added=0
    set +f
    for proc_stat in /proc/[0-9]*/stat; do
      child_pid=${proc_stat#/proc/}; child_pid=${child_pid%/stat}
      case " $seen " in *" $child_pid "*) continue;; esac
      IFS= read -r stat 2>/dev/null < "$proc_stat" || continue
      set -f
      set -- ${stat##*) }
      case " $seen " in *" $2 "*) :;; *) continue;; esac
      shift 19
      records="$records $child_pid:$1"
      seen="$seen $child_pid"
      added=1
    done
    [ "$added" = 1 ] || break
    rounds=$((rounds + 1))
    [ "$rounds" -lt 256 ] || return 1
  done
}
same_child() {
  child_pid=${1%:*}; child_start=${1#*:}
  IFS= read -r stat 2>/dev/null < "/proc/$child_pid/stat" || return 1
  set -f
  set -- ${stat##*) }
  [ "$1" != Z ] || return 1
  shift 19
  [ "$1" = "$child_start" ]
}
if same_process; then
  # Detached children may create another session (for example browsers).
  # Observe ancestry before parents exit, and guard every later signal with
  # that descendant's recorded start time as well as its PID.
  collect_descendants || exit 1
  kill -TERM "-$pid" 2>/dev/null || :
  for record in $records; do
    if same_child "$record"; then kill -TERM "$child_pid" 2>/dev/null || :; fi
  done
  sleep 0.15
  for record in $records; do
    if same_child "$record"; then kill -KILL "$child_pid" 2>/dev/null || :; fi
  done
  if same_process; then kill -KILL "-$pid" 2>/dev/null || :; fi
fi
group_alive() {
  set +f
  for proc_stat in /proc/[0-9]*/stat; do
    IFS= read -r stat 2>/dev/null < "$proc_stat" || continue
    set -f
    set -- ${stat##*) }
    if [ "$1" != Z ] && [ "$3" = "$pid" ] && [ "$4" = "$pid" ]; then return 0; fi
  done
  return 1
}
tracked_alive() {
  for record in $records; do if same_child "$record"; then return 0; fi; done
  group_alive
}
attempts=0
while tracked_alive; do
  attempts=$((attempts + 1))
  [ "$attempts" -lt 20 ] || exit 1
  sleep 0.05
done`

const dockerCommandReadExit = `pid_file=$1
if IFS= read -r exit_code 2>/dev/null < "$pid_file.exit"; then
  case "$exit_code" in ''|*[!0-9]*) :;; *) printf '%s' "$exit_code";; esac
fi
rm -f -- "$pid_file" "$pid_file.exit"`
