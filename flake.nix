{
  description = "skopeo - Work with remote images registries";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs { inherit system; };
    in
    {
      packages.${system} = {
        skopeo = pkgs.buildGoModule {
          pname = "skopeo";
          version = "dev";

          src = self;

          vendorHash = null;

          doCheck = false;

          nativeBuildInputs = with pkgs; [
            pkg-config
            go-md2man
            installShellFiles
          ];

          buildInputs = with pkgs; [
            gpgme
            lvm2
            btrfs-progs
          ];

          buildPhase = ''
            runHook preBuild
            patchShebangs .
            make bin/skopeo docs
            make completions
            runHook postBuild
          '';

          installPhase = ''
            runHook preInstall
            PREFIX=$out make install-binary install-docs install-completions
            runHook postInstall
          '';
        };
        default = self.packages.${system}.skopeo;
      };

      checks.${system} = {
        skopeo = self.packages.${system}.skopeo;
      };

      devShells.${system}.default = pkgs.mkShell {
        inputsFrom = [ self.packages.${system}.skopeo ];
        packages = with pkgs; [
          go
          golangci-lint
        ];
      };
    };
}
