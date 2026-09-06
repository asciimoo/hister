#!/bin/sh

# Manage a per-user macOS launchd agent for Hister.
# The script keeps Hister's data and logs in the user's home directory and
# never requires root privileges. It is safe to run from a source checkout or
# with an installed binary selected through HISTER_BIN.

set -eu
umask 077

agent_label="org.hister.server"
user_home="${HOME:?HOME must be set}"
data_dir="${HISTER_DATA_DIR:-}"
config_path="${HISTER_CONFIG:-}"
log_dir="$user_home/Library/Logs"
agent_dir="$user_home/Library/LaunchAgents"
plist_path="$agent_dir/$agent_label.plist"
stdout_path="$log_dir/hister.log"
stderr_path="$log_dir/hister-error.log"

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
repo_root=$(CDPATH= cd -- "$script_dir/../.." && pwd -P)
default_binary="$repo_root/hister"
binary_path="${HISTER_BIN:-$default_binary}"
temp_plist=""
backup_path=""
had_existing_agent=false
was_loaded=false
transaction_active=false

usage() {
    cat >&2 <<EOF
Usage: $0 {install|uninstall|start|stop|restart|status|logs}

Environment overrides:
  HISTER_BIN             Absolute or relative path to the hister binary.
  HISTER_DATA_DIR        Optional Hister data directory override.
  HISTER_CONFIG          Optional absolute or relative configuration file.
EOF
}

user_domain="gui/$(id -u)"
service_target="$user_domain/$agent_label"

cleanup_temp_plist() {
    if [ -n "$temp_plist" ] && [ -e "$temp_plist" ]; then
        rm -f "$temp_plist"
    fi
    temp_plist=""
}

cleanup_backup() {
    if [ -n "$backup_path" ] && [ -e "$backup_path" ]; then
        rm -f "$backup_path"
    fi
    backup_path=""
}

validate_inputs() {
    case "$user_home" in
        /*) ;;
        *)
            echo "HOME must be an absolute path: $user_home" >&2
            exit 1
            ;;
    esac
}

require_macos_tools() {
    if [ "$(uname -s)" != "Darwin" ]; then
        echo "This script only supports macOS." >&2
        exit 1
    fi
    command -v launchctl >/dev/null 2>&1 || {
        echo "launchctl was not found." >&2
        exit 1
    }
    command -v plutil >/dev/null 2>&1 || {
        echo "plutil was not found." >&2
        exit 1
    }
}

resolve_binary() {
    case "$binary_path" in
        /*) ;;
        *)
            if [ -f "$binary_path" ] && [ -x "$binary_path" ]; then
                binary_path=$(CDPATH= cd -- "$(dirname -- "$binary_path")" && pwd -P)/$(basename -- "$binary_path")
            else
                echo "Hister binary not found or not executable: $binary_path" >&2
                echo "Build it with ./manage.sh build or set HISTER_BIN." >&2
                exit 1
            fi
            ;;
    esac
    if [ ! -f "$binary_path" ] || [ ! -x "$binary_path" ]; then
        echo "Hister binary not found or not executable: $binary_path" >&2
        echo "Build it with ./manage.sh build or set HISTER_BIN." >&2
        exit 1
    fi
}

resolve_data_dir() {
    if [ -z "$data_dir" ]; then
        return
    fi
    if [ ! -d "$data_dir" ]; then
        mkdir -p "$data_dir"
        chmod 0700 "$data_dir"
    fi
    data_dir=$(CDPATH= cd -- "$data_dir" && pwd -P)
}

resolve_config() {
    if [ -z "$config_path" ]; then
        return
    fi
    case "$config_path" in
        /*) ;;
        *)
            if [ -f "$config_path" ]; then
                config_path=$(CDPATH= cd -- "$(dirname -- "$config_path")" && pwd -P)/$(basename -- "$config_path")
            else
                echo "Hister config file not found: $config_path" >&2
                exit 1
            fi
            ;;
    esac
    if [ ! -f "$config_path" ]; then
        echo "Hister config file not found: $config_path" >&2
        exit 1
    fi
}

is_loaded() {
    launchctl print "$service_target" >/dev/null 2>&1
}

wait_for_unloaded() {
    attempts=0
    while is_loaded; do
        attempts=$((attempts + 1))
        if [ "$attempts" -ge 50 ]; then
            echo "Timed out waiting for $service_target to unload." >&2
            return 1
        fi
        sleep 0.1
    done
}

unload_if_loaded() {
    if is_loaded; then
        launchctl bootout "$service_target"
        wait_for_unloaded
    fi
}

create_plist() {
    temp_plist=$(mktemp "$agent_dir/.hister-launchd.XXXXXX")
    plutil -create xml1 "$temp_plist"
    plutil -insert Label -string "$agent_label" "$temp_plist"
    plutil -insert ProgramArguments -array "$temp_plist"
    plutil -insert ProgramArguments.0 -string "$binary_path" "$temp_plist"
    plutil -insert ProgramArguments.1 -string listen "$temp_plist"
    plutil -insert EnvironmentVariables -dictionary "$temp_plist"
    plutil -insert EnvironmentVariables.HOME -string "$user_home" "$temp_plist"
    if [ -n "$data_dir" ]; then
        plutil -insert WorkingDirectory -string "$data_dir" "$temp_plist"
        plutil -insert EnvironmentVariables.HISTER_DATA_DIR -string "$data_dir" "$temp_plist"
    fi
    if [ -n "$config_path" ]; then
        plutil -insert EnvironmentVariables.HISTER_CONFIG -string "$config_path" "$temp_plist"
    fi
    plutil -insert RunAtLoad -bool true "$temp_plist"
    plutil -insert KeepAlive -xml '<dict><key>Crashed</key><true/><key>SuccessfulExit</key><false/></dict>' "$temp_plist"
    plutil -insert ProcessType -string Background "$temp_plist"
    plutil -insert Umask -string 077 "$temp_plist"
    plutil -insert StandardOutPath -string "$stdout_path" "$temp_plist"
    plutil -insert StandardErrorPath -string "$stderr_path" "$temp_plist"
    plutil -lint "$temp_plist"
}

backup_existing_plist() {
    if [ -e "$plist_path" ]; then
        had_existing_agent=true
        backup_path=$(mktemp "$plist_path.backup.XXXXXX")
        cp "$plist_path" "$backup_path"
        echo "Backed up existing agent to $backup_path"
    fi
}

rollback_install() {
    transaction_active=false
    launchctl bootout "$service_target" >/dev/null 2>&1 || :

    if [ "$had_existing_agent" = true ]; then
        if cp "$backup_path" "$plist_path" && chmod 0644 "$plist_path"; then
            if [ "$was_loaded" = true ]; then
                if launchctl bootstrap "$user_domain" "$plist_path"; then
                    echo "Restored the previous agent after installation failed." >&2
                else
                    echo "Failed to reload the previous agent from $plist_path" >&2
                fi
            else
                echo "Restored the previous agent plist after installation failed." >&2
            fi
            cleanup_backup
        else
            echo "Failed to restore the previous agent. Backup: $backup_path" >&2
        fi
    else
        rm -f "$plist_path"
    fi
}

handle_exit() {
    exit_status=$?
    trap - 0 HUP INT TERM
    if [ "$transaction_active" = true ]; then
        rollback_install
    fi
    cleanup_temp_plist
    exit "$exit_status"
}

install_agent() {
    resolve_binary
    resolve_config
    mkdir -p "$agent_dir" "$log_dir"
    touch "$stdout_path" "$stderr_path"
    chmod 0600 "$stdout_path" "$stderr_path"
    resolve_data_dir
    create_plist
    backup_existing_plist

    if is_loaded; then
        if [ "$had_existing_agent" = false ]; then
            echo "$service_target is loaded, but $plist_path does not exist; refusing to replace it without a rollback source." >&2
            cleanup_temp_plist
            return 1
        fi
        was_loaded=true
    fi

    transaction_active=true
    if [ "$was_loaded" = true ]; then
        if ! unload_if_loaded; then
            rollback_install
            cleanup_temp_plist
            return 1
        fi
    fi

    if ! mv "$temp_plist" "$plist_path"; then
        rollback_install
        cleanup_temp_plist
        return 1
    fi
    temp_plist=""
    if ! chmod 0644 "$plist_path" || ! launchctl bootstrap "$user_domain" "$plist_path"; then
        rollback_install
        cleanup_temp_plist
        return 1
    fi
    transaction_active=false
    cleanup_backup
    echo "Installed and started $service_target"
    echo "Binary: $binary_path"
    if [ -n "$data_dir" ]; then
        echo "Data:   $data_dir"
    else
        echo "Data:   selected by Hister configuration"
    fi
    echo "Plist:  $plist_path"
}

uninstall_agent() {
    unload_if_loaded
    if [ -e "$plist_path" ]; then
        rm "$plist_path"
        echo "Removed $plist_path"
    else
        echo "No plist found at $plist_path"
    fi
    echo "Hister data was not removed."
}

start_agent() {
    if ! [ -e "$plist_path" ]; then
        echo "No plist found. Run install first: $0 install" >&2
        exit 1
    fi
    if ! is_loaded; then
        launchctl bootstrap "$user_domain" "$plist_path"
    else
        launchctl kickstart "$service_target"
    fi
    echo "Started $service_target"
}

stop_agent() {
    unload_if_loaded
    echo "Stopped $service_target"
}

restart_agent() {
    if ! [ -e "$plist_path" ]; then
        echo "No plist found. Run install first: $0 install" >&2
        exit 1
    fi
    if is_loaded; then
        launchctl kickstart -k "$service_target"
        echo "Restarted $service_target"
    else
        start_agent
    fi
}

status_agent() {
    if is_loaded; then
        launchctl print "$service_target"
    else
        echo "$service_target is not loaded"
        exit 3
    fi
}

logs_agent() {
    echo "stdout: $stdout_path"
    echo "stderr: $stderr_path"
    if [ -f "$stdout_path" ]; then
        tail -n 40 "$stdout_path"
    fi
    if [ -f "$stderr_path" ]; then
        tail -n 40 "$stderr_path"
    fi
}

require_macos_tools
validate_inputs
trap handle_exit 0
trap 'exit 1' HUP INT TERM

command_name=${1:-}
case "$command_name" in
    install) install_agent ;;
    uninstall) uninstall_agent ;;
    start) start_agent ;;
    stop) stop_agent ;;
    restart) restart_agent ;;
    status) status_agent ;;
    logs) logs_agent ;;
    *) usage; exit 2 ;;
esac
