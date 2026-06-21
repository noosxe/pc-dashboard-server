{
  description = "A lightweight, low-overhead system daemon for PC Dashboard";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.pc-dashboard-server = pkgs.buildGoModule {
          pname = "pc-dashboard-server";
          version = "0.3.0";

          src = ./.;

          # Initial placeholder hash. Users should replace this with the actual computed hash
          # output by Nix on the first build attempt.
          vendorHash = pkgs.lib.fakeHash;

          ldflags = [ "-s" "-w" ];

          env = {
            CGO_ENABLED = "0";
          };

          meta = with pkgs.lib; {
            description = "A lightweight, low-overhead system daemon for PC Dashboard";
            homepage = "https://github.com/noosxe/pc-dashboard-server";
          };
        };

        packages.default = self.packages.${system}.pc-dashboard-server;

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
            go-tools
          ];
        };
      }
    ) // {
      nixosModules.pc-dashboard-server = import ./nixos-module.nix self;
      nixosModules.default = self.nixosModules.pc-dashboard-server;
    };
}
