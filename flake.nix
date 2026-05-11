{
  description = "unictl — declarative reconciler + observability CLI for UniFi Network controllers";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        version =
          if (self ? rev) then builtins.substring 0 7 self.rev else "dev";
      in {
        packages.default = pkgs.buildGoModule {
          pname = "unictl";
          inherit version;
          src = ./.;
          vendorHash = null;
          subPackages = [ "cmd/unictl" ];
          ldflags = [
            "-s"
            "-w"
            "-X github.com/gunk-dev/unictl/internal/version.GitSHA=${version}"
          ];
          meta = with pkgs.lib; {
            description =
              "Declarative reconciler + observability CLI for UniFi Network controllers";
            homepage = "https://github.com/gunk-dev/unictl";
            license = licenses.asl20;
            mainProgram = "unictl";
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            golangci-lint
            cue
          ];
        };
      });
}
