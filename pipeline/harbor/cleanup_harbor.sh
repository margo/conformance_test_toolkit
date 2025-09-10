#!/bin/bash
set -e

echo "Stopping Harbor containers..."
# Stop only Harbor-related containers
docker ps --filter "name=harbor" --format "{{.ID}}" | xargs -r docker stop
docker ps --filter "ancestor=goharbor/*" --format "{{.ID}}" | xargs -r docker stop

echo "Removing Harbor containers..."
# Remove only Harbor-related containers
docker ps -a --filter "name=harbor" --format "{{.ID}}" | xargs -r docker rm -f
docker ps -a --filter "ancestor=goharbor/*" --format "{{.ID}}" | xargs -r docker rm -f

echo "Removing Harbor Docker images..."
# Force remove Harbor-related images
docker images --filter=reference='goharbor/*' --format "{{.ID}}" | xargs -r docker rmi -f
docker images --filter=reference='*harbor*' --format "{{.ID}}" | xargs -r docker rmi -f

echo "Removing Harbor Docker volumes..."
# Remove only Harbor-related volumes
docker volume ls --filter "name=harbor" --format "{{.Name}}" | xargs -r docker volume rm -f

echo "Removing Harbor Docker networks..."
# Remove only Harbor-related networks
docker network ls --filter "name=harbor" --format "{{.ID}}" | xargs -r docker network rm

echo "Removing Harbor installer files..."
rm -rf harbor harbor-offline-installer-*

echo "Harbor cleanup complete!"

