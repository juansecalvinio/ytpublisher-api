#!/usr/bin/env bash
set -euo pipefail

PUBLIC_IP="$1"
KEY_PATH="${KEY_PATH:-$HOME/.ssh/ytpublisher-api.pem}"

echo "Building linux/amd64 binary..."
GOOS=linux GOARCH=amd64 go build -o bin/ytpublisher-api ./cmd/api

echo "Preparing remote directory..."
ssh -i "$KEY_PATH" "ec2-user@$PUBLIC_IP" \
  "sudo mkdir -p /opt/ytpublisher-api && sudo chown ec2-user:ec2-user /opt/ytpublisher-api"

echo "Copying binary and service file..."
scp -i "$KEY_PATH" bin/ytpublisher-api "ec2-user@$PUBLIC_IP:/opt/ytpublisher-api/ytpublisher-api"
scp -i "$KEY_PATH" deploy/ytpublisher-api.service "ec2-user@$PUBLIC_IP:/tmp/ytpublisher-api.service"

echo "Installing systemd service..."
ssh -i "$KEY_PATH" "ec2-user@$PUBLIC_IP" \
  "sudo mv /tmp/ytpublisher-api.service /etc/systemd/system/ytpublisher-api.service && \
   sudo systemctl daemon-reload && \
   sudo systemctl enable ytpublisher-api && \
   sudo systemctl restart ytpublisher-api"

echo "Waiting for the service to come up..."
sleep 2
curl -sf "http://$PUBLIC_IP:8080/healthz" && echo " -> healthz OK"
