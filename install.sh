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

install_base_deps() {
  log "Checking base dependencies (curl, git, build tools)..."
  case "$OS" in
    debian)
      apt-get update
      local pkgs="ca-certificates"
      check_command curl || pkgs="$pkgs curl"
      check_command git  || pkgs="$pkgs git"
      check_command gcc  || pkgs="$pkgs build-essential"
      if [[ -n "$pkgs" ]]; then
        apt-get install -y $pkgs
      else
        log "Base dependencies already present"
      fi
      ;;
    rhel)
      local pkgs="ca-certificates"
      check_command curl || pkgs="$pkgs curl"
      check_command git  || pkgs="$pkgs git"
      check_command gcc  || pkgs="$pkgs gcc gcc-c++ make"
      if [[ -n "$pkgs" ]]; then
        yum install -y $pkgs
      else
        log "Base dependencies already present"
      fi
      ;;
    macos)
      if ! check_command brew; then
        error "Homebrew not found. Please install it first: https://brew.sh"
        exit 1
      fi
      local pkgs=""
      check_command curl || pkgs="$pkgs curl"
      check_command git  || pkgs="$pkgs git"
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
    log "Go already installed: $(go version)"
    return
  fi

  log "Installing Go..."
  case "$OS" in
    debian|rhel|linux)
      local go_version="1.22.4"
      local go_tarball="go${go_version}.linux-amd64.tar.gz"
      cd /tmp
      curl -fsSL "https://go.dev/dl/${go_tarball}" -o "${go_tarball}"
      rm -rf /usr/local/go
      tar -C /usr/local -xzf "${go_tarball}"
      rm -f "${go_tarball}"

      if ! grep -q "/usr/local/go/bin" /etc/profile 2>/dev/null; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
      fi
      export PATH=$PATH:/usr/local/go/bin
      ;;
    macos)
      brew install go
      ;;
  esac

  if ! check_command go; then
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
ExecStart=$ROOT/start.sh
ExecStop=$ROOT/stop.sh
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  log "Service created. Use: sudo systemctl enable --now zapravka"
}

main() {
  log "Starting installation of Zapravka..."
  detect_os
  install_base_deps
  install_go
  install_node
  install_project_deps

  if [[ $EUID -eq 0 ]] && check_command systemctl; then
    create_systemd_service
  fi

  log "Installation complete!"
  log "Run: $ROOT/start.sh"
  log "Open: http://localhost:5173"
}

main "$@"
