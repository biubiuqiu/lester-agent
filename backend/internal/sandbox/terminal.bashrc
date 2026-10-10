# Respect the user's interactive configuration, then configure this terminal.
_lester_start_dir=$PWD
[[ -r /etc/bash.bashrc ]] && source /etc/bash.bashrc
[[ -r ~/.bashrc ]] && source ~/.bashrc
cd -- "$_lester_start_dir" || return

export TERM=xterm-256color COLORTERM=truecolor
if ! declare -F _completion_loader >/dev/null; then
  if [[ -r /usr/share/bash-completion/bash_completion ]]; then
    source /usr/share/bash-completion/bash_completion
  elif [[ -r /etc/bash_completion ]]; then
    source /etc/bash_completion
  fi
fi

# History belongs to the conversation's persistent workspace, not the browser.
# A leading space excludes a sensitive command from history.
if (umask 077; mkdir -p -- "$PWD/.agent/terminal"); then
  HISTFILE="$PWD/.agent/terminal/bash_history"
else
  unset HISTFILE
fi
HISTSIZE=2000
HISTFILESIZE=10000
HISTCONTROL=ignoreboth
shopt -s histappend checkwinsize
history -c
[[ -r ${HISTFILE:-} ]] && history -r
_lester_sync_history() { history -a; history -n; }
if [[ $(declare -p PROMPT_COMMAND 2>/dev/null) == 'declare -a '* ]]; then
  PROMPT_COMMAND=(_lester_sync_history "${PROMPT_COMMAND[@]}")
else
  PROMPT_COMMAND="_lester_sync_history${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
fi
bind 'set completion-ignore-case on'
bind 'set show-all-if-ambiguous on'
bind 'set enable-bracketed-paste on'
bind '"\e[A": history-search-backward'
bind '"\e[B": history-search-forward'
bind '"\e[1;5D": backward-word'
bind '"\e[1;5C": forward-word'
bind '"\e[Z": menu-complete-backward'
# Conversation IDs would otherwise consume most of a narrow terminal row.
_lester_prompt_dir() {
  local directory=${PWD##*/}
  if [[ $PWD == /workspace/conversations/* && $directory =~ ^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{4}-[[:xdigit:]]{12}$ ]]; then
    printf '%s…' "${directory:0:8}"
  else
    printf '%s' "$directory"
  fi
}
PS1='\[\e[32m\]lester\[\e[0m\] \[\e[34m\]$(_lester_prompt_dir)\[\e[0m\] \$ '
unset _lester_start_dir
