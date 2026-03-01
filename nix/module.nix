{ config, lib, pkgs, ... }:

with lib;

let
  cfg = config.services.grimnir-radio;

  # Default configuration file
  configFile = pkgs.writeText "grimnir-config.yaml" ''
    environment: ${cfg.environment}
    http_bind: ${cfg.httpBind}
    http_port: ${toString cfg.httpPort}
    database_url: ${cfg.databaseUrl}
    redis_url: ${cfg.redisUrl}
    media_engine_grpc_addr: ${cfg.mediaEngineGrpcAddr}
    jwt_secret: ${cfg.jwtSecret}
    media_storage_path: ${cfg.mediaStoragePath}
    tracing_enabled: ${toString cfg.tracingEnabled}
    tracing_sample_rate: ${toString cfg.tracingSampleRate}
    otlp_endpoint: ${cfg.otlpEndpoint}
  '';

in
{
  options.services.grimnir-radio = {
    enable = mkEnableOption "Grimnir Radio broadcast automation system";

    package = mkOption {
      type = types.package;
      default = pkgs.grimnir-radio;
      defaultText = literalExpression "pkgs.grimnir-radio";
      description = "Grimnir Radio control plane package to use";
    };

    mediaEnginePackage = mkOption {
      type = types.package;
      default = pkgs.mediaengine;
      defaultText = literalExpression "pkgs.mediaengine";
      description = "Grimnir Radio media engine package to use";
    };

    environment = mkOption {
      type = types.str;
      default = "production";
      description = "Environment (production, development)";
    };

    httpBind = mkOption {
      type = types.str;
      default = "0.0.0.0";
      description = "HTTP bind address";
    };

    httpPort = mkOption {
      type = types.port;
      default = 8080;
      description = "HTTP port";
    };

    databaseUrl = mkOption {
      type = types.str;
      default = "postgres://grimnir:grimnir@localhost:5432/grimnir?sslmode=disable";
      description = "PostgreSQL database URL";
    };

    redisUrl = mkOption {
      type = types.str;
      default = "redis://localhost:6379/0";
      description = "Redis URL for event bus";
    };

    mediaEngineGrpcAddr = mkOption {
      type = types.str;
      default = "localhost:9091";
      description = "Media engine gRPC address";
    };

    jwtSecret = mkOption {
      type = types.str;
      default = "CHANGE_THIS_IN_PRODUCTION_PLEASE";
      description = "JWT secret for authentication (CHANGE THIS!)";
    };

    mediaStoragePath = mkOption {
      type = types.path;
      default = "/var/lib/grimnir-radio/media";
      description = "Path to media storage directory";
    };

    tracingEnabled = mkOption {
      type = types.bool;
      default = false;
      description = "Enable OpenTelemetry tracing";
    };

    tracingSampleRate = mkOption {
      type = types.float;
      default = 0.1;
      description = "Tracing sample rate (0.0-1.0)";
    };

    otlpEndpoint = mkOption {
      type = types.str;
      default = "localhost:4317";
      description = "OTLP endpoint for traces";
    };

    # Full stack options
    enableDatabase = mkOption {
      type = types.bool;
      default = true;
      description = "Enable PostgreSQL database (full installation)";
    };

    enableRedis = mkOption {
      type = types.bool;
      default = true;
      description = "Enable Redis (full installation)";
    };

    user = mkOption {
      type = types.str;
      default = "grimnir";
      description = "User to run Grimnir Radio as";
    };

    group = mkOption {
      type = types.str;
      default = "grimnir";
      description = "Group to run Grimnir Radio as";
    };
  };

  config = mkIf cfg.enable {
    # Create user and group
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = "/var/lib/grimnir-radio";
      createHome = true;
      description = "Grimnir Radio system user";
    };

    users.groups.${cfg.group} = { };

    # Create media storage directory
    systemd.tmpfiles.rules = [
      "d ${cfg.mediaStoragePath} 0755 ${cfg.user} ${cfg.group} -"
      "d /var/lib/grimnir-radio 0755 ${cfg.user} ${cfg.group} -"
      "d /var/log/grimnir-radio 0755 ${cfg.user} ${cfg.group} -"
    ];

    # PostgreSQL (if enabled)
    services.postgresql = mkIf cfg.enableDatabase {
      enable = true;
      ensureDatabases = [ "grimnir" ];
      ensureUsers = [{
        name = "grimnir";
        ensureDBOwnership = true;
      }];
    };

    # Redis (if enabled)
    services.redis.servers.grimnir = mkIf cfg.enableRedis {
      enable = true;
      port = 6379;
      bind = "127.0.0.1";
      save = [ [ 900 1 ] [ 300 10 ] [ 60 10000 ] ];
    };

    # Media Engine systemd service
    systemd.services.grimnir-mediaengine = {
      description = "Grimnir Radio Media Engine";
      wantedBy = [ "multi-user.target" ];
      after = [ "network.target" ];
      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        ExecStart = "${cfg.mediaEnginePackage}/bin/mediaengine --grpc-port 9091";
        Restart = "always";
        RestartSec = "10s";

        # Security hardening
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        NoNewPrivileges = true;
        ReadWritePaths = [
          "/var/lib/grimnir-radio"
          "/var/log/grimnir-radio"
          cfg.mediaStoragePath
        ];

        # Resource limits
        MemoryMax = "2G";
        CPUQuota = "200%";

        # Logging
        StandardOutput = "journal";
        StandardError = "journal";
        SyslogIdentifier = "grimnir-mediaengine";
      };
    };

    # Control Plane systemd service
    systemd.services.grimnir-radio = {
      description = "Grimnir Radio Control Plane";
      wantedBy = [ "multi-user.target" ];
      after = [
        "network.target"
        "grimnir-mediaengine.service"
      ] ++ optional cfg.enableDatabase "postgresql.service"
        ++ optional cfg.enableRedis "redis-grimnir.service";
      requires = [ "grimnir-mediaengine.service" ];

      serviceConfig = {
        Type = "simple";
        User = cfg.user;
        Group = cfg.group;
        ExecStart = "${cfg.package}/bin/grimnirradio serve";
        Restart = "always";
        RestartSec = "10s";

        # Environment
        Environment = [
          "CONFIG_FILE=${configFile}"
        ];

        # Security hardening
        PrivateTmp = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        NoNewPrivileges = true;
        ReadWritePaths = [
          "/var/lib/grimnir-radio"
          "/var/log/grimnir-radio"
          cfg.mediaStoragePath
        ];

        # Resource limits
        MemoryMax = "1G";
        CPUQuota = "100%";

        # Logging
        StandardOutput = "journal";
        StandardError = "journal";
        SyslogIdentifier = "grimnir-radio";
      };
    };

    # Nginx reverse proxy (optional, for production)
    services.nginx = mkIf (cfg.httpPort != 80) {
      enable = mkDefault false;
      recommendedProxySettings = true;
      recommendedTlsSettings = true;
      recommendedOptimisation = true;
      recommendedGzipSettings = true;

      virtualHosts."grimnir.local" = mkDefault {
        locations."/" = {
          proxyPass = "http://${cfg.httpBind}:${toString cfg.httpPort}";
          proxyWebsockets = true;
        };
      };
    };

    # Firewall rules (open ports)
    networking.firewall.allowedTCPPorts = [ cfg.httpPort ];
  };
}
