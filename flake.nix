{
  description = "Ari development shell";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs {
          inherit system;
        };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            golangci-lint
            gofumpt
            just
            git
            nixpkgs-fmt
            sqlc
            sqlite
            atlas
          ];

          shellHook = ''
            export GOTOOLCHAIN=local

            echo "Ari dev shell (pre-alpha)"
            echo "Run: just verify"
          '';
        };
      }
    );
}
