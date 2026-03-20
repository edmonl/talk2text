#!/usr/bin/env bash
set -euo pipefail

# Configuration
action="${1:-install}"
model_name="${MODEL_NAME:-small.en}"
port="${PORT:-9898}"
bin_dir="${HOME}/bin"
model_dir="${HOME}/stow/whisper-models"
repo_dir="${HOME}/projects/whisper.cpp"
service_dir="${HOME}/.config/systemd/user"
service_name='whisper-server.service'

add_package_if_missing() {
	local command_name="$1"
	local package_name="$2"

	if ! command -v "$command_name" >/dev/null 2>&1; then
		missing_packages+=("$package_name")
	fi
}

need_cmd() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf 'missing required command after install: %s\n' "$1" >&2
		exit 1
	fi
}

build_dir="${repo_dir}/build"
repo_model_dir="${repo_dir}/models"
repo_server_bin="${build_dir}/bin/whisper-server"
repo_model_path="${repo_model_dir}/ggml-${model_name}.bin"
installed_server_bin="${bin_dir}/whisper-server"
model_path="${model_dir}/ggml-${model_name}.bin"
service_path="${service_dir}/${service_name}"
server_bin=''
server_source=''
service_exists='false'
missing_packages=()

if [ "$action" = 'uninstall' ]; then
	if command -v systemctl >/dev/null 2>&1; then
		systemctl --user disable --now "$service_name" >/dev/null 2>&1 || true
		systemctl --user daemon-reload >/dev/null 2>&1 || true
	fi

	rm -f "$service_path"
	rm -f "$installed_server_bin"

	if command -v systemctl >/dev/null 2>&1; then
		systemctl --user daemon-reload >/dev/null 2>&1 || true
	fi

	cat <<EOF
uninstall complete.

Removed service: ${service_path}
Removed binary: ${installed_server_bin}
Model left in place: ${model_dir}
Repo left in place: ${repo_dir}
EOF
	exit 0
fi

if [ "$action" != 'install' ]; then
	printf 'unknown action: %s\n' "$action" >&2
	printf 'usage: %s [install|uninstall]\n' "$0" >&2
	exit 1
fi

add_package_if_missing git git
add_package_if_missing cmake cmake
add_package_if_missing curl curl
add_package_if_missing ffmpeg ffmpeg
add_package_if_missing wl-copy wl-clipboard
add_package_if_missing notify-send libnotify-bin
add_package_if_missing mako mako-notifier

if ! command -v make >/dev/null 2>&1 || ! command -v cc >/dev/null 2>&1 || ! command -v c++ >/dev/null 2>&1; then
	missing_packages+=(build-essential)
fi

if ! command -v git >/dev/null 2>&1 || ! command -v curl >/dev/null 2>&1; then
	missing_packages+=(ca-certificates)
fi

if [ "${#missing_packages[@]}" -gt 0 ]; then
	sudo apt-get update
	sudo apt-get install -y --no-install-recommends "${missing_packages[@]}"
fi

need_cmd git
need_cmd cmake
need_cmd curl
need_cmd make
need_cmd cc
need_cmd c++
need_cmd ffmpeg
need_cmd wl-copy
need_cmd notify-send
need_cmd mako
need_cmd systemctl

if [ -f "$service_path" ]; then
	service_exists='true'
elif systemctl --user cat "$service_name" >/dev/null 2>&1; then
	service_exists='true'
fi

if [ -x "$installed_server_bin" ]; then
	server_bin="$installed_server_bin"
	server_source='managed'
elif command -v whisper-server >/dev/null 2>&1; then
	server_bin="$(command -v whisper-server)"
	server_source='external'
fi

if [ "$server_source" = '' ]; then
	if [ -e "$repo_dir" ] && [ ! -d "${repo_dir}/.git" ]; then
		printf 'refusing to use existing non-git path: %s\n' "$repo_dir" >&2
		exit 1
	fi

	mkdir -p "${HOME}/projects"

	if [ ! -d "${repo_dir}/.git" ]; then
		git clone https://github.com/ggml-org/whisper.cpp.git "$repo_dir"
	else
		git -C "$repo_dir" pull --ff-only
	fi

	cmake -S "$repo_dir" -B "$build_dir" -DCMAKE_BUILD_TYPE=Release
	cmake --build "$build_dir" -j"$(nproc)"

	if [ ! -x "$repo_server_bin" ]; then
		printf 'build finished, but whisper-server was not produced at %s\n' "$repo_server_bin" >&2
		exit 1
	fi

	mkdir -p "$bin_dir"
	if [ ! -d "$bin_dir" ]; then
		printf 'failed to create binary directory: %s\n' "$bin_dir" >&2
		exit 1
	fi

	install -m 0755 "$repo_server_bin" "$installed_server_bin"
	server_bin="$installed_server_bin"
	server_source='built'
fi

if [ "$service_exists" = 'true' ]; then
	cat <<EOF
install complete.

whisper-server: ${server_bin}
Service already exists: ${service_path}
No service changes were made.
EOF
	exit 0
fi

if [ "$server_source" = 'external' ]; then
	cat <<EOF
install complete.

Found existing whisper-server: ${server_bin}
No user service was installed because this script does not know that binary's model path or desired arguments.
Create ${service_path} manually if you want to manage that installation with systemd --user.
EOF
	exit 0
fi

if [ ! -f "$model_path" ]; then
	if [ ! -d "$repo_model_dir" ]; then
		printf 'missing model source directory: %s\n' "$repo_model_dir" >&2
		exit 1
	fi

	if [ ! -f "$repo_model_path" ]; then
		"${repo_model_dir}/download-ggml-model.sh" "$model_name"
	fi

	if [ ! -f "$repo_model_path" ]; then
		printf 'model download did not produce expected file: %s\n' "$repo_model_path" >&2
		exit 1
	fi

	mkdir -p "$model_dir"
	if [ ! -d "$model_dir" ]; then
		printf 'failed to create model directory: %s\n' "$model_dir" >&2
		exit 1
	fi

	install -m 0644 "$repo_model_path" "$model_path"
fi

mkdir -p "$service_dir"
if [ ! -d "$service_dir" ]; then
	printf 'failed to create service directory: %s\n' "$service_dir" >&2
	exit 1
fi

cat >"$service_path" <<EOF
[Unit]
Description=Whisper HTTP server
After=default.target

[Service]
ExecStart=${server_bin} --host 127.0.0.1 --port ${port} -m ${model_path}
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
EOF

systemctl --user daemon-reload
systemctl --user enable --now "$service_name"

cat <<EOF
install complete.

whisper-server: ${server_bin}
Model: ${model_path}
Port: ${port}
Service: ${service_path}

Example checks:
  systemctl --user status ${service_name}
  journalctl --user -u ${service_name} -n 50 --no-pager
EOF
