#!/bin/bash
set -e

# Цвета
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

ROOT="$(cd "$(dirname "$0")" && pwd)"
OS=""
ARCH="$(uname -m)"

# Убедимся, что Go из /usr/local/go доступен внутри этого скрипта (например, при повторном запуске)
if [ -x /usr/local/go/bin/go ]; then
  export PATH="/usr/local/go/bin:$PATH"
fi

# Отключаем автообновление Homebrew, чтобы не трогать существующие пакеты
export HOMEBREW_NO_AUTO_UPDATE=1

log() {
  echo -e "${GREEN}[install]${NC} $1"
}

warn() {
  echo -e "${YELLOW}[warn]${NC} $1"
}

error() {
  echo -e "${RED}[error]${NC} $1"
}

detect_os() {
  if [[ "$OSTYPE" == "linux-gnu"* ]]; then
    if command -v apt-get >/dev/null 2>&1; then
      OS="debian"
    elif command -v yum >/dev/null 2>&1; then
      OS="rhel"
    else
      OS="linux"
    fi
  elif [[ "$OSTYPE" == "darwin"* ]]; then
    OS="macos"
  else
    error "Unsupported OS: $OSTYPE"
    exit 1
  fi
  log "Detected OS: $OS ($ARCH)"
}

check_command() {
  command -v "$1" >/dev/null 2>&1
}

go_arch() {
  case "$ARCH" in
    x86_64)  echo "amd64" ;;
    amd64)   echo "amd64" ;;
    aarch64) echo "arm64" ;;
    arm64)   echo "arm64" ;;
    armv7l)  echo "armv6l" ;;
    *)       echo "$ARCH" ;;
  esac
}

install_base_deps() {
  log "Checking base dependencies (curl, git, nginx, build tools)..."
  case "$OS" in
    debian)
      apt-get update
      local pkgs="ca-certificates nginx"
      check_command curl || pkgs="$pkgs curl"
      check_command git  || pkgs="$pkgs git"
      check_command gcc  || pkgs="$pkgs build-essential"
      apt-get install -y $pkgs
      ;;
    rhel)
      local pkgs="ca-certificates nginx"
      check_command curl || pkgs="$pkgs curl"
      check_command git  || pkgs="$pkgs git"
      check_command gcc  || pkgs="$pkgs gcc gcc-c++ make"
      yum install -y $pkgs
      ;;
    macos)
      if ! check_command brew; then
        error "Homebrew not found. Please install it first: https://brew.sh"
        exit 1
      fi
      local pkgs=""
      check_command curl || pkgs="$pkgs curl"
      check_command git  || pkgs="$pkgs git"
      check_command nginx || pkgs="$pkgs nginx"
      if [[ -n "$pkgs" ]]; then
        # shellcheck disable=SC2086
        brew install $pkgs
      else
        log "Base dependencies already present"
      fi
      ;;
  esac
}

install_go() {
  if check_command go; then
    if go version >/dev/null 2>&1; then
      log "Go already installed: $(go version)"
      return
    fi
    warn "Found broken Go installation, reinstalling..."
    rm -rf /usr/local/go
  fi

  log "Installing Go..."
  case "$OS" in
    debian|rhel|linux)
      local go_version="1.22.4"
      local go_arch="$(go_arch)"
      local go_tarball="go${go_version}.linux-${go_arch}.tar.gz"
      log "Downloading ${go_tarball}..."
      cd /tmp
      curl -fsSL "https://go.dev/dl/${go_tarball}" -o "${go_tarball}"
      rm -rf /usr/local/go
      tar -C /usr/local -xzf "${go_tarball}"
      rm -f "${go_tarball}"

      export PATH="/usr/local/go/bin:$PATH"
      if ! grep -q "/usr/local/go/bin" /etc/profile 2>/dev/null; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
      fi
      # Для systemd и новых сессий: дописываем /usr/local/go/bin в PATH
      if [ -f /etc/environment ] && grep -q '^PATH=' /etc/environment; then
        if ! grep -q "/usr/local/go/bin" /etc/environment; then
          sed -i 's|PATH="\([^"]*\)"|PATH="\1:/usr/local/go/bin"|' /etc/environment
        fi
      else
        echo 'PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/go/bin"' >> /etc/environment
      fi
      ;;
    macos)
      brew install go
      ;;
  esac

  if ! check_command go || ! go version >/dev/null 2>&1; then
    error "Go installation failed"
    exit 1
  fi
  log "Go installed: $(go version)"
}

install_node() {
  if check_command node && check_command npm; then
    log "Node.js already installed: $(node --version), npm: $(npm --version)"
    return
  fi

  log "Installing Node.js 20 LTS..."
  case "$OS" in
    debian)
      curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
      apt-get install -y nodejs
      ;;
    rhel)
      curl -fsSL https://rpm.nodesource.com/setup_20.x | bash -
      yum install -y nodejs
      ;;
    macos)
      brew install node
      ;;
  esac

  if ! check_command node || ! check_command npm; then
    error "Node.js/npm installation failed"
    exit 1
  fi
  log "Node.js installed: $(node --version), npm: $(npm --version)"
}

install_project_deps() {
  if [ ! -f "$ROOT/frontend/.env" ]; then
    log "Creating frontend/.env from example..."
    cp "$ROOT/frontend/.env.example" "$ROOT/frontend/.env"
  fi

  log "Installing frontend dependencies..."
  cd "$ROOT/frontend"
  npm install

  log "Building backend binary..."
  cd "$ROOT/backend"
  go build -o zapravka .

  log "Creating start/stop scripts..."
  chmod +x "$ROOT/start.sh" "$ROOT/stop.sh"
}

create_systemd_service() {
  if [[ "$OS" != "debian" && "$OS" != "rhel" ]]; then
    return
  fi

  if [[ ! -d /etc/systemd/system ]]; then
    warn "systemd not detected, skipping service creation"
    return
  fi

  if [[ -z "$SUDO_USER" && $EUID -ne 0 ]]; then
    warn "Run with sudo to create systemd service"
    return
  fi

  log "Creating systemd service zapravka.service..."
  cat > /etc/systemd/system/zapravka.service <<EOF
[Unit]
Description=Zapravka Map Service
After=network.target

[Service]
Type=forking
WorkingDirectory=$ROOT
Environment="PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
ExecStart=$ROOT/start.sh
ExecStop=$ROOT/stop.sh
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  log "Service created. Use: sudo systemctl enable --now zapravka"
}

setup_https() {
  if [[ "$OS" == "macos" ]]; then
    warn "HTTPS auto-setup via Nginx is supported on Linux only. On macOS use 'brew install mkcert' manually."
    return
  fi

  if [[ $EUID -ne 0 ]]; then
    warn "Run with sudo to set up HTTPS via Nginx"
    return
  fi

  log "Setting up Nginx with HTTPS (self-signed certificate)..."

  local cert_dir="/etc/nginx/ssl"
  local cert="$cert_dir/zapravka.crt"
  local key="$cert_dir/zapravka.key"

  mkdir -p "$cert_dir"

  if [[ ! -f "$cert" || ! -f "$key" ]]; then
    log "Generating self-signed certificate..."
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
      -keyout "$key" -out "$cert" \
      -subj "/CN=localhost" \
      -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
    chmod 600 "$key"
    chmod 644 "$cert"
  else
    log "Certificate already exists"
  fi

  log "Creating Nginx config..."
  cat > /etc/nginx/sites-available/zapravka <<'EOF'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl default_server;
    listen [::]:443 ssl default_server;
    server_name _;

    ssl_certificate /etc/nginx/ssl/zapravka.crt;
    ssl_certificate_key /etc/nginx/ssl/zapravka.key;

    location / {
        proxy_pass http://127.0.0.1:5173;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_cache_bypass $http_upgrade;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
EOF

  if [[ -d /etc/nginx/sites-enabled ]]; then
    rm -f /etc/nginx/sites-enabled/default
    ln -sf /etc/nginx/sites-available/zapravka /etc/nginx/sites-enabled/zapravka
  else
    # RHEL/CentOS style
    cp /etc/nginx/sites-available/zapravka /etc/nginx/conf.d/zapravka.conf
  fi

  if nginx -t; then
    systemctl enable nginx
    systemctl restart nginx
    log "Nginx HTTPS is running on https://<your-server-ip>/"
    warn "Self-signed certificate will show a browser warning. Accept it or replace with a real certificate (e.g. Let's Encrypt)."
  else
    error "Nginx config test failed"
  fi
}

main() {
  log "Starting installation of Zapravka..."
  detect_os

  if [[ "$OS" != "macos" && $EUID -ne 0 ]]; then
    error "On Linux install.sh must be run as root or with sudo"
    exit 1
  fi

  install_base_deps
  install_go
  install_node
  install_project_deps

  log "Verifying installation..."
  if ! go version >/dev/null 2>&1; then
    error "Go is not available after installation"
    exit 1
  fi
  if ! node --version >/dev/null 2>&1 || ! npm --version >/dev/null 2>&1; then
    error "Node.js/npm is not available after installation"
    exit 1
  fi

  if [[ $EUID -eq 0 ]] && check_command systemctl; then
    create_systemd_service
    setup_https
  fi

  log "Installation complete!"
  log "Run: $ROOT/start.sh"
  log "Or:  sudo systemctl enable --now zapravka"
  log "Open: https://<your-server-ip>/"
}

main "$@"
