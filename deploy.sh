#!/bin/bash
set -e
echo "🔄 Начало обновления..."
echo "📥 Получение последних изменений из репозитория..."
git pull
echo "🔨 Сборка frontend части..."
npm run build
echo "⚙️ Компиляция backend части..."
go mod tidy
go build -o homeshare ./cmd/homeshare
echo "📦 Копирование исполняемого файла и фронтенд бандла в системную директорию..."
sudo systemctl stop lares.service
sudo cp homeshare /usr/local/bin/homeshare
sudo mkdir -p /var/lib/homeshare/dist
sudo rm -rf /var/lib/homeshare/dist/*
sudo cp -r dist/* /var/lib/homeshare/dist/
echo "🔄 Перезапуск сервиса lares..."
sudo systemctl restart lares.service
echo "✅ Статус сервиса lares:"
sudo systemctl status lares.service
echo "🎉 Обновление завершено!"
