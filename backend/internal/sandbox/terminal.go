package sandbox

import (
	_ "embed"
)

//go:embed terminal.bashrc
var terminalBashRC string

// Record the Linux process start time as well as its PID, so a later close
// guards against signalling another process after the PID has been reused.
// This ephemeral private marker is outside the conversation's file inventory.
const dockerTerminalStart = `pid_file=$1
shift
start_time=$(
  IFS= read -r stat < /proc/$$/stat || exit 1
  set -f
  set -- ${stat##*) }
  shift 19
  printf '%s' "$1"
) || exit 1
(umask 077; set -C; printf '%s %s\n' "$$" "$start_time" > "$pid_file") || exit 1
exec sh "$@"`

const dockerTerminalHangup = `pid_file=$1
attempts=0
until IFS=' ' read -r pid start_time 2>/dev/null < "$pid_file"; do
  attempts=$((attempts + 1))
  [ "$attempts" -lt 30 ] || exit 0
  sleep 0.05
done
case "$pid:$start_time" in *[!0-9:]*|:*|*:) exit 0;; esac
same_process() {
  IFS= read -r stat 2>/dev/null < "/proc/$pid/stat" || return 1
  set -f
  set -- ${stat##*) }
  shift 19
  [ "$1" = "$start_time" ]
}
if same_process; then
  kill -HUP "$pid" 2>/dev/null || :
  # Readline can defer HUP while waiting for an incomplete escape sequence.
  # Allow graceful shutdown, then terminate only that same shell if necessary.
  sleep 0.1
  if same_process; then kill -KILL "$pid" 2>/dev/null || :; fi
fi
rm -f -- "$pid_file"`

// Bash reads its rc file from an inherited descriptor; no generated startup
// file is written into the user's home or conversation. Minimal custom images
// without Bash remain usable, with an explicit reduced-capability warning.
func terminalShellArgs() []string {
	return []string{"-c", `if command -v bash >/dev/null 2>&1; then
  exec bash -c 'exec bash --noprofile --rcfile <(printf "%s\n" "$1") -i' lester-terminal "$1"
fi
printf '\r\n当前镜像未安装 Bash，使用基础 sh；补全和历史取决于该 Shell。\r\n'
exec sh -i`, "lester-terminal", terminalBashRC}
}

func terminalSize(cols, rows int) (int, int) {
	return min(max(cols, 1), 1000), min(max(rows, 1), 1000)
}
