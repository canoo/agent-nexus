#!/usr/bin/env bash
# Run this ON THE LLM PC (The machine with the GPU)
set -e

echo "Configuring Ollama as a Headless Network AI Server..."

# 1. Install Ollama (if not already installed)
if ! command -v ollama &> /dev/null; then
    curl -fsSL https://ollama.com/install.sh | sh
fi

# 2. Create a systemd override to expose Ollama to the local network
sudo mkdir -p /etc/systemd/system/ollama.service.d
echo '[Service]' | sudo tee /etc/systemd/system/ollama.service.d/environment.conf > /dev/null
echo 'Environment="OLLAMA_HOST=0.0.0.0"' | sudo tee -a /etc/systemd/system/ollama.service.d/environment.conf > /dev/null

# 3. Reload and restart the service
sudo systemctl daemon-reload
sudo systemctl enable ollama
sudo systemctl restart ollama

# 4. Configure Firewall (if UFW is active)
if command -v ufw &> /dev/null && sudo ufw status | grep -q "Status: active"; then
    echo "Opening port 11434 on UFW firewall..."
    sudo ufw allow 11434/tcp
fi

# 5. Pull default NEXUS routing models required by ollama-delegate.sh
echo "Pre-pulling models required by NEXUS (this may take a while)..."
ollama pull qwen2.5-coder:1.5b
ollama pull llama3.2:3b

echo "Ollama Server is now running and listening on the network!"
echo "Find this machine's IP address by running: ip addr"
echo "Set this on your NEXUS machine: export OLLAMA_HOST_URL=\"http://\$(hostname -I | awk '{print \$1}'):11434\""