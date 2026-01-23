{
  description = "Grimnir Radio - Modern broadcast automation system";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
          overlays = [ self.overlays.default ];
        };
      in
      {
        packages = {
          # Basic: Just Grimnir Radio binaries (for nerds who know what they're doing)
          grimnir-radio = pkgs.callPackage ./nix/package.nix { };
          mediaengine = pkgs.callPackage ./nix/mediaengine-package.nix { };

          # Combined package with both binaries
          default = pkgs.symlinkJoin {
            name = "grimnir-radio-full";
            paths = [
              pkgs.grimnir-radio
              pkgs.mediaengine
            ];
          };
        };

        # Dev: Development environment with all dependencies
        devShells.default = pkgs.callPackage ./nix/dev-shell.nix { };

        # Apps for easy running
        apps = {
          # Run control plane
          grimnir-radio = {
            type = "app";
            program = "${pkgs.grimnir-radio}/bin/grimnirradio";
          };

          # Run media engine
          mediaengine = {
            type = "app";
            program = "${pkgs.mediaengine}/bin/mediaengine";
          };

          # Default: run control plane
          default = self.apps.${system}.grimnir-radio;
        };

        # Formatter for nix files
        formatter = pkgs.nixpkgs-fmt;
      }
    ) // {
      # NixOS module for full turn-key installation
      nixosModules.default = import ./nix/module.nix;

      overlays.default = final: prev: {
        grimnir-radio = final.callPackage ./nix/package.nix { };
        mediaengine = final.callPackage ./nix/mediaengine-package.nix { };
      };
    };
}
