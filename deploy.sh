#!/bin/bash
set -e
echo "🔄 Начало обновления..."
echo "📥 Получение последних изменений из репозитория..."
git pull
echo "🔨 Сборка frontend части..."
npm install --no-package-lock
npm run build
echo "⚙️ Компиляция backend части..."
go mod download
go build -o homeshare ./cmd/homeshare
echo "📦 Копирование исполняемого файла и фронтенд бандла в системную директорию..."
sudo systemctl stop lares.service
sudo install -o root -g root -m 0755 homeshare /usr/local/bin/homeshare
sudo install -d -o root -g homeshare -m 0750 /usr/local/share/lares/dist
sudo find /usr/local/share/lares/dist -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
sudo cp -r dist/. /usr/local/share/lares/dist/
sudo chown -R root:homeshare /usr/local/share/lares/dist
echo "🔄 Перезапуск сервиса lares..."
sudo systemctl restart lares.service
echo "✅ Статус сервиса lares:"
sudo systemctl status lares.service
echo "🎉 Обновление завершено!"
