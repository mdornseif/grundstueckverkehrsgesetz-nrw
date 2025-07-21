#!/bin/bash

# Grundstückverkehrsgesetz Monitor Installationsskript
# Dieses Skript installiert und konfiguriert den Monitor als systemd-Service

set -e  # Beende bei Fehlern

# Konstanten
SERVICE_USER="gvgbot"
SERVICE_GROUP="gvgbot"

# Prüfe ob das Skript als root ausgeführt wird
check_root() {
    if [ "$EUID" -eq 0 ]; then
        echo "Dieses Skript sollte nicht als root ausgeführt werden!"
        echo "Führe es als normaler Benutzer aus (sudo wird automatisch verwendet)"
        exit 1
    fi
}

# Prüfe ob Go installiert ist
check_go() {
    if ! command -v go &> /dev/null; then
        echo "Go ist nicht installiert!"
        echo "Bitte installiere Go von https://golang.org/dl/"
        exit 1
    fi
    
    echo "Go Version: $(go version)"
}

# Prüfe ob Git installiert ist
check_git() {
    if ! command -v git &> /dev/null; then
        echo "Git ist nicht installiert!"
        echo "Bitte installiere Git: sudo apt-get install git (Ubuntu/Debian) oder sudo yum install git (CentOS/RHEL)"
        exit 1
    fi
    
    echo "Git Version: $(git --version)"
}

# Erstelle gvgbot Benutzer
create_service_user() {
    echo "Erstelle Service-Benutzer $SERVICE_USER..."
    
    # Prüfe ob Benutzer bereits existiert
    if id "$SERVICE_USER" &>/dev/null; then
        echo "Benutzer $SERVICE_USER existiert bereits"
    else
        # Erstelle Benutzer ohne Login-Shell
        sudo useradd --system --shell /bin/false --create-home --home-dir /home/$SERVICE_USER $SERVICE_USER
        echo "Benutzer $SERVICE_USER erstellt"
    fi
    
    # Prüfe ob Gruppe bereits existiert
    if getent group "$SERVICE_GROUP" &>/dev/null; then
        echo "Gruppe $SERVICE_GROUP existiert bereits"
    else
        # Erstelle Gruppe
        sudo groupadd --system $SERVICE_GROUP
        echo "Gruppe $SERVICE_GROUP erstellt"
    fi
    
    # Füge Benutzer zur Gruppe hinzu
    sudo usermod -a -G $SERVICE_GROUP $SERVICE_USER
    echo "Benutzer $SERVICE_USER zur Gruppe $SERVICE_GROUP hinzugefügt"
}

create_directories() {
    echo "Erstelle Verzeichnisse..."
    
    sudo mkdir -p /opt/grundstueckverkehrsgesetz
    sudo mkdir -p /usr/local/bin
    
    # Setze Besitzer auf gvgbot Benutzer
    sudo chown $SERVICE_USER:$SERVICE_GROUP /opt/grundstueckverkehrsgesetz
    sudo chmod 755 /opt/grundstueckverkehrsgesetz
    
    echo "Verzeichnisse erstellt"
}

compile_program() {
    echo "Kompiliere Programm..."
    
    if [ ! -f "main.go" ]; then
        echo "main.go nicht gefunden! Stelle sicher, dass du im richtigen Verzeichnis bist."
        exit 1
    fi
    
    go build -o monitor main.go
    
    if [ ! -f "monitor" ]; then
        echo "Kompilierung fehlgeschlagen!"
        exit 1
    fi
    
    echo "Programm erfolgreich kompiliert"
}

install_program() {
    echo "Installiere Programm..."

    TARGET=/usr/local/bin/grundstueckverkehrsgesetz-monitor
    BACKUP=/usr/local/bin/grundstueckverkehrsgesetz-monitor.old

    if [ -f "$TARGET" ]; then
        echo "Verschiebe laufende alte Version nach $BACKUP ..."
        sudo mv -f "$TARGET" "$BACKUP"
    fi

    sudo cp monitor "$TARGET"
    sudo chown $SERVICE_USER:$SERVICE_GROUP "$TARGET"
    sudo chmod +x "$TARGET"

    echo "Programm installiert"
}

copy_config_files() {
    echo "Kopiere Konfigurationsdateien..."

    # config.json
    if [ -f "/opt/grundstueckverkehrsgesetz/config.json" ]; then
        echo "/opt/grundstueckverkehrsgesetz/config.json existiert bereits - wird NICHT überschrieben."
    elif [ -f "config.json" ]; then
        sudo cp config.json /opt/grundstueckverkehrsgesetz/
        sudo chown $SERVICE_USER:$SERVICE_GROUP /opt/grundstueckverkehrsgesetz/config.json
        sudo chmod 644 /opt/grundstueckverkehrsgesetz/config.json
    else
        echo "config.json nicht gefunden - wird beim ersten Start erstellt"
    fi

    # links.json
    if [ -f "/opt/grundstueckverkehrsgesetz/links.json" ]; then
        echo "/opt/grundstueckverkehrsgesetz/links.json existiert bereits - wird NICHT überschrieben."
    elif [ -f "links.json" ]; then
        sudo cp links.json /opt/grundstueckverkehrsgesetz/
        sudo chown $SERVICE_USER:$SERVICE_GROUP /opt/grundstueckverkehrsgesetz/links.json
        sudo chmod 644 /opt/grundstueckverkehrsgesetz/links.json
    else
        echo "links.json nicht gefunden - wird beim ersten Start erstellt"
    fi

    echo "Konfigurationsdateien kopiert"
}

# Erstelle systemd-Service
create_service() {
    echo "Erstelle systemd-Service..."
    
    SERVICE_FILE="/tmp/grundstueckverkehrsgesetz-monitor.service"
    
    cat > $SERVICE_FILE << EOF
[Unit]
Description=Grundstückverkehrsgesetz Website Monitor
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
Group=$SERVICE_GROUP
WorkingDirectory=/opt/grundstueckverkehrsgesetz
ExecStart=/usr/local/bin/grundstueckverkehrsgesetz-monitor --loop
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

# Umgebungsvariablen
Environment=GOMAXPROCS=1

# Sicherheitseinstellungen
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/grundstueckverkehrsgesetz

[Install]
WantedBy=multi-user.target
EOF
    
    sudo cp $SERVICE_FILE /etc/systemd/system/
    rm $SERVICE_FILE
    
    echo "systemd-Service erstellt"
}

# Aktiviere und starte Service
enable_service() {
    echo "Aktiviere und starte Service..."
    
    sudo systemctl daemon-reload
    sudo systemctl enable grundstueckverkehrsgesetz-monitor
    sudo systemctl start grundstueckverkehrsgesetz-monitor
    
    # Warte kurz und prüfe Status
    sleep 2
    
    if sudo systemctl is-active --quiet grundstueckverkehrsgesetz-monitor; then
        echo "Service erfolgreich gestartet"
        echo "Service-Status: $(sudo systemctl is-active grundstueckverkehrsgesetz-monitor)"
    else
        echo "Service konnte nicht gestartet werden"
        echo "Prüfe die Logs mit: sudo journalctl -u grundstueckverkehrsgesetz-monitor -n 20"
    fi
}

# Hauptfunktion
main() {
    check_root
    check_go
    check_git
    create_service_user
    create_directories
    compile_program
    install_program
    copy_config_files
    create_service
    enable_service
    echo "Installation abgeschlossen!"
    echo "Service läuft als Benutzer: $SERVICE_USER"
    echo "Verwende folgende Befehle zum Verwalten des Services:"
    echo "  Status: sudo systemctl status grundstueckverkehrsgesetz-monitor"
    echo "  Stoppen: sudo systemctl stop grundstueckverkehrsgesetz-monitor"
    echo "  Starten: sudo systemctl start grundstueckverkehrsgesetz-monitor"
    echo "  Logs: sudo journalctl -u grundstueckverkehrsgesetz-monitor -f"
}

# Skript ausführen
main "$@" 