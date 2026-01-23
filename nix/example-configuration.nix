# Example NixOS Configuration for Grimnir Radio
# This shows how to use the full turn-key installation

{ config, pkgs, ... }:

{
  # If using flakes, add this to your flake.nix inputs:
  # inputs.grimnir-radio.url = "github:friendsincode/grimnir_radio";
  #
  # Then import the module:
  # imports = [ inputs.grimnir-radio.nixosModules.default ];

  # Enable Grimnir Radio with full stack
  services.grimnir-radio = {
    enable = true;

    # HTTP configuration
    httpBind = "0.0.0.0";
    httpPort = 8080;

    # Auto-install PostgreSQL, Redis, and Icecast2
    enableDatabase = true;
    enableRedis = true;
    enableIcecast = true;

    # Database configuration (auto-created)
    databaseUrl = "postgres://grimnir:grimnir@localhost:5432/grimnir?sslmode=disable";

    # Redis configuration (auto-configured)
    redisUrl = "redis://localhost:6379/0";

    # Media engine gRPC address
    mediaEngineGrpcAddr = "localhost:9091";

    # SECURITY: Change these in production!
    jwtSecret = "CHANGE_THIS_TO_A_RANDOM_SECRET_IN_PRODUCTION";
    icecastPassword = "CHANGE_THIS_ICECAST_PASSWORD";

    # Media storage
    mediaStoragePath = "/var/lib/grimnir-radio/media";

    # Optional: Enable distributed tracing
    tracingEnabled = false;
    tracingSampleRate = 0.1;
    otlpEndpoint = "localhost:4317"; # Jaeger or Tempo

    # System user
    user = "grimnir";
    group = "grimnir";
  };

  # Optional: Nginx reverse proxy with TLS
  services.nginx = {
    enable = true;
    recommendedProxySettings = true;
    recommendedTlsSettings = true;
    recommendedOptimisation = true;

    virtualHosts."radio.example.com" = {
      # Enable ACME (Let's Encrypt) for automatic TLS
      enableACME = true;
      forceSSL = true;

      locations."/" = {
        proxyPass = "http://localhost:8080";
        proxyWebsockets = true; # Required for WebSocket events
        extraConfig = ''
          # CORS headers if needed
          add_header Access-Control-Allow-Origin * always;
          add_header Access-Control-Allow-Methods "GET, POST, PUT, DELETE, OPTIONS" always;
          add_header Access-Control-Allow-Headers "Authorization, Content-Type" always;
        '';
      };

      # Serve media files directly (if using local storage)
      locations."/media/" = {
        alias = "/var/lib/grimnir-radio/media/";
        extraConfig = ''
          autoindex off;
        '';
      };
    };

    # Icecast reverse proxy (optional)
    virtualHosts."stream.example.com" = {
      enableACME = true;
      forceSSL = true;

      locations."/" = {
        proxyPass = "http://localhost:8000";
      };
    };
  };

  # ACME (Let's Encrypt) configuration
  security.acme = {
    acceptTerms = true;
    defaults.email = "admin@example.com";
  };

  # Open firewall ports
  networking.firewall.allowedTCPPorts = [
    80    # HTTP (for ACME challenge)
    443   # HTTPS (Nginx)
    8000  # Icecast (if accessing directly)
    # 8080 # Grimnir API (only if not behind Nginx)
  ];

  # Optional: Automatic backup
  services.postgresqlBackup = {
    enable = true;
    databases = [ "grimnir" ];
    startAt = "daily";
    location = "/var/backup/postgresql";
  };

  # Optional: Prometheus monitoring
  services.prometheus = {
    enable = true;
    port = 9090;

    scrapeConfigs = [
      {
        job_name = "grimnir-radio";
        static_configs = [{
          targets = [ "localhost:9000" ]; # Grimnir metrics endpoint
        }];
      }
    ];
  };

  # Optional: Grafana dashboards
  services.grafana = {
    enable = true;
    settings.server.http_port = 3000;
  };
}
