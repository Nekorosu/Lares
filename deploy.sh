#!/usr/bin/env bash
# Run from an explicitly selected, reviewed checkout. Does not pull/merge Git refs.
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
if (( EUID == 0 )); then
  echo 'Запускайте сборку обычным пользователем с доступом sudo.' >&2
  exit 1
fi
command -v go >/dev/null
build_dir=$(mktemp -d)
trap 'rm -rf -- "$build_dir"' EXIT
printf '%s\n' 'Проверка и сборка выбранной версии…'
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o "$build_dir/homeshare" ./cmd/homeshare
sudo -v
# The service stays stopped on failure so an old binary cannot open a migrated DB.
sudo install -m 0755 "$build_dir/homeshare" /usr/local/bin/homeshare.next
sudo systemctl stop lares.service
sudo -u homeshare env LARES_CONFIG=/etc/homeshare/config.yaml /usr/local/bin/homeshare.next backup
sudo mv /usr/local/bin/homeshare.next /usr/local/bin/homeshare
sudo install -m 0644 lares.service /etc/systemd/system/lares.service
sudo systemctl daemon-reload
sudo systemctl start lares.service
sudo systemctl is-active --quiet lares.service
curl --fail --silent --show-error http://127.0.0.1:8090/robots.txt
printf '\n%s\n' 'Обновление установлено. Резервные копии upgrade-* сохранены для ручного отката.'
